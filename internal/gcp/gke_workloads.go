package gcp

import (
	"context"
	"fmt"
	"strings"

	containerpb "cloud.google.com/go/container/apiv1/containerpb"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gkehub/v1"
	"google.golang.org/api/iterator"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (a *gcpAdapter) dialSecurityK8s(ctx context.Context, projectID, location, clusterName string) (*k8sClient, string, error) {
	mode := a.securityConfig.KubernetesAccess
	if mode == "" {
		mode = "auto"
	}
	if mode == "disabled" {
		return nil, "disabled", fmt.Errorf("Kubernetes security enrichment is disabled")
	}
	if mode == "auto" || mode == "direct" {
		client, err := a.dialK8s(ctx, projectID, location, clusterName)
		if err == nil {
			return client, "direct", nil
		}
		if mode == "direct" {
			return nil, "direct", err
		}
	}
	client, err := a.dialSecurityK8sGateway(ctx, projectID, location, clusterName)
	if err != nil {
		return nil, "connect_gateway", err
	}
	return client, "connect_gateway", nil
}

func (a *gcpAdapter) dialSecurityK8sGateway(ctx context.Context, projectID, location, clusterName string) (*k8sClient, error) {
	if a.gkeHubSvc == nil {
		return nil, fmt.Errorf("GKE Hub client is unavailable")
	}
	fleetProject := a.securityConfig.FleetProjectID
	if fleetProject == "" {
		fleetProject = projectID
	}
	resourceLink := fmt.Sprintf("//container.googleapis.com/projects/%s/locations/%s/clusters/%s", projectID, location, clusterName)
	var membershipName string
	err := a.gkeHubSvc.Projects.Locations.Memberships.List("projects/"+fleetProject+"/locations/-").Pages(ctx, func(page *gkehub.ListMembershipsResponse) error {
		for _, membership := range page.Resources {
			if membership.Endpoint != nil && membership.Endpoint.GkeCluster != nil && membership.Endpoint.GkeCluster.ResourceLink == resourceLink {
				membershipName = membership.Name
				return iterator.Done
			}
		}
		return nil
	})
	if err != nil && err != iterator.Done {
		return nil, fmt.Errorf("list fleet memberships: %w", err)
	}
	if membershipName == "" {
		return nil, fmt.Errorf("cluster is not registered in fleet project %q", fleetProject)
	}
	parts := strings.Split(membershipName, "/")
	if len(parts) != 6 {
		return nil, fmt.Errorf("unexpected fleet membership name %q", membershipName)
	}
	endpoint := fmt.Sprintf("https://connectgateway.googleapis.com/v1/projects/%s/locations/%s/gkeMemberships/%s", parts[1], parts[3], parts[5])
	tokenSource, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("connect gateway token source: %w", err)
	}
	return dialGatewayWithTokenSource(endpoint, tokenSource), nil
}

// dialK8s fetches cluster endpoint + CA cert from the GKE API and constructs
// a thin K8s REST client authenticated via ADC. It returns a PermissionDeniedError
// when the cluster has a private endpoint with no external access.
func (a *gcpAdapter) dialK8s(ctx context.Context, projectID, location, clusterName string) (*k8sClient, error) {
	name := fmt.Sprintf("projects/%s/locations/%s/clusters/%s", projectID, location, clusterName)
	c, err := a.clusterMgr.GetCluster(ctx, &containerpb.GetClusterRequest{Name: name})
	if err != nil {
		return nil, wrapGCPError("gke.dialK8s.GetCluster", err)
	}

	// Private clusters with no public endpoint cannot be reached externally.
	if c.PrivateClusterConfig != nil && c.PrivateClusterConfig.EnablePrivateEndpoint {
		return nil, &PermissionDeniedError{
			Op: "gke.dialK8s",
			Err: fmt.Errorf(
				"cluster %q has a private control-plane endpoint (no public Kubernetes API); "+
					"configure authorized networks or enable Connect Gateway (GKE Hub) for external access",
				clusterName,
			),
		}
	}

	caCert := ""
	if c.MasterAuth != nil {
		caCert = c.MasterAuth.ClusterCaCertificate
	}
	return dialCluster(ctx, c.Endpoint, caCert)
}

