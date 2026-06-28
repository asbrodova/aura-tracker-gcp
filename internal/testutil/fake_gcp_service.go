// Package testutil provides shared test doubles for the ports.GCPService interface.
// Use FakeGCPService instead of duplicating mocks across packages.
//
// Zero value is valid: every method returns its zero value and nil error.
// Override individual methods by setting the corresponding Func field.
//
//	fake := &testutil.FakeGCPService{
//	    ListClustersFunc: func(_ context.Context, _ models.ListClustersRequest) (models.ListClustersResponse, error) {
//	        return models.ListClustersResponse{Clusters: []models.ClusterSummary{{Name: "c1"}}}, nil
//	    },
//	}
package testutil

import (
	"context"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// FakeGCPService is a configurable test double that satisfies ports.GCPService.
// Set any Func field to override that method's behaviour; unset fields return
// zero values and nil errors.
type FakeGCPService struct {
	// GKE
	ListClustersFunc          func(context.Context, models.ListClustersRequest) (models.ListClustersResponse, error)
	GetClusterDetailsFunc     func(context.Context, models.GetClusterDetailsRequest) (models.ClusterDetails, error)
	GetClusterBottlenecksFunc func(context.Context, models.GetClusterBottlenecksRequest) (models.ClusterBottleneckReport, error)
	ScaleDeploymentFunc       func(context.Context, models.ScaleDeploymentRequest) (models.ScaleDeploymentResponse, error)

	// GKE Workloads
	ListGKEWorkloadsFunc       func(context.Context, models.ListGKEWorkloadsRequest) (models.ListGKEWorkloadsResponse, error)
	GetGKEWorkloadDetailsFunc  func(context.Context, models.GetGKEWorkloadDetailsRequest) (models.GKEWorkloadDetails, error)
	ListGKEServicesFunc        func(context.Context, models.ListGKEServicesRequest) (models.ListGKEServicesResponse, error)
	ListGKEIngressesFunc       func(context.Context, models.ListGKEIngressesRequest) (models.ListGKEIngressesResponse, error)
	ListGKENetworkPoliciesFunc func(context.Context, models.ListGKENetworkPoliciesRequest) (models.ListGKENetworkPoliciesResponse, error)

	// GKE Mesh
	GetGKEMeshTopologyFunc func(context.Context, models.GetGKEMeshTopologyRequest) (models.GKEMeshTopologyResponse, error)

	// Networking
	ListLoadBalancersFunc func(context.Context, models.ListLoadBalancersRequest) (models.ListLoadBalancersResponse, error)
	ListURLMapsFunc       func(context.Context, models.ListURLMapsRequest) (models.ListURLMapsResponse, error)
	ListNEGsFunc          func(context.Context, models.ListNEGsRequest) (models.ListNEGsResponse, error)
	ListAPIGatewaysFunc   func(context.Context, models.ListAPIGatewaysRequest) (models.ListAPIGatewaysResponse, error)
	ListVPCNetworksFunc   func(context.Context, models.ListVPCNetworksRequest) (models.ListVPCNetworksResponse, error)
	ListVPCSubnetsFunc    func(context.Context, models.ListVPCSubnetsRequest) (models.ListVPCSubnetsResponse, error)
	ListPSCEndpointsFunc  func(context.Context, models.ListPSCEndpointsRequest) (models.ListPSCEndpointsResponse, error)

	// Cloud Run
	ListServicesFunc      func(context.Context, models.ListServicesRequest) (models.ListServicesResponse, error)
	GetServiceDetailsFunc func(context.Context, models.GetServiceDetailsRequest) (models.ServiceDetails, error)
	UpdateTrafficFunc     func(context.Context, models.UpdateTrafficRequest) (models.UpdateTrafficResponse, error)
	ListJobsFunc          func(context.Context, models.ListJobsRequest) (models.ListJobsResponse, error)
	GetJobDetailsFunc     func(context.Context, models.GetJobDetailsRequest) (models.JobDetails, error)
	ListJobExecutionsFunc func(context.Context, models.ListJobExecutionsRequest) (models.ListJobExecutionsResponse, error)

	// Functions
	ListFunctionsFunc      func(context.Context, models.ListFunctionsRequest) (models.ListFunctionsResponse, error)
	GetFunctionDetailsFunc func(context.Context, models.GetFunctionDetailsRequest) (models.FunctionDetails, error)

	// Eventarc
	ListTriggersFunc func(context.Context, models.ListTriggersRequest) (models.ListTriggersResponse, error)
	GetTriggerFunc   func(context.Context, models.GetTriggerRequest) (models.TriggerDetails, error)

	// Scheduler
	ListSchedulerJobsFunc func(context.Context, models.ListSchedulerJobsRequest) (models.ListSchedulerJobsResponse, error)

	// Workflows
	ListWorkflowsFunc          func(context.Context, models.ListWorkflowsRequest) (models.ListWorkflowsResponse, error)
	ListWorkflowExecutionsFunc func(context.Context, models.ListWorkflowExecutionsRequest) (models.ListWorkflowExecutionsResponse, error)

	// Tasks
	ListTaskQueuesFunc func(context.Context, models.ListTaskQueuesRequest) (models.ListTaskQueuesResponse, error)

	// Secret Manager
	ListSecretsFunc func(context.Context, models.ListSecretsRequest) (models.ListSecretsResponse, error)

	// VPC Access
	ListVPCConnectorsFunc func(context.Context, models.ListVPCConnectorsRequest) (models.ListVPCConnectorsResponse, error)

	// Cloud SQL
	ListSQLInstancesFunc func(context.Context, models.ListSQLInstancesRequest) (models.ListSQLInstancesResponse, error)

	// Pub/Sub
	ListTopicsFunc         func(context.Context, models.ListTopicsRequest) (models.ListTopicsResponse, error)
	InspectTopicHealthFunc func(context.Context, models.InspectTopicHealthRequest) (models.TopicHealthReport, error)
	ListSubscriptionsFunc  func(context.Context, models.ListSubscriptionsRequest) (models.ListSubscriptionsResponse, error)

	// Logging
	QueryRecentLogsFunc func(context.Context, models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error)

	// Monitoring
	GetMetricsFunc               func(context.Context, models.GetMetricsRequest) (models.GetMetricsResponse, error)
	ListMetricDescriptorsFunc    func(context.Context, models.ListMetricDescriptorsRequest) (models.ListMetricDescriptorsResponse, error)
	ListTraceServicesFunc        func(context.Context, models.ListTraceServicesRequest) (models.ListTraceServicesResponse, error)
	ListAlertPoliciesFunc        func(context.Context, models.ListAlertPoliciesRequest) (models.ListAlertPoliciesResponse, error)
	ListUptimeChecksFunc         func(context.Context, models.ListUptimeChecksRequest) (models.ListUptimeChecksResponse, error)
	ListSLOsFunc                 func(context.Context, models.ListSLOsRequest) (models.ListSLOsResponse, error)
	ListDashboardsFunc           func(context.Context, models.ListDashboardsRequest) (models.ListDashboardsResponse, error)
	ListTraceDependencyEdgesFunc func(context.Context, models.ListTraceDependencyEdgesRequest) (models.ListTraceDependencyEdgesResponse, error)

	// IAM
	TestPermissionsFunc        func(context.Context, models.TestPermissionsRequest) (models.TestPermissionsResponse, error)
	GetResourceIAMBindingsFunc func(context.Context, models.GetResourceIAMBindingsRequest) (models.GetResourceIAMBindingsResponse, error)
	ListServiceAccountsFunc    func(context.Context, models.ListServiceAccountsRequest) (models.ListServiceAccountsResponse, error)

	// Topology
	GetServiceTopologyFunc func(context.Context, models.GetServiceTopologyRequest) (models.ServiceTopologyReport, error)

	// Aura
	GetAuraScoreFunc          func(context.Context, models.GetAuraScoreRequest) (models.AuraReport, error)
	GetProjectAuraSummaryFunc func(context.Context, models.ProjectAuraSummaryRequest) (models.ProjectAuraSummaryResponse, error)

	// BigQuery
	ListDatasetsFunc   func(context.Context, models.ListDatasetsRequest) (models.ListDatasetsResponse, error)
	ListTablesFunc     func(context.Context, models.ListTablesRequest) (models.ListTablesResponse, error)
	GetTableSchemaFunc func(context.Context, models.GetTableSchemaRequest) (models.TableSchemaResponse, error)

	// Storage
	ListBucketsFunc       func(context.Context, models.ListBucketsRequest) (models.ListBucketsResponse, error)
	GetBucketMetadataFunc func(context.Context, models.GetBucketMetadataRequest) (models.BucketMetadataResponse, error)
	ListBucketObjectsFunc func(context.Context, models.ListBucketObjectsRequest) (models.ListBucketObjectsResponse, error)

	// Serverless Graph
	ExportServerlessGraphFunc func(context.Context, models.ExportServerlessGraphRequest) (models.ServerlessGraph, error)

	// Datastores
	ListSpannerInstancesFunc     func(context.Context, models.ListSpannerInstancesRequest) (models.ListSpannerInstancesResponse, error)
	ListAlloyDBClustersFunc      func(context.Context, models.ListAlloyDBClustersRequest) (models.ListAlloyDBClustersResponse, error)
	ListFirestoreDatabasesFunc   func(context.Context, models.ListFirestoreDatabasesRequest) (models.ListFirestoreDatabasesResponse, error)
	ListMemorystoreInstancesFunc func(context.Context, models.ListMemorystoreInstancesRequest) (models.ListMemorystoreInstancesResponse, error)

	// Supply Chain
	ListArtifactRegistryReposFunc      func(context.Context, models.ListArtifactRegistryReposRequest) (models.ListArtifactRegistryReposResponse, error)
	ListArtifactRegistryImagesFunc     func(context.Context, models.ListArtifactRegistryImagesRequest) (models.ListArtifactRegistryImagesResponse, error)
	ListCloudBuildTriggersFunc         func(context.Context, models.ListCloudBuildTriggersRequest) (models.ListCloudBuildTriggersResponse, error)
	ListServiceDirectoryNamespacesFunc func(context.Context, models.ListServiceDirectoryNamespacesRequest) (models.ListServiceDirectoryNamespacesResponse, error)

	// Tagging
	ListTaggedResourcesFunc func(context.Context, models.ListTaggedResourcesRequest) (models.ListTaggedResourcesResponse, error)

	// Coverage
	GetObservabilityCoverageFunc func(context.Context, models.GetObservabilityCoverageRequest) (models.ObservabilityCoverageResponse, error)

	// ArchGraph
	ExportArchitectureGraphFunc func(context.Context, models.ExportArchitectureGraphRequest) (models.ServerlessGraph, error)

	// Additional Aura scores (added in main after branch cut)
	GetGKEAuraScoreFunc func(context.Context, models.GetGKEAuraScoreRequest) (models.GKEAuraReport, error)
	GetGCSAuraScoreFunc func(context.Context, models.GetGCSAuraScoreRequest) (models.GCSAuraReport, error)
}

func (f *FakeGCPService) ListClusters(ctx context.Context, req models.ListClustersRequest) (models.ListClustersResponse, error) {
	if f.ListClustersFunc != nil {
		return f.ListClustersFunc(ctx, req)
	}
	return models.ListClustersResponse{}, nil
}
func (f *FakeGCPService) GetClusterDetails(ctx context.Context, req models.GetClusterDetailsRequest) (models.ClusterDetails, error) {
	if f.GetClusterDetailsFunc != nil {
		return f.GetClusterDetailsFunc(ctx, req)
	}
	return models.ClusterDetails{}, nil
}
func (f *FakeGCPService) GetClusterBottlenecks(ctx context.Context, req models.GetClusterBottlenecksRequest) (models.ClusterBottleneckReport, error) {
	if f.GetClusterBottlenecksFunc != nil {
		return f.GetClusterBottlenecksFunc(ctx, req)
	}
	return models.ClusterBottleneckReport{}, nil
}
func (f *FakeGCPService) ScaleDeployment(ctx context.Context, req models.ScaleDeploymentRequest) (models.ScaleDeploymentResponse, error) {
	if f.ScaleDeploymentFunc != nil {
		return f.ScaleDeploymentFunc(ctx, req)
	}
	return models.ScaleDeploymentResponse{}, nil
}
func (f *FakeGCPService) ListGKEWorkloads(ctx context.Context, req models.ListGKEWorkloadsRequest) (models.ListGKEWorkloadsResponse, error) {
	if f.ListGKEWorkloadsFunc != nil {
		return f.ListGKEWorkloadsFunc(ctx, req)
	}
	return models.ListGKEWorkloadsResponse{}, nil
}
func (f *FakeGCPService) GetGKEWorkloadDetails(ctx context.Context, req models.GetGKEWorkloadDetailsRequest) (models.GKEWorkloadDetails, error) {
	if f.GetGKEWorkloadDetailsFunc != nil {
		return f.GetGKEWorkloadDetailsFunc(ctx, req)
	}
	return models.GKEWorkloadDetails{}, nil
}
func (f *FakeGCPService) ListGKEServices(ctx context.Context, req models.ListGKEServicesRequest) (models.ListGKEServicesResponse, error) {
	if f.ListGKEServicesFunc != nil {
		return f.ListGKEServicesFunc(ctx, req)
	}
	return models.ListGKEServicesResponse{}, nil
}
func (f *FakeGCPService) ListGKEIngresses(ctx context.Context, req models.ListGKEIngressesRequest) (models.ListGKEIngressesResponse, error) {
	if f.ListGKEIngressesFunc != nil {
		return f.ListGKEIngressesFunc(ctx, req)
	}
	return models.ListGKEIngressesResponse{}, nil
}
func (f *FakeGCPService) ListGKENetworkPolicies(ctx context.Context, req models.ListGKENetworkPoliciesRequest) (models.ListGKENetworkPoliciesResponse, error) {
	if f.ListGKENetworkPoliciesFunc != nil {
		return f.ListGKENetworkPoliciesFunc(ctx, req)
	}
	return models.ListGKENetworkPoliciesResponse{}, nil
}
func (f *FakeGCPService) GetGKEMeshTopology(ctx context.Context, req models.GetGKEMeshTopologyRequest) (models.GKEMeshTopologyResponse, error) {
	if f.GetGKEMeshTopologyFunc != nil {
		return f.GetGKEMeshTopologyFunc(ctx, req)
	}
	return models.GKEMeshTopologyResponse{}, nil
}
func (f *FakeGCPService) ListLoadBalancers(ctx context.Context, req models.ListLoadBalancersRequest) (models.ListLoadBalancersResponse, error) {
	if f.ListLoadBalancersFunc != nil {
		return f.ListLoadBalancersFunc(ctx, req)
	}
	return models.ListLoadBalancersResponse{}, nil
}
func (f *FakeGCPService) ListURLMaps(ctx context.Context, req models.ListURLMapsRequest) (models.ListURLMapsResponse, error) {
	if f.ListURLMapsFunc != nil {
		return f.ListURLMapsFunc(ctx, req)
	}
	return models.ListURLMapsResponse{}, nil
}
func (f *FakeGCPService) ListNEGs(ctx context.Context, req models.ListNEGsRequest) (models.ListNEGsResponse, error) {
	if f.ListNEGsFunc != nil {
		return f.ListNEGsFunc(ctx, req)
	}
	return models.ListNEGsResponse{}, nil
}
func (f *FakeGCPService) ListAPIGateways(ctx context.Context, req models.ListAPIGatewaysRequest) (models.ListAPIGatewaysResponse, error) {
	if f.ListAPIGatewaysFunc != nil {
		return f.ListAPIGatewaysFunc(ctx, req)
	}
	return models.ListAPIGatewaysResponse{}, nil
}
func (f *FakeGCPService) ListVPCNetworks(ctx context.Context, req models.ListVPCNetworksRequest) (models.ListVPCNetworksResponse, error) {
	if f.ListVPCNetworksFunc != nil {
		return f.ListVPCNetworksFunc(ctx, req)
	}
	return models.ListVPCNetworksResponse{}, nil
}
func (f *FakeGCPService) ListVPCSubnets(ctx context.Context, req models.ListVPCSubnetsRequest) (models.ListVPCSubnetsResponse, error) {
	if f.ListVPCSubnetsFunc != nil {
		return f.ListVPCSubnetsFunc(ctx, req)
	}
	return models.ListVPCSubnetsResponse{}, nil
}
func (f *FakeGCPService) ListPSCEndpoints(ctx context.Context, req models.ListPSCEndpointsRequest) (models.ListPSCEndpointsResponse, error) {
	if f.ListPSCEndpointsFunc != nil {
		return f.ListPSCEndpointsFunc(ctx, req)
	}
	return models.ListPSCEndpointsResponse{}, nil
}
func (f *FakeGCPService) ListServices(ctx context.Context, req models.ListServicesRequest) (models.ListServicesResponse, error) {
	if f.ListServicesFunc != nil {
		return f.ListServicesFunc(ctx, req)
	}
	return models.ListServicesResponse{}, nil
}
func (f *FakeGCPService) GetServiceDetails(ctx context.Context, req models.GetServiceDetailsRequest) (models.ServiceDetails, error) {
	if f.GetServiceDetailsFunc != nil {
		return f.GetServiceDetailsFunc(ctx, req)
	}
	return models.ServiceDetails{}, nil
}
func (f *FakeGCPService) UpdateTraffic(ctx context.Context, req models.UpdateTrafficRequest) (models.UpdateTrafficResponse, error) {
	if f.UpdateTrafficFunc != nil {
		return f.UpdateTrafficFunc(ctx, req)
	}
	return models.UpdateTrafficResponse{}, nil
}
func (f *FakeGCPService) ListJobs(ctx context.Context, req models.ListJobsRequest) (models.ListJobsResponse, error) {
	if f.ListJobsFunc != nil {
		return f.ListJobsFunc(ctx, req)
	}
	return models.ListJobsResponse{}, nil
}
func (f *FakeGCPService) GetJobDetails(ctx context.Context, req models.GetJobDetailsRequest) (models.JobDetails, error) {
	if f.GetJobDetailsFunc != nil {
		return f.GetJobDetailsFunc(ctx, req)
	}
	return models.JobDetails{}, nil
}
func (f *FakeGCPService) ListJobExecutions(ctx context.Context, req models.ListJobExecutionsRequest) (models.ListJobExecutionsResponse, error) {
	if f.ListJobExecutionsFunc != nil {
		return f.ListJobExecutionsFunc(ctx, req)
	}
	return models.ListJobExecutionsResponse{}, nil
}
func (f *FakeGCPService) ListFunctions(ctx context.Context, req models.ListFunctionsRequest) (models.ListFunctionsResponse, error) {
	if f.ListFunctionsFunc != nil {
		return f.ListFunctionsFunc(ctx, req)
	}
	return models.ListFunctionsResponse{}, nil
}
func (f *FakeGCPService) GetFunctionDetails(ctx context.Context, req models.GetFunctionDetailsRequest) (models.FunctionDetails, error) {
	if f.GetFunctionDetailsFunc != nil {
		return f.GetFunctionDetailsFunc(ctx, req)
	}
	return models.FunctionDetails{}, nil
}
func (f *FakeGCPService) ListTriggers(ctx context.Context, req models.ListTriggersRequest) (models.ListTriggersResponse, error) {
	if f.ListTriggersFunc != nil {
		return f.ListTriggersFunc(ctx, req)
	}
	return models.ListTriggersResponse{}, nil
}
func (f *FakeGCPService) GetTrigger(ctx context.Context, req models.GetTriggerRequest) (models.TriggerDetails, error) {
	if f.GetTriggerFunc != nil {
		return f.GetTriggerFunc(ctx, req)
	}
	return models.TriggerDetails{}, nil
}
func (f *FakeGCPService) ListSchedulerJobs(ctx context.Context, req models.ListSchedulerJobsRequest) (models.ListSchedulerJobsResponse, error) {
	if f.ListSchedulerJobsFunc != nil {
		return f.ListSchedulerJobsFunc(ctx, req)
	}
	return models.ListSchedulerJobsResponse{}, nil
}
func (f *FakeGCPService) ListWorkflows(ctx context.Context, req models.ListWorkflowsRequest) (models.ListWorkflowsResponse, error) {
	if f.ListWorkflowsFunc != nil {
		return f.ListWorkflowsFunc(ctx, req)
	}
	return models.ListWorkflowsResponse{}, nil
}
func (f *FakeGCPService) ListWorkflowExecutions(ctx context.Context, req models.ListWorkflowExecutionsRequest) (models.ListWorkflowExecutionsResponse, error) {
	if f.ListWorkflowExecutionsFunc != nil {
		return f.ListWorkflowExecutionsFunc(ctx, req)
	}
	return models.ListWorkflowExecutionsResponse{}, nil
}
func (f *FakeGCPService) ListTaskQueues(ctx context.Context, req models.ListTaskQueuesRequest) (models.ListTaskQueuesResponse, error) {
	if f.ListTaskQueuesFunc != nil {
		return f.ListTaskQueuesFunc(ctx, req)
	}
	return models.ListTaskQueuesResponse{}, nil
}
func (f *FakeGCPService) ListSecrets(ctx context.Context, req models.ListSecretsRequest) (models.ListSecretsResponse, error) {
	if f.ListSecretsFunc != nil {
		return f.ListSecretsFunc(ctx, req)
	}
	return models.ListSecretsResponse{}, nil
}
func (f *FakeGCPService) ListVPCConnectors(ctx context.Context, req models.ListVPCConnectorsRequest) (models.ListVPCConnectorsResponse, error) {
	if f.ListVPCConnectorsFunc != nil {
		return f.ListVPCConnectorsFunc(ctx, req)
	}
	return models.ListVPCConnectorsResponse{}, nil
}
func (f *FakeGCPService) ListSQLInstances(ctx context.Context, req models.ListSQLInstancesRequest) (models.ListSQLInstancesResponse, error) {
	if f.ListSQLInstancesFunc != nil {
		return f.ListSQLInstancesFunc(ctx, req)
	}
	return models.ListSQLInstancesResponse{}, nil
}
func (f *FakeGCPService) ListTopics(ctx context.Context, req models.ListTopicsRequest) (models.ListTopicsResponse, error) {
	if f.ListTopicsFunc != nil {
		return f.ListTopicsFunc(ctx, req)
	}
	return models.ListTopicsResponse{}, nil
}
func (f *FakeGCPService) InspectTopicHealth(ctx context.Context, req models.InspectTopicHealthRequest) (models.TopicHealthReport, error) {
	if f.InspectTopicHealthFunc != nil {
		return f.InspectTopicHealthFunc(ctx, req)
	}
	return models.TopicHealthReport{}, nil
}
func (f *FakeGCPService) ListSubscriptions(ctx context.Context, req models.ListSubscriptionsRequest) (models.ListSubscriptionsResponse, error) {
	if f.ListSubscriptionsFunc != nil {
		return f.ListSubscriptionsFunc(ctx, req)
	}
	return models.ListSubscriptionsResponse{}, nil
}
func (f *FakeGCPService) QueryRecentLogs(ctx context.Context, req models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error) {
	if f.QueryRecentLogsFunc != nil {
		return f.QueryRecentLogsFunc(ctx, req)
	}
	return models.QueryRecentLogsResponse{}, nil
}
func (f *FakeGCPService) GetMetrics(ctx context.Context, req models.GetMetricsRequest) (models.GetMetricsResponse, error) {
	if f.GetMetricsFunc != nil {
		return f.GetMetricsFunc(ctx, req)
	}
	return models.GetMetricsResponse{}, nil
}
func (f *FakeGCPService) ListMetricDescriptors(ctx context.Context, req models.ListMetricDescriptorsRequest) (models.ListMetricDescriptorsResponse, error) {
	if f.ListMetricDescriptorsFunc != nil {
		return f.ListMetricDescriptorsFunc(ctx, req)
	}
	return models.ListMetricDescriptorsResponse{}, nil
}
func (f *FakeGCPService) ListTraceServices(ctx context.Context, req models.ListTraceServicesRequest) (models.ListTraceServicesResponse, error) {
	if f.ListTraceServicesFunc != nil {
		return f.ListTraceServicesFunc(ctx, req)
	}
	return models.ListTraceServicesResponse{}, nil
}
func (f *FakeGCPService) ListAlertPolicies(ctx context.Context, req models.ListAlertPoliciesRequest) (models.ListAlertPoliciesResponse, error) {
	if f.ListAlertPoliciesFunc != nil {
		return f.ListAlertPoliciesFunc(ctx, req)
	}
	return models.ListAlertPoliciesResponse{}, nil
}
func (f *FakeGCPService) ListUptimeChecks(ctx context.Context, req models.ListUptimeChecksRequest) (models.ListUptimeChecksResponse, error) {
	if f.ListUptimeChecksFunc != nil {
		return f.ListUptimeChecksFunc(ctx, req)
	}
	return models.ListUptimeChecksResponse{}, nil
}
func (f *FakeGCPService) ListSLOs(ctx context.Context, req models.ListSLOsRequest) (models.ListSLOsResponse, error) {
	if f.ListSLOsFunc != nil {
		return f.ListSLOsFunc(ctx, req)
	}
	return models.ListSLOsResponse{}, nil
}
func (f *FakeGCPService) ListDashboards(ctx context.Context, req models.ListDashboardsRequest) (models.ListDashboardsResponse, error) {
	if f.ListDashboardsFunc != nil {
		return f.ListDashboardsFunc(ctx, req)
	}
	return models.ListDashboardsResponse{}, nil
}
func (f *FakeGCPService) ListTraceDependencyEdges(ctx context.Context, req models.ListTraceDependencyEdgesRequest) (models.ListTraceDependencyEdgesResponse, error) {
	if f.ListTraceDependencyEdgesFunc != nil {
		return f.ListTraceDependencyEdgesFunc(ctx, req)
	}
	return models.ListTraceDependencyEdgesResponse{}, nil
}
func (f *FakeGCPService) TestPermissions(ctx context.Context, req models.TestPermissionsRequest) (models.TestPermissionsResponse, error) {
	if f.TestPermissionsFunc != nil {
		return f.TestPermissionsFunc(ctx, req)
	}
	return models.TestPermissionsResponse{}, nil
}
func (f *FakeGCPService) GetResourceIAMBindings(ctx context.Context, req models.GetResourceIAMBindingsRequest) (models.GetResourceIAMBindingsResponse, error) {
	if f.GetResourceIAMBindingsFunc != nil {
		return f.GetResourceIAMBindingsFunc(ctx, req)
	}
	return models.GetResourceIAMBindingsResponse{}, nil
}
func (f *FakeGCPService) ListServiceAccounts(ctx context.Context, req models.ListServiceAccountsRequest) (models.ListServiceAccountsResponse, error) {
	if f.ListServiceAccountsFunc != nil {
		return f.ListServiceAccountsFunc(ctx, req)
	}
	return models.ListServiceAccountsResponse{}, nil
}
func (f *FakeGCPService) GetServiceTopology(ctx context.Context, req models.GetServiceTopologyRequest) (models.ServiceTopologyReport, error) {
	if f.GetServiceTopologyFunc != nil {
		return f.GetServiceTopologyFunc(ctx, req)
	}
	return models.ServiceTopologyReport{}, nil
}
func (f *FakeGCPService) GetAuraScore(ctx context.Context, req models.GetAuraScoreRequest) (models.AuraReport, error) {
	if f.GetAuraScoreFunc != nil {
		return f.GetAuraScoreFunc(ctx, req)
	}
	return models.AuraReport{}, nil
}
func (f *FakeGCPService) GetProjectAuraSummary(ctx context.Context, req models.ProjectAuraSummaryRequest) (models.ProjectAuraSummaryResponse, error) {
	if f.GetProjectAuraSummaryFunc != nil {
		return f.GetProjectAuraSummaryFunc(ctx, req)
	}
	return models.ProjectAuraSummaryResponse{}, nil
}
func (f *FakeGCPService) ListDatasets(ctx context.Context, req models.ListDatasetsRequest) (models.ListDatasetsResponse, error) {
	if f.ListDatasetsFunc != nil {
		return f.ListDatasetsFunc(ctx, req)
	}
	return models.ListDatasetsResponse{}, nil
}
func (f *FakeGCPService) ListTables(ctx context.Context, req models.ListTablesRequest) (models.ListTablesResponse, error) {
	if f.ListTablesFunc != nil {
		return f.ListTablesFunc(ctx, req)
	}
	return models.ListTablesResponse{}, nil
}
func (f *FakeGCPService) GetTableSchema(ctx context.Context, req models.GetTableSchemaRequest) (models.TableSchemaResponse, error) {
	if f.GetTableSchemaFunc != nil {
		return f.GetTableSchemaFunc(ctx, req)
	}
	return models.TableSchemaResponse{}, nil
}
func (f *FakeGCPService) ListBuckets(ctx context.Context, req models.ListBucketsRequest) (models.ListBucketsResponse, error) {
	if f.ListBucketsFunc != nil {
		return f.ListBucketsFunc(ctx, req)
	}
	return models.ListBucketsResponse{}, nil
}
func (f *FakeGCPService) GetBucketMetadata(ctx context.Context, req models.GetBucketMetadataRequest) (models.BucketMetadataResponse, error) {
	if f.GetBucketMetadataFunc != nil {
		return f.GetBucketMetadataFunc(ctx, req)
	}
	return models.BucketMetadataResponse{}, nil
}
func (f *FakeGCPService) ListBucketObjects(ctx context.Context, req models.ListBucketObjectsRequest) (models.ListBucketObjectsResponse, error) {
	if f.ListBucketObjectsFunc != nil {
		return f.ListBucketObjectsFunc(ctx, req)
	}
	return models.ListBucketObjectsResponse{}, nil
}
func (f *FakeGCPService) ExportServerlessGraph(ctx context.Context, req models.ExportServerlessGraphRequest) (models.ServerlessGraph, error) {
	if f.ExportServerlessGraphFunc != nil {
		return f.ExportServerlessGraphFunc(ctx, req)
	}
	return models.ServerlessGraph{}, nil
}
func (f *FakeGCPService) ListSpannerInstances(ctx context.Context, req models.ListSpannerInstancesRequest) (models.ListSpannerInstancesResponse, error) {
	if f.ListSpannerInstancesFunc != nil {
		return f.ListSpannerInstancesFunc(ctx, req)
	}
	return models.ListSpannerInstancesResponse{}, nil
}
func (f *FakeGCPService) ListAlloyDBClusters(ctx context.Context, req models.ListAlloyDBClustersRequest) (models.ListAlloyDBClustersResponse, error) {
	if f.ListAlloyDBClustersFunc != nil {
		return f.ListAlloyDBClustersFunc(ctx, req)
	}
	return models.ListAlloyDBClustersResponse{}, nil
}
func (f *FakeGCPService) ListFirestoreDatabases(ctx context.Context, req models.ListFirestoreDatabasesRequest) (models.ListFirestoreDatabasesResponse, error) {
	if f.ListFirestoreDatabasesFunc != nil {
		return f.ListFirestoreDatabasesFunc(ctx, req)
	}
	return models.ListFirestoreDatabasesResponse{}, nil
}
func (f *FakeGCPService) ListMemorystoreInstances(ctx context.Context, req models.ListMemorystoreInstancesRequest) (models.ListMemorystoreInstancesResponse, error) {
	if f.ListMemorystoreInstancesFunc != nil {
		return f.ListMemorystoreInstancesFunc(ctx, req)
	}
	return models.ListMemorystoreInstancesResponse{}, nil
}
func (f *FakeGCPService) ListArtifactRegistryRepos(ctx context.Context, req models.ListArtifactRegistryReposRequest) (models.ListArtifactRegistryReposResponse, error) {
	if f.ListArtifactRegistryReposFunc != nil {
		return f.ListArtifactRegistryReposFunc(ctx, req)
	}
	return models.ListArtifactRegistryReposResponse{}, nil
}
func (f *FakeGCPService) ListArtifactRegistryImages(ctx context.Context, req models.ListArtifactRegistryImagesRequest) (models.ListArtifactRegistryImagesResponse, error) {
	if f.ListArtifactRegistryImagesFunc != nil {
		return f.ListArtifactRegistryImagesFunc(ctx, req)
	}
	return models.ListArtifactRegistryImagesResponse{}, nil
}
func (f *FakeGCPService) ListCloudBuildTriggers(ctx context.Context, req models.ListCloudBuildTriggersRequest) (models.ListCloudBuildTriggersResponse, error) {
	if f.ListCloudBuildTriggersFunc != nil {
		return f.ListCloudBuildTriggersFunc(ctx, req)
	}
	return models.ListCloudBuildTriggersResponse{}, nil
}
func (f *FakeGCPService) ListServiceDirectoryNamespaces(ctx context.Context, req models.ListServiceDirectoryNamespacesRequest) (models.ListServiceDirectoryNamespacesResponse, error) {
	if f.ListServiceDirectoryNamespacesFunc != nil {
		return f.ListServiceDirectoryNamespacesFunc(ctx, req)
	}
	return models.ListServiceDirectoryNamespacesResponse{}, nil
}
func (f *FakeGCPService) ListTaggedResources(ctx context.Context, req models.ListTaggedResourcesRequest) (models.ListTaggedResourcesResponse, error) {
	if f.ListTaggedResourcesFunc != nil {
		return f.ListTaggedResourcesFunc(ctx, req)
	}
	return models.ListTaggedResourcesResponse{}, nil
}
func (f *FakeGCPService) GetObservabilityCoverage(ctx context.Context, req models.GetObservabilityCoverageRequest) (models.ObservabilityCoverageResponse, error) {
	if f.GetObservabilityCoverageFunc != nil {
		return f.GetObservabilityCoverageFunc(ctx, req)
	}
	return models.ObservabilityCoverageResponse{}, nil
}
func (f *FakeGCPService) ExportArchitectureGraph(ctx context.Context, req models.ExportArchitectureGraphRequest) (models.ServerlessGraph, error) {
	if f.ExportArchitectureGraphFunc != nil {
		return f.ExportArchitectureGraphFunc(ctx, req)
	}
	return models.ServerlessGraph{}, nil
}
func (f *FakeGCPService) GetGKEAuraScore(ctx context.Context, req models.GetGKEAuraScoreRequest) (models.GKEAuraReport, error) {
	if f.GetGKEAuraScoreFunc != nil {
		return f.GetGKEAuraScoreFunc(ctx, req)
	}
	return models.GKEAuraReport{}, nil
}
func (f *FakeGCPService) GetGCSAuraScore(ctx context.Context, req models.GetGCSAuraScoreRequest) (models.GCSAuraReport, error) {
	if f.GetGCSAuraScoreFunc != nil {
		return f.GetGCSAuraScoreFunc(ctx, req)
	}
	return models.GCSAuraReport{}, nil
}
func (f *FakeGCPService) ExportRecommendationsToBQ(_ context.Context, _ models.ExportRecommendationsToBQRequest) (models.ExportRecommendationsToBQResponse, error) {
	return models.ExportRecommendationsToBQResponse{}, nil
}
