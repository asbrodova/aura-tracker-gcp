package gcp

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	containerpb "cloud.google.com/go/container/apiv1/containerpb"
	"google.golang.org/api/cloudasset/v1"
	"google.golang.org/api/iterator"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

type securityClusterExposureResult struct {
	services []models.PublicServiceSecurityFact
	coverage []models.SecurityCoverageUnit
}

func (a *gcpAdapter) collectGKEPublicServiceSecurityFacts(ctx context.Context, projectID string, result *models.PublicServiceSecurityFacts) {
	if a.clusterMgr == nil {
		result.Coverage = append(result.Coverage, securityCoverageUnit("gke_clusters", "project", "projects/"+projectID, "error", 0, "GKE client is unavailable"))
		return
	}
	response, err := a.clusterMgr.ListClusters(ctx, &containerpb.ListClustersRequest{Parent: "projects/" + projectID + "/locations/-"})
	if err != nil {
		result.Coverage = append(result.Coverage, securityCoverageUnit("gke_clusters", "project", "projects/"+projectID, "error", 0, err.Error()))
		return
	}
	result.Coverage = append(result.Coverage, securityCoverageUnit("gke_clusters", "project", "projects/"+projectID, "complete", len(response.Clusters), ""))

	concurrency := a.securityConfig.ClusterConcurrency
	if concurrency <= 0 {
		concurrency = 4
	}
	semaphore := make(chan struct{}, concurrency)
	outcomes := make(chan securityClusterExposureResult, len(response.Clusters))
	var wg sync.WaitGroup
	for _, cluster := range response.Clusters {
		cluster := cluster
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				outcomes <- a.collectClusterExposure(ctx, projectID, cluster)
				return
			}
			outcomes <- a.collectClusterExposure(ctx, projectID, cluster)
		}()
	}
	wg.Wait()
	close(outcomes)
	for outcome := range outcomes {
		result.Services = append(result.Services, outcome.services...)
		result.Coverage = append(result.Coverage, outcome.coverage...)
	}
	sort.Slice(result.Coverage, func(i, j int) bool {
		return result.Coverage[i].Collector+result.Coverage[i].Scope < result.Coverage[j].Collector+result.Coverage[j].Scope
	})
}

