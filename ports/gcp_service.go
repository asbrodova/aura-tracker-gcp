// Package ports defines the hexagon boundary between the MCP protocol layer
// and the GCP adapter layer. The MCP layer imports only this package — never
// internal/gcp — keeping the two sides strictly decoupled.
package ports

import (
	"context"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// GCPService is the single secondary-port interface that all GCP adapters must
// implement. Tool handlers in internal/mcp call only these methods; they never
// import a GCP SDK type directly.
//
// All implementations must be safe for concurrent use.
type GCPService interface {
	// --- GKE ---
	ListClusters(ctx context.Context, req models.ListClustersRequest) (models.ListClustersResponse, error)
	GetClusterDetails(ctx context.Context, req models.GetClusterDetailsRequest) (models.ClusterDetails, error)
	GetClusterBottlenecks(ctx context.Context, req models.GetClusterBottlenecksRequest) (models.ClusterBottleneckReport, error)
	ScaleDeployment(ctx context.Context, req models.ScaleDeploymentRequest) (models.ScaleDeploymentResponse, error)

	// --- GKE Workloads (Phase 2) ---
	ListGKEWorkloads(ctx context.Context, req models.ListGKEWorkloadsRequest) (models.ListGKEWorkloadsResponse, error)
	GetGKEWorkloadDetails(ctx context.Context, req models.GetGKEWorkloadDetailsRequest) (models.GKEWorkloadDetails, error)
	ListGKEServices(ctx context.Context, req models.ListGKEServicesRequest) (models.ListGKEServicesResponse, error)
	ListGKEIngresses(ctx context.Context, req models.ListGKEIngressesRequest) (models.ListGKEIngressesResponse, error)
	ListGKENetworkPolicies(ctx context.Context, req models.ListGKENetworkPoliciesRequest) (models.ListGKENetworkPoliciesResponse, error)

	// --- GKE Mesh Topology (Phase 2) ---
	GetGKEMeshTopology(ctx context.Context, req models.GetGKEMeshTopologyRequest) (models.GKEMeshTopologyResponse, error)

	// --- Networking (Phase 2) ---
	ListLoadBalancers(ctx context.Context, req models.ListLoadBalancersRequest) (models.ListLoadBalancersResponse, error)
	ListURLMaps(ctx context.Context, req models.ListURLMapsRequest) (models.ListURLMapsResponse, error)
	ListNEGs(ctx context.Context, req models.ListNEGsRequest) (models.ListNEGsResponse, error)
	ListAPIGateways(ctx context.Context, req models.ListAPIGatewaysRequest) (models.ListAPIGatewaysResponse, error)
	ListVPCNetworks(ctx context.Context, req models.ListVPCNetworksRequest) (models.ListVPCNetworksResponse, error)
	ListVPCSubnets(ctx context.Context, req models.ListVPCSubnetsRequest) (models.ListVPCSubnetsResponse, error)
	ListPSCEndpoints(ctx context.Context, req models.ListPSCEndpointsRequest) (models.ListPSCEndpointsResponse, error)

	// --- Cloud Run ---
	ListServices(ctx context.Context, req models.ListServicesRequest) (models.ListServicesResponse, error)
	GetServiceDetails(ctx context.Context, req models.GetServiceDetailsRequest) (models.ServiceDetails, error)
	UpdateTraffic(ctx context.Context, req models.UpdateTrafficRequest) (models.UpdateTrafficResponse, error)
	ListJobs(ctx context.Context, req models.ListJobsRequest) (models.ListJobsResponse, error)
	GetJobDetails(ctx context.Context, req models.GetJobDetailsRequest) (models.JobDetails, error)
	ListJobExecutions(ctx context.Context, req models.ListJobExecutionsRequest) (models.ListJobExecutionsResponse, error)

	// --- Cloud Functions ---
	ListFunctions(ctx context.Context, req models.ListFunctionsRequest) (models.ListFunctionsResponse, error)
	GetFunctionDetails(ctx context.Context, req models.GetFunctionDetailsRequest) (models.FunctionDetails, error)

	// --- Eventarc ---
	ListTriggers(ctx context.Context, req models.ListTriggersRequest) (models.ListTriggersResponse, error)
	GetTrigger(ctx context.Context, req models.GetTriggerRequest) (models.TriggerDetails, error)

	// --- Cloud Scheduler ---
	ListSchedulerJobs(ctx context.Context, req models.ListSchedulerJobsRequest) (models.ListSchedulerJobsResponse, error)

	// --- Workflows ---
	ListWorkflows(ctx context.Context, req models.ListWorkflowsRequest) (models.ListWorkflowsResponse, error)
	ListWorkflowExecutions(ctx context.Context, req models.ListWorkflowExecutionsRequest) (models.ListWorkflowExecutionsResponse, error)

	// --- Cloud Tasks ---
	ListTaskQueues(ctx context.Context, req models.ListTaskQueuesRequest) (models.ListTaskQueuesResponse, error)

	// --- Secret Manager ---
	ListSecrets(ctx context.Context, req models.ListSecretsRequest) (models.ListSecretsResponse, error)

	// --- Serverless VPC Access ---
	ListVPCConnectors(ctx context.Context, req models.ListVPCConnectorsRequest) (models.ListVPCConnectorsResponse, error)

	// --- Cloud SQL ---
	ListSQLInstances(ctx context.Context, req models.ListSQLInstancesRequest) (models.ListSQLInstancesResponse, error)

	// --- Pub/Sub ---
	ListTopics(ctx context.Context, req models.ListTopicsRequest) (models.ListTopicsResponse, error)
	InspectTopicHealth(ctx context.Context, req models.InspectTopicHealthRequest) (models.TopicHealthReport, error)
	ListSubscriptions(ctx context.Context, req models.ListSubscriptionsRequest) (models.ListSubscriptionsResponse, error)

	// --- Cloud Logging ---
	QueryRecentLogs(ctx context.Context, req models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error)

	// --- Cloud Monitoring ---
	GetMetrics(ctx context.Context, req models.GetMetricsRequest) (models.GetMetricsResponse, error)
	ListMetricDescriptors(ctx context.Context, req models.ListMetricDescriptorsRequest) (models.ListMetricDescriptorsResponse, error)
	ListTraceServices(ctx context.Context, req models.ListTraceServicesRequest) (models.ListTraceServicesResponse, error)
	ListAlertPolicies(ctx context.Context, req models.ListAlertPoliciesRequest) (models.ListAlertPoliciesResponse, error)
	ListUptimeChecks(ctx context.Context, req models.ListUptimeChecksRequest) (models.ListUptimeChecksResponse, error)
	ListSLOs(ctx context.Context, req models.ListSLOsRequest) (models.ListSLOsResponse, error)
	ListDashboards(ctx context.Context, req models.ListDashboardsRequest) (models.ListDashboardsResponse, error)

	// --- IAM ---
	TestPermissions(ctx context.Context, req models.TestPermissionsRequest) (models.TestPermissionsResponse, error)
	GetResourceIAMBindings(ctx context.Context, req models.GetResourceIAMBindingsRequest) (models.GetResourceIAMBindingsResponse, error)
	ListServiceAccounts(ctx context.Context, req models.ListServiceAccountsRequest) (models.ListServiceAccountsResponse, error)

	// --- Topology ---
	GetServiceTopology(ctx context.Context, req models.GetServiceTopologyRequest) (models.ServiceTopologyReport, error)

	// --- Aura Score ---
	GetAuraScore(ctx context.Context, req models.GetAuraScoreRequest) (models.AuraReport, error)
	GetProjectAuraSummary(ctx context.Context, req models.ProjectAuraSummaryRequest) (models.ProjectAuraSummaryResponse, error)

	// --- BigQuery (Resources) ---
	ListDatasets(ctx context.Context, req models.ListDatasetsRequest) (models.ListDatasetsResponse, error)
	ListTables(ctx context.Context, req models.ListTablesRequest) (models.ListTablesResponse, error)
	GetTableSchema(ctx context.Context, req models.GetTableSchemaRequest) (models.TableSchemaResponse, error)

	// --- Cloud Storage (Resources) ---
	ListBuckets(ctx context.Context, req models.ListBucketsRequest) (models.ListBucketsResponse, error)
	GetBucketMetadata(ctx context.Context, req models.GetBucketMetadataRequest) (models.BucketMetadataResponse, error)

	// --- Serverless Graph ---
	ExportServerlessGraph(ctx context.Context, req models.ExportServerlessGraphRequest) (models.ServerlessGraph, error)

	// --- Data Stores (Phase 2) ---
	ListSpannerInstances(ctx context.Context, req models.ListSpannerInstancesRequest) (models.ListSpannerInstancesResponse, error)
	ListAlloyDBClusters(ctx context.Context, req models.ListAlloyDBClustersRequest) (models.ListAlloyDBClustersResponse, error)
	ListFirestoreDatabases(ctx context.Context, req models.ListFirestoreDatabasesRequest) (models.ListFirestoreDatabasesResponse, error)
	ListMemorystoreInstances(ctx context.Context, req models.ListMemorystoreInstancesRequest) (models.ListMemorystoreInstancesResponse, error)

	// --- Supply Chain (Phase 2) ---
	ListArtifactRegistryRepos(ctx context.Context, req models.ListArtifactRegistryReposRequest) (models.ListArtifactRegistryReposResponse, error)
	ListArtifactRegistryImages(ctx context.Context, req models.ListArtifactRegistryImagesRequest) (models.ListArtifactRegistryImagesResponse, error)
	ListCloudBuildTriggers(ctx context.Context, req models.ListCloudBuildTriggersRequest) (models.ListCloudBuildTriggersResponse, error)
	ListServiceDirectoryNamespaces(ctx context.Context, req models.ListServiceDirectoryNamespacesRequest) (models.ListServiceDirectoryNamespacesResponse, error)
}
