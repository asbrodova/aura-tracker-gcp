package drift

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// DataSource is the read-only subset of the GCP port used by environment
// comparison. The composite ports.GCPService satisfies it.
type DataSource interface {
	ListServices(context.Context, models.ListServicesRequest) (models.ListServicesResponse, error)
	GetServiceDetails(context.Context, models.GetServiceDetailsRequest) (models.ServiceDetails, error)
	ListRevisions(context.Context, models.ListRevisionsRequest) (models.ListRevisionsResponse, error)
	ListJobs(context.Context, models.ListJobsRequest) (models.ListJobsResponse, error)
	GetJobDetails(context.Context, models.GetJobDetailsRequest) (models.JobDetails, error)
	ListClusters(context.Context, models.ListClustersRequest) (models.ListClustersResponse, error)
	GetClusterDetails(context.Context, models.GetClusterDetailsRequest) (models.ClusterDetails, error)
	ListGKEWorkloads(context.Context, models.ListGKEWorkloadsRequest) (models.ListGKEWorkloadsResponse, error)
	GetGKEWorkloadDetails(context.Context, models.GetGKEWorkloadDetailsRequest) (models.GKEWorkloadDetails, error)
	ListGKEServices(context.Context, models.ListGKEServicesRequest) (models.ListGKEServicesResponse, error)
	ListGKEIngresses(context.Context, models.ListGKEIngressesRequest) (models.ListGKEIngressesResponse, error)
	ListGKENetworkPolicies(context.Context, models.ListGKENetworkPoliciesRequest) (models.ListGKENetworkPoliciesResponse, error)
	ListLoadBalancers(context.Context, models.ListLoadBalancersRequest) (models.ListLoadBalancersResponse, error)
	ListURLMaps(context.Context, models.ListURLMapsRequest) (models.ListURLMapsResponse, error)
	ListNEGs(context.Context, models.ListNEGsRequest) (models.ListNEGsResponse, error)
	ListAPIGateways(context.Context, models.ListAPIGatewaysRequest) (models.ListAPIGatewaysResponse, error)
	ListVPCNetworks(context.Context, models.ListVPCNetworksRequest) (models.ListVPCNetworksResponse, error)
	ListVPCSubnets(context.Context, models.ListVPCSubnetsRequest) (models.ListVPCSubnetsResponse, error)
	ListPSCEndpoints(context.Context, models.ListPSCEndpointsRequest) (models.ListPSCEndpointsResponse, error)
	ListFunctions(context.Context, models.ListFunctionsRequest) (models.ListFunctionsResponse, error)
	GetFunctionDetails(context.Context, models.GetFunctionDetailsRequest) (models.FunctionDetails, error)
	ListTriggers(context.Context, models.ListTriggersRequest) (models.ListTriggersResponse, error)
	ListSchedulerJobs(context.Context, models.ListSchedulerJobsRequest) (models.ListSchedulerJobsResponse, error)
	ListWorkflows(context.Context, models.ListWorkflowsRequest) (models.ListWorkflowsResponse, error)
	ListTaskQueues(context.Context, models.ListTaskQueuesRequest) (models.ListTaskQueuesResponse, error)
	ListSecrets(context.Context, models.ListSecretsRequest) (models.ListSecretsResponse, error)
	ListVPCConnectors(context.Context, models.ListVPCConnectorsRequest) (models.ListVPCConnectorsResponse, error)
	ListSQLInstances(context.Context, models.ListSQLInstancesRequest) (models.ListSQLInstancesResponse, error)
	ListTopics(context.Context, models.ListTopicsRequest) (models.ListTopicsResponse, error)
	ListSubscriptions(context.Context, models.ListSubscriptionsRequest) (models.ListSubscriptionsResponse, error)
	ListBuckets(context.Context, models.ListBucketsRequest) (models.ListBucketsResponse, error)
	GetBucketMetadata(context.Context, models.GetBucketMetadataRequest) (models.BucketMetadataResponse, error)
	ListDatasets(context.Context, models.ListDatasetsRequest) (models.ListDatasetsResponse, error)
	ListTables(context.Context, models.ListTablesRequest) (models.ListTablesResponse, error)
	GetTableSchema(context.Context, models.GetTableSchemaRequest) (models.TableSchemaResponse, error)
	ListSpannerInstances(context.Context, models.ListSpannerInstancesRequest) (models.ListSpannerInstancesResponse, error)
	ListAlloyDBClusters(context.Context, models.ListAlloyDBClustersRequest) (models.ListAlloyDBClustersResponse, error)
	ListFirestoreDatabases(context.Context, models.ListFirestoreDatabasesRequest) (models.ListFirestoreDatabasesResponse, error)
	ListMemorystoreInstances(context.Context, models.ListMemorystoreInstancesRequest) (models.ListMemorystoreInstancesResponse, error)
	ListArtifactRegistryRepos(context.Context, models.ListArtifactRegistryReposRequest) (models.ListArtifactRegistryReposResponse, error)
	ListCloudBuildTriggers(context.Context, models.ListCloudBuildTriggersRequest) (models.ListCloudBuildTriggersResponse, error)
	ListServiceDirectoryNamespaces(context.Context, models.ListServiceDirectoryNamespacesRequest) (models.ListServiceDirectoryNamespacesResponse, error)
	ListAlertPolicies(context.Context, models.ListAlertPoliciesRequest) (models.ListAlertPoliciesResponse, error)
	ListUptimeChecks(context.Context, models.ListUptimeChecksRequest) (models.ListUptimeChecksResponse, error)
	ListSLOs(context.Context, models.ListSLOsRequest) (models.ListSLOsResponse, error)
	ListDashboards(context.Context, models.ListDashboardsRequest) (models.ListDashboardsResponse, error)
	ListServiceAccounts(context.Context, models.ListServiceAccountsRequest) (models.ListServiceAccountsResponse, error)
}