func (a *gcpAdapter) collectClusterExposure(parent context.Context, projectID string, cluster *containerpb.Cluster) securityClusterExposureResult {
	resource := fmt.Sprintf("projects/%s/locations/%s/clusters/%s", projectID, cluster.Location, cluster.Name)
	result := securityClusterExposureResult{}
	ctx, cancel := context.WithTimeout(parent, a.securityClusterTimeout())
	defer cancel()
	k8s, accessMode, err := a.dialSecurityK8s(ctx, projectID, cluster.Location, cluster.Name)
	if err != nil {
		status := "error"
		if a.securityConfig.KubernetesAccess == "disabled" {
			status = "skipped"
		}
		result.coverage = append(result.coverage, securityCoverageUnit("kubernetes_access", "cluster", resource, status, 0, err.Error()))
		return result
	}
	result.coverage = append(result.coverage, securityCoverageUnit("kubernetes_access", "cluster", resource, "complete", 1, accessMode))

	workloads, workloadPageToken, workloadErr := k8s.listWorkloads(ctx, "", "", a.securityResourceLimit(), "")
	workloads, workloadStatus, workloadMessage := boundedSecurityResources(workloads, workloadErr, a.securityResourceLimit())
	if workloadErr == nil && workloadPageToken != "" {
		workloadStatus, workloadMessage = "truncated", fmt.Sprintf("resource safety cap reached: inspected %d workloads", len(workloads))
	}
	result.coverage = append(result.coverage, securityCoverageUnit("gke_workloads", "cluster", resource, workloadStatus, len(workloads), workloadMessage))

	services, servicesTruncated, serviceErr := k8s.listServicesBounded(ctx, "", a.securityResourceLimit())
	services, serviceStatus, serviceMessage := boundedSecurityResources(services, serviceErr, a.securityResourceLimit())
	if serviceErr == nil && servicesTruncated {
		serviceStatus, serviceMessage = "truncated", fmt.Sprintf("resource safety cap reached: inspected %d services", len(services))
	}
	result.coverage = append(result.coverage, securityCoverageUnit("gke_services", "cluster", resource, serviceStatus, len(services), serviceMessage))
	if serviceErr == nil {
		for _, service := range services {
			if fact, ok := publicGKEServiceFact(projectID, cluster, service, workloads); ok {
				result.services = append(result.services, fact)
			}
		}
	}

	ingresses, ingressesTruncated, ingressErr := k8s.listIngressesBounded(ctx, "", a.securityResourceLimit())
	ingresses, ingressStatus, ingressMessage := boundedSecurityResources(ingresses, ingressErr, a.securityResourceLimit())
	if ingressErr == nil && ingressesTruncated {
		ingressStatus, ingressMessage = "truncated", fmt.Sprintf("resource safety cap reached: inspected %d ingresses and HTTPRoutes", len(ingresses))
	}
	result.coverage = append(result.coverage, securityCoverageUnit("gke_ingresses", "cluster", resource, ingressStatus, len(ingresses), ingressMessage))

	gateways, gatewaysTruncated, gatewayErr := k8s.listGatewaysBounded(ctx, "", a.securityResourceLimit())
	if gatewayErr != nil && (strings.Contains(gatewayErr.Error(), "HTTP 404") || strings.Contains(gatewayErr.Error(), "HTTP 405")) {
		gatewayErr = nil
		gateways = []k8sGateway{}
	}
	gateways, gatewayStatus, gatewayMessage := boundedSecurityResources(gateways, gatewayErr, a.securityResourceLimit())
	if gatewayErr == nil && gatewaysTruncated {
		gatewayStatus, gatewayMessage = "truncated", fmt.Sprintf("resource safety cap reached: inspected %d gateways", len(gateways))
	}
	result.coverage = append(result.coverage, securityCoverageUnit("gke_gateways", "cluster", resource, gatewayStatus, len(gateways), gatewayMessage))

	if ingressErr == nil {
		gatewayIndex := make(map[string]k8sGateway, len(gateways))
		for _, gateway := range gateways {
			gatewayIndex[gateway.Metadata.Namespace+"/"+gateway.Metadata.Name] = gateway
		}
		for _, ingress := range ingresses {
			if fact, ok := publicGKEIngressFact(projectID, cluster, ingress, gatewayIndex, workloads); ok {
				result.services = append(result.services, fact)
			}
		}
	}
	if gatewayErr == nil {
		for _, gateway := range gateways {
			if fact, ok := publicGKEGatewayFact(projectID, cluster, gateway, ingresses, workloads); ok {
				result.services = append(result.services, fact)
			}
		}
	}
	return result
}

func publicGKEServiceFact(projectID string, cluster *containerpb.Cluster, service models.GKEServiceSummary, workloads []models.GKEWorkloadSummary) (models.PublicServiceSecurityFact, bool) {
	external := false
	reason := ""
	switch {
	case strings.EqualFold(service.Type, "LoadBalancer") && !service.Internal:
		external = true
		reason = "external LoadBalancer Service"
	case hasPublicAddress(service.ExternalIPs):
		external = true
		reason = "Service declares a public externalIP"
	}
	if !external {
		return models.PublicServiceSecurityFact{}, false
	}
	ports := make([]int32, 0, len(service.Ports))
	for _, port := range service.Ports {
		ports = appendUniqueInt32(ports, port.Port)
	}
	sourceRanges := append([]string(nil), service.LoadBalancerSourceRanges...)
	if len(sourceRanges) == 0 {
		sourceRanges = []string{"0.0.0.0/0", "::/0"}
	}
	return models.PublicServiceSecurityFact{
		ResourceName: securityK8sResource(projectID, cluster, service.Namespace, "services", service.Name),
		Name:         service.Name, Kind: "gke_service", Region: cluster.Location, ClusterName: cluster.Name, Namespace: service.Namespace,
		External: true, Addresses: append([]string(nil), service.LoadBalancerAddresses...), Ports: ports, SourceRanges: sourceRanges,
		WorkloadIdentities: matchingWorkloadIdentities(service.Namespace, service.Selector, workloads), ExposureReason: reason,
	}, true
}

