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