var supportedComponents = []string{
	"bigquery", "cloudrun", "cloudsql", "datastores", "eventarc", "functions",
	"gke", "gke_workloads", "iam", "monitoring", "networking", "pubsub",
	"scheduler", "secretmanager", "storage", "supplychain", "tasks", "vpcaccess", "workflows",
}

const maxResourcesPerComponent = 500

type GCPCollector struct{ source DataSource }

func NewGCPCollector(source DataSource) *GCPCollector { return &GCPCollector{source: source} }

func (c *GCPCollector) SupportedComponents() []string {
	out := append([]string(nil), supportedComponents...)
	return out
}

func (c *GCPCollector) Collect(ctx context.Context, req CollectionRequest) (CollectionResult, error) {
	if c.source == nil {
		return CollectionResult{}, errors.New("drift collector: data source is required")
	}
	var result CollectionResult
	var err error
	switch req.Component {
	case "cloudrun":
		result, err = c.collectCloudRun(ctx, req)
	case "gke":
		result, err = c.collectGKE(ctx, req)
	case "gke_workloads":
		result, err = c.collectGKEWorkloads(ctx, req)
	case "networking":
		result, err = c.collectNetworking(ctx, req)
	case "functions":
		result, err = c.collectFunctions(ctx, req)
	case "eventarc":
		result, err = c.collectEventarc(ctx, req)
	case "scheduler":
		result, err = c.collectScheduler(ctx, req)
	case "workflows":
		result, err = c.collectWorkflows(ctx, req)
	case "tasks":
		result, err = c.collectTasks(ctx, req)
	case "secretmanager":
		result, err = c.collectSecrets(ctx, req)
	case "vpcaccess":
		result, err = c.collectVPCAccess(ctx, req)
	case "cloudsql":
		result, err = c.collectCloudSQL(ctx, req)
	case "pubsub":
		result, err = c.collectPubSub(ctx, req)
	case "storage":
		result, err = c.collectStorage(ctx, req)
	case "bigquery":
		result, err = c.collectBigQuery(ctx, req)
	case "datastores":
		result, err = c.collectDatastores(ctx, req)
	case "supplychain":
		result, err = c.collectSupplyChain(ctx, req)
	case "monitoring":
		result, err = c.collectMonitoring(ctx, req)
	case "iam":
		result, err = c.collectIAM(ctx, req)
	default:
		err = fmt.Errorf("unsupported drift component %q", req.Component)
	}
	sort.SliceStable(result.Resources, func(i, j int) bool { return result.Resources[i].exactIdentity() < result.Resources[j].exactIdentity() })
	if len(result.Resources) > maxResourcesPerComponent {
		result.Resources = result.Resources[:maxResourcesPerComponent]
		result.Partial = true
		result.Warnings = append(result.Warnings, fmt.Sprintf("resource limit of %d reached", maxResourcesPerComponent))
	}
	return result, err
}

func resource(component, kind, name, location, qualifier string, config any) Resource {
	return Resource{Component: component, ResourceType: kind, Name: bareName(name), Location: location, Qualifier: qualifier, Config: configMap(config)}
}

func bareName(name string) string {
	if strings.Contains(name, "/") {
		return path.Base(name)
	}
	return name
}

func includeResource(req CollectionRequest, name, location string) bool {
	if len(req.ResourceNames) > 0 && !containsFold(req.ResourceNames, bareName(name)) {
		return false
	}
	if len(req.Locations) > 0 && !containsFold(req.Locations, location) {
		return false
	}
	return true
}

func containsFold(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), wanted) {
			return true
		}
	}
	return false
}

func toolErrors(errors []models.ToolError) (bool, []string) {
	if len(errors) == 0 {
		return false, nil
	}
	warnings := make([]string, 0, len(errors))
	for _, item := range errors {
		warnings = append(warnings, item.Message)
	}
	return true, warnings
}

func mergePartial(result *CollectionResult, partial bool, warnings []string) {
	result.Partial = result.Partial || partial
	result.Warnings = append(result.Warnings, warnings...)
}

func markInventoryTruncated(result *CollectionResult, truncated bool, inventory string) {
	if !truncated {
		return
	}
	result.Partial = true
	result.Warnings = append(result.Warnings, inventory+" inventory was truncated; omitted resources were not compared")
}