func publicGKEIngressFact(projectID string, cluster *containerpb.Cluster, ingress models.GKEIngressSummary, gateways map[string]k8sGateway, workloads []models.GKEWorkloadSummary) (models.PublicServiceSecurityFact, bool) {
	addresses := append([]string(nil), ingress.Addresses...)
	tls := ingress.TLSEnabled
	plaintext := ingress.PlaintextEnabled
	external := !ingress.Internal && (ingress.IngressClass == "" || strings.Contains(strings.ToLower(ingress.IngressClass), "gce") || strings.Contains(strings.ToLower(ingress.IngressClass), "external") || hasPublicAddress(addresses))
	reason := "external Kubernetes Ingress"
	if ingress.Kind == "HTTPRoute" {
		external = false
		reason = "HTTPRoute attached to an external Gateway"
		for _, parent := range ingress.ParentGateways {
			gateway, ok := gateways[parent]
			if !ok || !isExternalGateway(gateway) {
				continue
			}
			external = true
			addresses = appendUniqueStrings(addresses, gatewayAddresses(gateway)...)
			tls = tls || gatewayTLSEnabled(gateway)
			plaintext = plaintext || gatewayPlaintextEnabled(gateway)
		}
	}
	if !external {
		return models.PublicServiceSecurityFact{}, false
	}
	backends := ingressBackends(ingress)
	return models.PublicServiceSecurityFact{
		ResourceName: securityK8sResource(projectID, cluster, ingress.Namespace, strings.ToLower(ingress.Kind)+"s", ingress.Name),
		Name:         ingress.Name, Kind: "gke_" + strings.ToLower(ingress.Kind), Region: cluster.Location, ClusterName: cluster.Name, Namespace: ingress.Namespace,
		External: true, TLSEnabled: tls, PlaintextEnabled: plaintext, Addresses: addresses, Hosts: append([]string(nil), ingress.Hosts...), BackendServices: backends,
		WorkloadIdentities: backendWorkloadIdentities(ingress.Namespace, backends, workloads), ExposureReason: reason,
	}, true
}

func publicGKEGatewayFact(projectID string, cluster *containerpb.Cluster, gateway k8sGateway, ingresses []models.GKEIngressSummary, workloads []models.GKEWorkloadSummary) (models.PublicServiceSecurityFact, bool) {
	if !isExternalGateway(gateway) {
		return models.PublicServiceSecurityFact{}, false
	}
	hosts := []string{}
	ports := []int32{}
	backends := []string{}
	key := gateway.Metadata.Namespace + "/" + gateway.Metadata.Name
	for _, listener := range gateway.Spec.Listeners {
		hosts = appendUnique(hosts, listener.Hostname)
		ports = appendUniqueInt32(ports, listener.Port)
	}
	for _, route := range ingresses {
		if route.Kind != "HTTPRoute" || !containsString(route.ParentGateways, key) {
			continue
		}
		hosts = appendUniqueStrings(hosts, route.Hosts...)
		backends = appendUniqueStrings(backends, ingressBackends(route)...)
	}
	return models.PublicServiceSecurityFact{
		ResourceName: securityK8sResource(projectID, cluster, gateway.Metadata.Namespace, "gateways", gateway.Metadata.Name),
		Name:         gateway.Metadata.Name, Kind: "gke_gateway", Region: cluster.Location, ClusterName: cluster.Name, Namespace: gateway.Metadata.Namespace,
		External: true, TLSEnabled: gatewayTLSEnabled(gateway), PlaintextEnabled: gatewayPlaintextEnabled(gateway), Addresses: gatewayAddresses(gateway), Hosts: hosts, Ports: ports,
		BackendServices: backends, WorkloadIdentities: backendWorkloadIdentities(gateway.Metadata.Namespace, backends, workloads),
		ExposureReason: "external Gateway API Gateway",
	}, true
}

