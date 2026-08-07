// Package ports defines the hexagon boundary between the MCP protocol layer
// and the GCP adapter layer. The MCP layer imports only this package — never
// internal/gcp — keeping the two sides strictly decoupled.
package ports

import (
	"context"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// ─── Domain sub-interfaces ────────────────────────────────────────────────────
// Each *Tools struct and resource handler depends only on the sub-interface it
// actually uses. This keeps mocks small and makes tool boundaries explicit.
// The composite GCPService (below) embeds all sub-interfaces and is the single
// type that adapters must implement.

// GKEService covers GKE cluster management and mutation operations.
type GKEService interface {
	ListClusters(ctx context.Context, req models.ListClustersRequest) (models.ListClustersResponse, error)
	GetClusterDetails(ctx context.Context, req models.GetClusterDetailsRequest) (models.ClusterDetails, error)
	GetClusterBottlenecks(ctx context.Context, req models.GetClusterBottlenecksRequest) (models.ClusterBottleneckReport, error)
	ScaleDeployment(ctx context.Context, req models.ScaleDeploymentRequest) (models.ScaleDeploymentResponse, error)
}

// GKEWorkloadService covers Kubernetes workload introspection (Phase 2).
type GKEWorkloadService interface {
	ListGKEWorkloads(ctx context.Context, req models.ListGKEWorkloadsRequest) (models.ListGKEWorkloadsResponse, error)
	GetGKEWorkloadDetails(ctx context.Context, req models.GetGKEWorkloadDetailsRequest) (models.GKEWorkloadDetails, error)
	ListGKEServices(ctx context.Context, req models.ListGKEServicesRequest) (models.ListGKEServicesResponse, error)
	ListGKEIngresses(ctx context.Context, req models.ListGKEIngressesRequest) (models.ListGKEIngressesResponse, error)
	ListGKENetworkPolicies(ctx context.Context, req models.ListGKENetworkPoliciesRequest) (models.ListGKENetworkPoliciesResponse, error)
}

// GKEMeshService covers service mesh topology (Phase 2).
type GKEMeshService interface {
	GetGKEMeshTopology(ctx context.Context, req models.GetGKEMeshTopologyRequest) (models.GKEMeshTopologyResponse, error)
}

// NetworkingService covers VPC, load balancers, and API gateways (Phase 2).
type NetworkingService interface {
	ListLoadBalancers(ctx context.Context, req models.ListLoadBalancersRequest) (models.ListLoadBalancersResponse, error)
	ListURLMaps(ctx context.Context, req models.ListURLMapsRequest) (models.ListURLMapsResponse, error)
	ListNEGs(ctx context.Context, req models.ListNEGsRequest) (models.ListNEGsResponse, error)
	ListAPIGateways(ctx context.Context, req models.ListAPIGatewaysRequest) (models.ListAPIGatewaysResponse, error)
	ListVPCNetworks(ctx context.Context, req models.ListVPCNetworksRequest) (models.ListVPCNetworksResponse, error)
	ListVPCSubnets(ctx context.Context, req models.ListVPCSubnetsRequest) (models.ListVPCSubnetsResponse, error)
	ListPSCEndpoints(ctx context.Context, req models.ListPSCEndpointsRequest) (models.ListPSCEndpointsResponse, error)
}

// CloudRunService covers Cloud Run services, jobs, and traffic management.
type CloudRunService interface {
	ListServices(ctx context.Context, req models.ListServicesRequest) (models.ListServicesResponse, error)
	GetServiceDetails(ctx context.Context, req models.GetServiceDetailsRequest) (models.ServiceDetails, error)
	ListRevisions(ctx context.Context, req models.ListRevisionsRequest) (models.ListRevisionsResponse, error)
	UpdateTraffic(ctx context.Context, req models.UpdateTrafficRequest) (models.UpdateTrafficResponse, error)
	ListJobs(ctx context.Context, req models.ListJobsRequest) (models.ListJobsResponse, error)
	GetJobDetails(ctx context.Context, req models.GetJobDetailsRequest) (models.JobDetails, error)
	ListJobExecutions(ctx context.Context, req models.ListJobExecutionsRequest) (models.ListJobExecutionsResponse, error)
}

// FunctionsService covers Cloud Functions (Gen 1).
type FunctionsService interface {
	ListFunctions(ctx context.Context, req models.ListFunctionsRequest) (models.ListFunctionsResponse, error)
	GetFunctionDetails(ctx context.Context, req models.GetFunctionDetailsRequest) (models.FunctionDetails, error)
}

// EventarcService covers Eventarc triggers.
type EventarcService interface {
	ListTriggers(ctx context.Context, req models.ListTriggersRequest) (models.ListTriggersResponse, error)
	GetTrigger(ctx context.Context, req models.GetTriggerRequest) (models.TriggerDetails, error)
}

// SchedulerService covers Cloud Scheduler jobs.
type SchedulerService interface {
	ListSchedulerJobs(ctx context.Context, req models.ListSchedulerJobsRequest) (models.ListSchedulerJobsResponse, error)
}

// WorkflowsService covers Cloud Workflows and their executions.
type WorkflowsService interface {
	ListWorkflows(ctx context.Context, req models.ListWorkflowsRequest) (models.ListWorkflowsResponse, error)
	ListWorkflowExecutions(ctx context.Context, req models.ListWorkflowExecutionsRequest) (models.ListWorkflowExecutionsResponse, error)
}

// TasksService covers Cloud Tasks queues.
type TasksService interface {
	ListTaskQueues(ctx context.Context, req models.ListTaskQueuesRequest) (models.ListTaskQueuesResponse, error)
}

// SecretManagerService covers Secret Manager secrets.
type SecretManagerService interface {
	ListSecrets(ctx context.Context, req models.ListSecretsRequest) (models.ListSecretsResponse, error)
}

// VPCAccessService covers Serverless VPC Access connectors.
type VPCAccessService interface {
	ListVPCConnectors(ctx context.Context, req models.ListVPCConnectorsRequest) (models.ListVPCConnectorsResponse, error)
}

// CloudSQLService covers Cloud SQL instances.
type CloudSQLService interface {
	ListSQLInstances(ctx context.Context, req models.ListSQLInstancesRequest) (models.ListSQLInstancesResponse, error)
}

// PubSubService covers Pub/Sub topics and subscriptions.
type PubSubService interface {
	ListTopics(ctx context.Context, req models.ListTopicsRequest) (models.ListTopicsResponse, error)
	InspectTopicHealth(ctx context.Context, req models.InspectTopicHealthRequest) (models.TopicHealthReport, error)
	ListSubscriptions(ctx context.Context, req models.ListSubscriptionsRequest) (models.ListSubscriptionsResponse, error)
}

// LoggingService covers Cloud Logging queries.
type LoggingService interface {
	QueryRecentLogs(ctx context.Context, req models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error)
}

// MonitoringService covers Cloud Monitoring metrics, alerts, and observability.
type MonitoringService interface {
	GetMetrics(ctx context.Context, req models.GetMetricsRequest) (models.GetMetricsResponse, error)
	ListMetricDescriptors(ctx context.Context, req models.ListMetricDescriptorsRequest) (models.ListMetricDescriptorsResponse, error)
	ListTraceServices(ctx context.Context, req models.ListTraceServicesRequest) (models.ListTraceServicesResponse, error)
	ListAlertPolicies(ctx context.Context, req models.ListAlertPoliciesRequest) (models.ListAlertPoliciesResponse, error)
	ListUptimeChecks(ctx context.Context, req models.ListUptimeChecksRequest) (models.ListUptimeChecksResponse, error)
	ListSLOs(ctx context.Context, req models.ListSLOsRequest) (models.ListSLOsResponse, error)
	ListDashboards(ctx context.Context, req models.ListDashboardsRequest) (models.ListDashboardsResponse, error)
	ListTraceDependencyEdges(ctx context.Context, req models.ListTraceDependencyEdgesRequest) (models.ListTraceDependencyEdgesResponse, error)
}

// IAMService covers IAM permission testing, policy bindings, and service accounts.
type IAMService interface {
	TestPermissions(ctx context.Context, req models.TestPermissionsRequest) (models.TestPermissionsResponse, error)
	GetResourceIAMBindings(ctx context.Context, req models.GetResourceIAMBindingsRequest) (models.GetResourceIAMBindingsResponse, error)
	ListServiceAccounts(ctx context.Context, req models.ListServiceAccountsRequest) (models.ListServiceAccountsResponse, error)
}

// TopologyService covers cross-service dependency topology.
type TopologyService interface {
	GetServiceTopology(ctx context.Context, req models.GetServiceTopologyRequest) (models.ServiceTopologyReport, error)
}

// AuraService covers Aura Score calculation and project summary.
type AuraService interface {
	GetAuraScore(ctx context.Context, req models.GetAuraScoreRequest) (models.AuraReport, error)
	GetProjectAuraSummary(ctx context.Context, req models.ProjectAuraSummaryRequest) (models.ProjectAuraSummaryResponse, error)
	GetGKEAuraScore(ctx context.Context, req models.GetGKEAuraScoreRequest) (models.GKEAuraReport, error)
	GetGCSAuraScore(ctx context.Context, req models.GetGCSAuraScoreRequest) (models.GCSAuraReport, error)
}

// BigQueryService covers BigQuery dataset and table discovery.
type BigQueryService interface {
	ListDatasets(ctx context.Context, req models.ListDatasetsRequest) (models.ListDatasetsResponse, error)
	ListTables(ctx context.Context, req models.ListTablesRequest) (models.ListTablesResponse, error)
	GetTableSchema(ctx context.Context, req models.GetTableSchemaRequest) (models.TableSchemaResponse, error)
}

// StorageService covers Cloud Storage bucket and object operations.
type StorageService interface {
	ListBuckets(ctx context.Context, req models.ListBucketsRequest) (models.ListBucketsResponse, error)
	GetBucketMetadata(ctx context.Context, req models.GetBucketMetadataRequest) (models.BucketMetadataResponse, error)
	ListBucketObjects(ctx context.Context, req models.ListBucketObjectsRequest) (models.ListBucketObjectsResponse, error)
}

// ServerlessGraphService covers the serverless resource relationship graph.
type ServerlessGraphService interface {
	ExportServerlessGraph(ctx context.Context, req models.ExportServerlessGraphRequest) (models.ServerlessGraph, error)
}

// DatastoreService covers managed databases: Spanner, AlloyDB, Firestore, Memorystore (Phase 2).
type DatastoreService interface {
	ListSpannerInstances(ctx context.Context, req models.ListSpannerInstancesRequest) (models.ListSpannerInstancesResponse, error)
	ListAlloyDBClusters(ctx context.Context, req models.ListAlloyDBClustersRequest) (models.ListAlloyDBClustersResponse, error)
	ListFirestoreDatabases(ctx context.Context, req models.ListFirestoreDatabasesRequest) (models.ListFirestoreDatabasesResponse, error)
	ListMemorystoreInstances(ctx context.Context, req models.ListMemorystoreInstancesRequest) (models.ListMemorystoreInstancesResponse, error)
}

// SupplyChainService covers Artifact Registry, Cloud Build, and Service Directory (Phase 2).
type SupplyChainService interface {
	ListArtifactRegistryRepos(ctx context.Context, req models.ListArtifactRegistryReposRequest) (models.ListArtifactRegistryReposResponse, error)
	ListArtifactRegistryImages(ctx context.Context, req models.ListArtifactRegistryImagesRequest) (models.ListArtifactRegistryImagesResponse, error)
	ListCloudBuildTriggers(ctx context.Context, req models.ListCloudBuildTriggersRequest) (models.ListCloudBuildTriggersResponse, error)
	ListServiceDirectoryNamespaces(ctx context.Context, req models.ListServiceDirectoryNamespacesRequest) (models.ListServiceDirectoryNamespacesResponse, error)
}

// TaggingService covers resource tag discovery via CRM v3 TagBindings.
type TaggingService interface {
	ListTaggedResources(ctx context.Context, req models.ListTaggedResourcesRequest) (models.ListTaggedResourcesResponse, error)
}

// CoverageService covers observability coverage analysis across the project.
type CoverageService interface {
	GetObservabilityCoverage(ctx context.Context, req models.GetObservabilityCoverageRequest) (models.ObservabilityCoverageResponse, error)
}

// ArchGraphService covers full cross-service architecture graph export.
type ArchGraphService interface {
	ExportArchitectureGraph(ctx context.Context, req models.ExportArchitectureGraphRequest) (models.ServerlessGraph, error)
}

// RecommenderExportService exports active GCP recommendations to BigQuery.
// Off by default; enabled via RECOMMENDER_BQ_EXPORT_ENABLED=true.
type RecommenderExportService interface {
	ExportRecommendationsToBQ(ctx context.Context, req models.ExportRecommendationsToBQRequest) (models.ExportRecommendationsToBQResponse, error)
}

// ─── Composite port ───────────────────────────────────────────────────────────

// GCPService is the single composite secondary-port interface that all GCP
// adapters must implement. It embeds every domain sub-interface so that
// adapters, decorators (SafetyDecorator, anonymize middleware), and the
// dependency-injection root (main.go) deal with one type, while individual
// tool handlers depend only on the narrow sub-interface they actually use.
//
// All implementations must be safe for concurrent use.
type GCPService interface {
	GKEService
	GKEWorkloadService
	GKEMeshService
	NetworkingService
	CloudRunService
	FunctionsService
	EventarcService
	SchedulerService
	WorkflowsService
	TasksService
	SecretManagerService
	VPCAccessService
	CloudSQLService
	PubSubService
	LoggingService
	MonitoringService
	IAMService
	TopologyService
	AuraService
	BigQueryService
	StorageService
	ServerlessGraphService
	DatastoreService
	SupplyChainService
	TaggingService
	CoverageService
	ArchGraphService
	RecommenderExportService
}