// wrapK8sError converts raw K8s REST HTTP errors into typed gcp errors so that
// handleServiceError in the MCP layer surfaces them correctly to the LLM.
func wrapK8sErr(op string, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "HTTP 401") || strings.Contains(msg, "HTTP 403") {
		return &PermissionDeniedError{Op: op, Err: err}
	}
	if strings.Contains(msg, "HTTP 404") {
		return &NotFoundError{Op: op, Err: err}
	}
	return fmt.Errorf("%s: %w", op, err)
}

func (a *gcpAdapter) ListGKEWorkloads(ctx context.Context, req models.ListGKEWorkloadsRequest) (models.ListGKEWorkloadsResponse, error) {
	if err := a.rateWait(ctx, "gke.ListGKEWorkloads"); err != nil {
		return models.ListGKEWorkloadsResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	k8s, err := a.dialK8s(ctx, req.ProjectID, req.Location, req.ClusterName)
	if err != nil {
		return models.ListGKEWorkloadsResponse{}, err
	}

	workloads, err := k8s.listWorkloads(ctx, req.Namespace, req.Kind)
	if err != nil {
		return models.ListGKEWorkloadsResponse{}, wrapK8sErr("gke.ListGKEWorkloads", err)
	}
	return models.ListGKEWorkloadsResponse{Workloads: workloads}, nil
}

func (a *gcpAdapter) GetGKEWorkloadDetails(ctx context.Context, req models.GetGKEWorkloadDetailsRequest) (models.GKEWorkloadDetails, error) {
	if err := a.rateWait(ctx, "gke.GetGKEWorkloadDetails"); err != nil {
		return models.GKEWorkloadDetails{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	k8s, err := a.dialK8s(ctx, req.ProjectID, req.Location, req.ClusterName)
	if err != nil {
		return models.GKEWorkloadDetails{}, err
	}

	details, err := k8s.getWorkload(ctx, req.Namespace, req.Name, req.Kind)
	if err != nil {
		return models.GKEWorkloadDetails{}, wrapK8sErr("gke.GetGKEWorkloadDetails", err)
	}
	return details, nil
}

func (a *gcpAdapter) ListGKEServices(ctx context.Context, req models.ListGKEServicesRequest) (models.ListGKEServicesResponse, error) {
	if err := a.rateWait(ctx, "gke.ListGKEServices"); err != nil {
		return models.ListGKEServicesResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	k8s, err := a.dialK8s(ctx, req.ProjectID, req.Location, req.ClusterName)
	if err != nil {
		return models.ListGKEServicesResponse{}, err
	}

	services, err := k8s.listServices(ctx, req.Namespace)
	if err != nil {
		return models.ListGKEServicesResponse{}, wrapK8sErr("gke.ListGKEServices", err)
	}
	return models.ListGKEServicesResponse{Services: services}, nil
}

func (a *gcpAdapter) ListGKEIngresses(ctx context.Context, req models.ListGKEIngressesRequest) (models.ListGKEIngressesResponse, error) {
	if err := a.rateWait(ctx, "gke.ListGKEIngresses"); err != nil {
		return models.ListGKEIngressesResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	k8s, err := a.dialK8s(ctx, req.ProjectID, req.Location, req.ClusterName)
	if err != nil {
		return models.ListGKEIngressesResponse{}, err
	}

	ingresses, err := k8s.listIngresses(ctx, req.Namespace)
	if err != nil {
		return models.ListGKEIngressesResponse{}, wrapK8sErr("gke.ListGKEIngresses", err)
	}
	return models.ListGKEIngressesResponse{Ingresses: ingresses}, nil
}

func (a *gcpAdapter) ListGKENetworkPolicies(ctx context.Context, req models.ListGKENetworkPoliciesRequest) (models.ListGKENetworkPoliciesResponse, error) {
	if err := a.rateWait(ctx, "gke.ListGKENetworkPolicies"); err != nil {
		return models.ListGKENetworkPoliciesResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	k8s, err := a.dialK8s(ctx, req.ProjectID, req.Location, req.ClusterName)
	if err != nil {
		return models.ListGKENetworkPoliciesResponse{}, err
	}

	policies, err := k8s.listNetworkPolicies(ctx, req.Namespace)
	if err != nil {
		return models.ListGKENetworkPoliciesResponse{}, wrapK8sErr("gke.ListGKENetworkPolicies", err)
	}
	return models.ListGKENetworkPoliciesResponse{Policies: policies}, nil
}