func (a *gcpAdapter) collectWorkloadIdentitySecurityFacts(ctx context.Context, projectID string) (models.WorkloadIdentitySecurityFacts, error) {
	response, err := a.clusterMgr.ListClusters(ctx, &containerpb.ListClustersRequest{Parent: "projects/" + projectID + "/locations/-"})
	if err != nil {
		return models.WorkloadIdentitySecurityFacts{}, wrapGCPError("security.ListWorkloadIdentitySecurityFacts", err)
	}
	result := models.WorkloadIdentitySecurityFacts{Clusters: []models.WorkloadIdentitySecurityFact{}}
	result.Coverage = append(result.Coverage, securityCoverageUnit("gke_clusters", "project", "projects/"+projectID, "complete", len(response.Clusters), ""))
	concurrency := a.securityConfig.ClusterConcurrency
	if concurrency <= 0 {
		concurrency = 4
	}
	semaphore := make(chan struct{}, concurrency)
	type outcome struct {
		cluster  models.WorkloadIdentitySecurityFact
		coverage []models.SecurityCoverageUnit
	}
	outcomes := make(chan outcome, len(response.Clusters))
	var wg sync.WaitGroup
	for _, cluster := range response.Clusters {
		cluster := cluster
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				fact, coverage := a.collectClusterWorkloadIdentity(ctx, projectID, cluster)
				outcomes <- outcome{cluster: fact, coverage: coverage}
				return
			}
			fact, coverage := a.collectClusterWorkloadIdentity(ctx, projectID, cluster)
			outcomes <- outcome{cluster: fact, coverage: coverage}
		}()
	}
	wg.Wait()
	close(outcomes)
	for item := range outcomes {
		result.Clusters = append(result.Clusters, item.cluster)
		result.Coverage = append(result.Coverage, item.coverage...)
	}
	a.enrichAnnotatedGSAs(ctx, projectID, &result)
	sort.Slice(result.Clusters, func(i, j int) bool { return result.Clusters[i].ResourceName < result.Clusters[j].ResourceName })
	sort.Slice(result.Coverage, func(i, j int) bool {
		return result.Coverage[i].Collector+result.Coverage[i].Scope < result.Coverage[j].Collector+result.Coverage[j].Scope
	})
	return result, nil
}

type gsaSecurityEvidence struct {
	impersonators map[string]bool
	roles         []string
	coverage      []models.SecurityCoverageUnit
}

func (a *gcpAdapter) enrichAnnotatedGSAs(ctx context.Context, projectID string, result *models.WorkloadIdentitySecurityFacts) {
	evidenceByGSA := make(map[string]gsaSecurityEvidence)
	for _, cluster := range result.Clusters {
		for _, account := range cluster.ServiceAccounts {
			gsa := strings.TrimSpace(account.GoogleServiceAccount)
			if gsa == "" {
				continue
			}
			if _, ok := evidenceByGSA[gsa]; ok {
				continue
			}
			evidenceByGSA[gsa] = a.collectGSAEvidence(ctx, projectID, gsa)
		}
	}
	for clusterIndex := range result.Clusters {
		cluster := &result.Clusters[clusterIndex]
		for accountIndex := range cluster.ServiceAccounts {
			account := &cluster.ServiceAccounts[accountIndex]
			evidence, ok := evidenceByGSA[account.GoogleServiceAccount]
			if !ok {
				continue
			}
			legacyMember := fmt.Sprintf("serviceAccount:%s[%s/%s]", cluster.WorkloadPool, account.Namespace, account.Name)
			account.ImpersonationVerified = evidence.impersonators[legacyMember]
			account.MappedGSARoles = append([]string(nil), evidence.roles...)
		}
	}
	for _, evidence := range evidenceByGSA {
		result.Coverage = append(result.Coverage, evidence.coverage...)
	}
}

