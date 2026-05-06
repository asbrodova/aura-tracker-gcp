package tools

import (
	"context"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// mockGCPService is a hand-written test double that implements ports.GCPService.
// Set the return* fields before calling.
type mockGCPService struct {
	returnListClusters         models.ListClustersResponse
	returnListClustersErr      error
	returnGetClusterDetails    models.ClusterDetails
	returnGetClusterDetailsErr error
	returnGetBottlenecks       models.ClusterBottleneckReport
	returnGetBottlenecksErr    error
	returnScaleDeployment      models.ScaleDeploymentResponse
	returnScaleDeploymentErr   error
	returnListServices         models.ListServicesResponse
	returnListServicesErr      error
	returnGetServiceDetails    models.ServiceDetails
	returnGetServiceDetailsErr error
	returnUpdateTraffic        models.UpdateTrafficResponse
	returnUpdateTrafficErr     error
	returnListTopics           models.ListTopicsResponse
	returnListTopicsErr        error
	returnTopicHealth          models.TopicHealthReport
	returnTopicHealthErr       error
	returnQueryLogs            models.QueryRecentLogsResponse
	returnQueryLogsErr         error
	returnGetMetrics           models.GetMetricsResponse
	returnGetMetricsErr        error
	returnTestPermissions      models.TestPermissionsResponse
	returnTestPermissionsErr   error
}

func (m *mockGCPService) ListClusters(_ context.Context, _ models.ListClustersRequest) (models.ListClustersResponse, error) {
	return m.returnListClusters, m.returnListClustersErr
}
func (m *mockGCPService) GetClusterDetails(_ context.Context, _ models.GetClusterDetailsRequest) (models.ClusterDetails, error) {
	return m.returnGetClusterDetails, m.returnGetClusterDetailsErr
}
func (m *mockGCPService) GetClusterBottlenecks(_ context.Context, _ models.GetClusterBottlenecksRequest) (models.ClusterBottleneckReport, error) {
	return m.returnGetBottlenecks, m.returnGetBottlenecksErr
}
func (m *mockGCPService) ScaleDeployment(_ context.Context, _ models.ScaleDeploymentRequest) (models.ScaleDeploymentResponse, error) {
	return m.returnScaleDeployment, m.returnScaleDeploymentErr
}
func (m *mockGCPService) ListServices(_ context.Context, _ models.ListServicesRequest) (models.ListServicesResponse, error) {
	return m.returnListServices, m.returnListServicesErr
}
func (m *mockGCPService) GetServiceDetails(_ context.Context, _ models.GetServiceDetailsRequest) (models.ServiceDetails, error) {
	return m.returnGetServiceDetails, m.returnGetServiceDetailsErr
}
func (m *mockGCPService) UpdateTraffic(_ context.Context, _ models.UpdateTrafficRequest) (models.UpdateTrafficResponse, error) {
	return m.returnUpdateTraffic, m.returnUpdateTrafficErr
}
func (m *mockGCPService) ListJobs(_ context.Context, _ models.ListJobsRequest) (models.ListJobsResponse, error) {
	return models.ListJobsResponse{}, nil
}
func (m *mockGCPService) GetJobDetails(_ context.Context, _ models.GetJobDetailsRequest) (models.JobDetails, error) {
	return models.JobDetails{}, nil
}
func (m *mockGCPService) ListJobExecutions(_ context.Context, _ models.ListJobExecutionsRequest) (models.ListJobExecutionsResponse, error) {
	return models.ListJobExecutionsResponse{}, nil
}
func (m *mockGCPService) ListTopics(_ context.Context, _ models.ListTopicsRequest) (models.ListTopicsResponse, error) {
	return m.returnListTopics, m.returnListTopicsErr
}
func (m *mockGCPService) InspectTopicHealth(_ context.Context, _ models.InspectTopicHealthRequest) (models.TopicHealthReport, error) {
	return m.returnTopicHealth, m.returnTopicHealthErr
}
func (m *mockGCPService) QueryRecentLogs(_ context.Context, _ models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error) {
	return m.returnQueryLogs, m.returnQueryLogsErr
}
func (m *mockGCPService) GetMetrics(_ context.Context, _ models.GetMetricsRequest) (models.GetMetricsResponse, error) {
	return m.returnGetMetrics, m.returnGetMetricsErr
}
func (m *mockGCPService) TestPermissions(_ context.Context, _ models.TestPermissionsRequest) (models.TestPermissionsResponse, error) {
	return m.returnTestPermissions, m.returnTestPermissionsErr
}
func (m *mockGCPService) GetServiceTopology(_ context.Context, _ models.GetServiceTopologyRequest) (models.ServiceTopologyReport, error) {
	return models.ServiceTopologyReport{}, nil
}
func (m *mockGCPService) GetAuraScore(_ context.Context, _ models.GetAuraScoreRequest) (models.AuraReport, error) {
	return models.AuraReport{}, nil
}
func (m *mockGCPService) GetProjectAuraSummary(_ context.Context, _ models.ProjectAuraSummaryRequest) (models.ProjectAuraSummaryResponse, error) {
	return models.ProjectAuraSummaryResponse{}, nil
}
func (m *mockGCPService) ListDatasets(_ context.Context, _ models.ListDatasetsRequest) (models.ListDatasetsResponse, error) {
	return models.ListDatasetsResponse{}, nil
}
func (m *mockGCPService) ListTables(_ context.Context, _ models.ListTablesRequest) (models.ListTablesResponse, error) {
	return models.ListTablesResponse{}, nil
}
func (m *mockGCPService) GetTableSchema(_ context.Context, _ models.GetTableSchemaRequest) (models.TableSchemaResponse, error) {
	return models.TableSchemaResponse{}, nil
}
func (m *mockGCPService) ListBuckets(_ context.Context, _ models.ListBucketsRequest) (models.ListBucketsResponse, error) {
	return models.ListBucketsResponse{}, nil
}
func (m *mockGCPService) GetBucketMetadata(_ context.Context, _ models.GetBucketMetadataRequest) (models.BucketMetadataResponse, error) {
	return models.BucketMetadataResponse{}, nil
}
func (m *mockGCPService) ListFunctions(_ context.Context, _ models.ListFunctionsRequest) (models.ListFunctionsResponse, error) {
	return models.ListFunctionsResponse{}, nil
}
func (m *mockGCPService) GetFunctionDetails(_ context.Context, _ models.GetFunctionDetailsRequest) (models.FunctionDetails, error) {
	return models.FunctionDetails{}, nil
}
func (m *mockGCPService) ListTriggers(_ context.Context, _ models.ListTriggersRequest) (models.ListTriggersResponse, error) {
	return models.ListTriggersResponse{}, nil
}
func (m *mockGCPService) GetTrigger(_ context.Context, _ models.GetTriggerRequest) (models.TriggerDetails, error) {
	return models.TriggerDetails{}, nil
}
func (m *mockGCPService) ListSchedulerJobs(_ context.Context, _ models.ListSchedulerJobsRequest) (models.ListSchedulerJobsResponse, error) {
	return models.ListSchedulerJobsResponse{}, nil
}
func (m *mockGCPService) ListWorkflows(_ context.Context, _ models.ListWorkflowsRequest) (models.ListWorkflowsResponse, error) {
	return models.ListWorkflowsResponse{}, nil
}
func (m *mockGCPService) ListWorkflowExecutions(_ context.Context, _ models.ListWorkflowExecutionsRequest) (models.ListWorkflowExecutionsResponse, error) {
	return models.ListWorkflowExecutionsResponse{}, nil
}
func (m *mockGCPService) ListTaskQueues(_ context.Context, _ models.ListTaskQueuesRequest) (models.ListTaskQueuesResponse, error) {
	return models.ListTaskQueuesResponse{}, nil
}
func (m *mockGCPService) ListSecrets(_ context.Context, _ models.ListSecretsRequest) (models.ListSecretsResponse, error) {
	return models.ListSecretsResponse{}, nil
}
func (m *mockGCPService) ListSubscriptions(_ context.Context, _ models.ListSubscriptionsRequest) (models.ListSubscriptionsResponse, error) {
	return models.ListSubscriptionsResponse{}, nil
}
func (m *mockGCPService) ListVPCConnectors(_ context.Context, _ models.ListVPCConnectorsRequest) (models.ListVPCConnectorsResponse, error) {
	return models.ListVPCConnectorsResponse{}, nil
}
func (m *mockGCPService) ListSQLInstances(_ context.Context, _ models.ListSQLInstancesRequest) (models.ListSQLInstancesResponse, error) {
	return models.ListSQLInstancesResponse{}, nil
}
func (m *mockGCPService) ListMetricDescriptors(_ context.Context, _ models.ListMetricDescriptorsRequest) (models.ListMetricDescriptorsResponse, error) {
	return models.ListMetricDescriptorsResponse{}, nil
}
func (m *mockGCPService) ListTraceServices(_ context.Context, _ models.ListTraceServicesRequest) (models.ListTraceServicesResponse, error) {
	return models.ListTraceServicesResponse{}, nil
}
func (m *mockGCPService) ExportServerlessGraph(_ context.Context, _ models.ExportServerlessGraphRequest) (models.ServerlessGraph, error) {
	return models.ServerlessGraph{}, nil
}
func (m *mockGCPService) ListGKEWorkloads(_ context.Context, _ models.ListGKEWorkloadsRequest) (models.ListGKEWorkloadsResponse, error) {
	return models.ListGKEWorkloadsResponse{}, nil
}
func (m *mockGCPService) GetGKEWorkloadDetails(_ context.Context, _ models.GetGKEWorkloadDetailsRequest) (models.GKEWorkloadDetails, error) {
	return models.GKEWorkloadDetails{}, nil
}
func (m *mockGCPService) ListGKEServices(_ context.Context, _ models.ListGKEServicesRequest) (models.ListGKEServicesResponse, error) {
	return models.ListGKEServicesResponse{}, nil
}
func (m *mockGCPService) ListGKEIngresses(_ context.Context, _ models.ListGKEIngressesRequest) (models.ListGKEIngressesResponse, error) {
	return models.ListGKEIngressesResponse{}, nil
}
func (m *mockGCPService) ListGKENetworkPolicies(_ context.Context, _ models.ListGKENetworkPoliciesRequest) (models.ListGKENetworkPoliciesResponse, error) {
	return models.ListGKENetworkPoliciesResponse{}, nil
}