func (a *gcpAdapter) collectGSAEvidence(ctx context.Context, auditedProjectID, gsa string) gsaSecurityEvidence {
	result := gsaSecurityEvidence{impersonators: make(map[string]bool)}
	resource := "projects/-/serviceAccounts/" + gsa
	if a.iamAdminSvc == nil {
		result.coverage = append(result.coverage, securityCoverageUnit("gsa_impersonation_policy", "service_account", resource, "error", 0, "IAM Admin client is unavailable"))
	} else {
		policy, err := a.iamAdminSvc.Projects.ServiceAccounts.GetIamPolicy(resource).OptionsRequestedPolicyVersion(3).Context(ctx).Do()
		if err != nil {
			result.coverage = append(result.coverage, securityCoverageUnit("gsa_impersonation_policy", "service_account", resource, "error", 0, err.Error()))
		} else {
			bindings := 0
			for _, binding := range policy.Bindings {
				if binding.Role != "roles/iam.workloadIdentityUser" {
					continue
				}
				bindings++
				for _, member := range binding.Members {
					result.impersonators[member] = true
				}
			}
			result.coverage = append(result.coverage, securityCoverageUnit("gsa_impersonation_policy", "service_account", resource, "complete", bindings, ""))
		}
	}

	gsaProjectID := googleServiceAccountProjectID(gsa)
	if a.assetSvc == nil {
		result.coverage = append(result.coverage, securityCoverageUnit("gsa_role_search", "project", "projects/"+gsaProjectID, "error", 0, "Cloud Asset client is unavailable"))
		return result
	}
	if gsaProjectID == "" {
		result.coverage = append(result.coverage, securityCoverageUnit("gsa_role_search", "service_account", resource, "skipped", 0, "could not infer the GSA project from its email"))
		return result
	}
	member := "serviceAccount:" + gsa
	count := 0
	search := a.assetSvc.V1.SearchAllIamPolicies("projects/" + gsaProjectID).PageSize(500).Query(fmt.Sprintf("policy:%q", member))
	err := search.Pages(ctx, func(page *cloudasset.SearchAllIamPoliciesResponse) error {
		for _, policy := range page.Results {
			if policy.Policy == nil {
				continue
			}
			count++
			if count > maxSecurityIAMPolicies {
				return errSecurityPolicyLimit
			}
			for _, binding := range policy.Policy.Bindings {
				if containsString(binding.Members, member) {
					result.roles = appendUnique(result.roles, binding.Role)
				}
			}
		}
		return nil
	})
	sort.Strings(result.roles)
	switch err {
	case nil:
		message := ""
		if gsaProjectID != auditedProjectID {
			message = "cross-project GSA role grants verified"
		}
		result.coverage = append(result.coverage, securityCoverageUnit("gsa_role_search", "project", "projects/"+gsaProjectID, "complete", count, message))
	case errSecurityPolicyLimit, iterator.Done:
		result.coverage = append(result.coverage, securityCoverageUnit("gsa_role_search", "project", "projects/"+gsaProjectID, "truncated", count, "IAM policy safety cap reached"))
	default:
		result.coverage = append(result.coverage, securityCoverageUnit("gsa_role_search", "project", "projects/"+gsaProjectID, "error", count, err.Error()))
	}
	return result
}

func googleServiceAccountProjectID(email string) string {
	at := strings.LastIndexByte(email, '@')
	const suffix = ".iam.gserviceaccount.com"
	if at < 0 || !strings.HasSuffix(email, suffix) {
		return ""
	}
	return strings.TrimSuffix(email[at+1:], suffix)
}

func (a *gcpAdapter) collectClusterWorkloadIdentity(parent context.Context, projectID string, cluster *containerpb.Cluster) (models.WorkloadIdentitySecurityFact, []models.SecurityCoverageUnit) {
	resourceName := fmt.Sprintf("//container.googleapis.com/projects/%s/locations/%s/clusters/%s", projectID, cluster.Location, cluster.Name)
	resource := strings.TrimPrefix(resourceName, "//container.googleapis.com/")
	fact := models.WorkloadIdentitySecurityFact{ResourceName: resourceName, ClusterName: cluster.Name, Location: cluster.Location, NodePools: []models.NodePoolIdentityFact{}}
	if cluster.WorkloadIdentityConfig != nil {
		fact.WorkloadPool = cluster.WorkloadIdentityConfig.WorkloadPool
	}
	if cluster.PrivateClusterConfig != nil {
		fact.PrivateNodes = cluster.PrivateClusterConfig.EnablePrivateNodes
	}
	for _, pool := range cluster.NodePools {
		node := models.NodePoolIdentityFact{Name: pool.Name}
		if pool.Config != nil {
			node.ServiceAccount = pool.Config.ServiceAccount
			if pool.Config.WorkloadMetadataConfig != nil {
				node.MetadataMode = pool.Config.WorkloadMetadataConfig.Mode.String()
			}
		}
		fact.NodePools = append(fact.NodePools, node)
	}

	ctx, cancel := context.WithTimeout(parent, a.securityClusterTimeout())
	defer cancel()
	k8s, mode, err := a.dialSecurityK8s(ctx, projectID, cluster.Location, cluster.Name)
	if err != nil {
		status := "error"
		if a.securityConfig.KubernetesAccess == "disabled" {
			status = "skipped"
		}
		return fact, []models.SecurityCoverageUnit{securityCoverageUnit("kubernetes_access", "cluster", resource, status, 0, err.Error())}
	}
	fact.AccessMode = mode
	coverage := []models.SecurityCoverageUnit{securityCoverageUnit("kubernetes_access", "cluster", resource, "complete", 1, mode)}

	serviceAccounts, serviceAccountsTruncated, saErr := k8s.listKubernetesServiceAccountsBounded(ctx, "", a.securityResourceLimit())
	serviceAccounts, saStatus, saMessage := boundedSecurityResources(serviceAccounts, saErr, a.securityResourceLimit())
	if saErr == nil && serviceAccountsTruncated {
		saStatus, saMessage = "truncated", fmt.Sprintf("resource safety cap reached: inspected %d service accounts", len(serviceAccounts))
	}
	coverage = append(coverage, securityCoverageUnit("kubernetes_service_accounts", "cluster", resource, saStatus, len(serviceAccounts), saMessage))
	if saErr == nil {
		for _, account := range serviceAccounts {
			fact.ServiceAccounts = append(fact.ServiceAccounts, models.KubernetesServiceAccountFact{
				Namespace: account.Metadata.Namespace, Name: account.Metadata.Name,
				GoogleServiceAccount: account.Metadata.Annotations["iam.gke.io/gcp-service-account"], AutomountToken: account.AutomountServiceAccountToken,
			})
		}
	}

	workloads, workloadPageToken, workloadErr := k8s.listWorkloads(ctx, "", "", a.securityResourceLimit(), "")
	workloads, workloadStatus, workloadMessage := boundedSecurityResources(workloads, workloadErr, a.securityResourceLimit())
	if workloadErr == nil && workloadPageToken != "" {
		workloadStatus, workloadMessage = "truncated", fmt.Sprintf("resource safety cap reached: inspected %d workloads", len(workloads))
	}
	coverage = append(coverage, securityCoverageUnit("kubernetes_workload_identities", "cluster", resource, workloadStatus, len(workloads), workloadMessage))
	if workloadErr == nil {
		for _, workload := range workloads {
			serviceAccount := workload.ServiceAccount
			if serviceAccount == "" {
				serviceAccount = "default"
			}
			fact.Workloads = append(fact.Workloads, models.KubernetesWorkloadIdentityFact{
				Namespace: workload.Namespace, Name: workload.Name, Kind: workload.Kind, ServiceAccount: serviceAccount,
				AutomountToken: workload.AutomountServiceAccountToken, Labels: workload.Labels,
			})
			linkWorkloadToServiceAccount(&fact, workload.Namespace, serviceAccount, workload.Kind+"/"+workload.Name)
		}
	}
	sort.Slice(fact.ServiceAccounts, func(i, j int) bool {
		return fact.ServiceAccounts[i].Namespace+"/"+fact.ServiceAccounts[i].Name < fact.ServiceAccounts[j].Namespace+"/"+fact.ServiceAccounts[j].Name
	})
	sort.Slice(fact.Workloads, func(i, j int) bool {
		return fact.Workloads[i].Namespace+"/"+fact.Workloads[i].Kind+"/"+fact.Workloads[i].Name < fact.Workloads[j].Namespace+"/"+fact.Workloads[j].Kind+"/"+fact.Workloads[j].Name
	})
	return fact, coverage
}

func linkWorkloadToServiceAccount(fact *models.WorkloadIdentitySecurityFact, namespace, name, workload string) {
	for i := range fact.ServiceAccounts {
		if fact.ServiceAccounts[i].Namespace == namespace && fact.ServiceAccounts[i].Name == name {
			fact.ServiceAccounts[i].Workloads = appendUnique(fact.ServiceAccounts[i].Workloads, workload)
			return
		}
	}
	fact.ServiceAccounts = append(fact.ServiceAccounts, models.KubernetesServiceAccountFact{Namespace: namespace, Name: name, Workloads: []string{workload}})
}

func (a *gcpAdapter) securityClusterTimeout() time.Duration {
	if a.securityConfig.PerClusterTimeout > 0 {
		return a.securityConfig.PerClusterTimeout
	}
	return 20 * time.Second
}

func (a *gcpAdapter) securityResourceLimit() int {
	if a.securityConfig.MaxResourcesPerKind > 0 {
		return a.securityConfig.MaxResourcesPerKind
	}
	return 2000
}

func boundedSecurityResources[T any](values []T, err error, limit int) ([]T, string, string) {
	if err != nil {
		return nil, "error", err.Error()
	}
	if len(values) > limit {
		return values[:limit], "truncated", fmt.Sprintf("resource safety cap reached: inspected %d of %d", limit, len(values))
	}
	return values, "complete", ""
}

func securityK8sResource(projectID string, cluster *containerpb.Cluster, namespace, kind, name string) string {
	return fmt.Sprintf("//container.googleapis.com/projects/%s/locations/%s/clusters/%s/kubernetes/namespaces/%s/%s/%s", projectID, cluster.Location, cluster.Name, namespace, kind, name)
}

func matchingWorkloadIdentities(namespace string, selector map[string]string, workloads []models.GKEWorkloadSummary) []string {
	if len(selector) == 0 {
		return nil
	}
	identities := []string{}
	for _, workload := range workloads {
		if workload.Namespace != namespace || !labelsMatch(selector, workload.Labels) {
			continue
		}
		serviceAccount := workload.ServiceAccount
		if serviceAccount == "" {
			serviceAccount = "default"
		}
		identities = appendUnique(identities, namespace+"/"+serviceAccount)
	}
	sort.Strings(identities)
	return identities
}

func backendWorkloadIdentities(namespace string, backends []string, _ []models.GKEWorkloadSummary) []string {
	// Without Service selectors here, the backend can only be identified by
	// name. Preserve namespace/name evidence; Service collection performs the
	// exact selector-to-workload identity correlation.
	identities := []string{}
	for _, backend := range backends {
		if backend != "" {
			identities = appendUnique(identities, namespace+"/service:"+backend)
		}
	}
	return identities
}

func labelsMatch(selector, labels map[string]string) bool {
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}

func ingressBackends(ingress models.GKEIngressSummary) []string {
	backends := []string{}
	if ingress.DefaultBackend != "" {
		backends = append(backends, ingress.DefaultBackend)
	}
	for _, rule := range ingress.Rules {
		for _, path := range rule.Paths {
			backends = appendUnique(backends, path.BackendName)
		}
	}
	sort.Strings(backends)
	return backends
}

func isExternalGateway(gateway k8sGateway) bool {
	class := strings.ToLower(gateway.Spec.GatewayClassName)
	if strings.Contains(class, "internal") {
		return false
	}
	return strings.Contains(class, "external") || hasPublicAddress(gatewayAddresses(gateway))
}

func gatewayAddresses(gateway k8sGateway) []string {
	addresses := []string{}
	for _, address := range gateway.Spec.Addresses {
		addresses = appendUnique(addresses, address.Value)
	}
	for _, address := range gateway.Status.Addresses {
		addresses = appendUnique(addresses, address.Value)
	}
	return addresses
}

func gatewayTLSEnabled(gateway k8sGateway) bool {
	for _, listener := range gateway.Spec.Listeners {
		if strings.EqualFold(listener.Protocol, "HTTPS") || strings.EqualFold(listener.Protocol, "TLS") || listener.TLS != nil {
			return true
		}
	}
	return false
}

func gatewayPlaintextEnabled(gateway k8sGateway) bool {
	for _, listener := range gateway.Spec.Listeners {
		if strings.EqualFold(listener.Protocol, "HTTP") {
			return true
		}
	}
	return false
}

func hasPublicAddress(addresses []string) bool {
	for _, address := range addresses {
		if address == "" {
			continue
		}
		ip := net.ParseIP(address)
		if ip == nil {
			return true // DNS name.
		}
		if !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified() {
			return true
		}
	}
	return false
}

func appendUniqueInt32(values []int32, value int32) []int32 {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueStrings(values []string, additions ...string) []string {
	for _, value := range additions {
		if value != "" {
			values = appendUnique(values, value)
		}
	}
	return values
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
