package mcp

import (
	"context"
	"log/slog"
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// mockSvc is a no-op implementation of ports.GCPService for wiring tests.
type mockSvc struct{}

func (m *mockSvc) ListClusters(_ context.Context, _ models.ListClustersRequest) (models.ListClustersResponse, error) {
	return models.ListClustersResponse{}, nil
}
func (m *mockSvc) GetClusterDetails(_ context.Context, _ models.GetClusterDetailsRequest) (models.ClusterDetails, error) {
	return models.ClusterDetails{}, nil
}
func (m *mockSvc) GetClusterBottlenecks(_ context.Context, _ models.GetClusterBottlenecksRequest) (models.ClusterBottleneckReport, error) {
	return models.ClusterBottleneckReport{}, nil
}
func (m *mockSvc) ScaleDeployment(_ context.Context, _ models.ScaleDeploymentRequest) (models.ScaleDeploymentResponse, error) {
	return models.ScaleDeploymentResponse{}, nil
}
func (m *mockSvc) ListServices(_ context.Context, _ models.ListServicesRequest) (models.ListServicesResponse, error) {
	return models.ListServicesResponse{}, nil
}
func (m *mockSvc) GetServiceDetails(_ context.Context, _ models.GetServiceDetailsRequest) (models.ServiceDetails, error) {
	return models.ServiceDetails{}, nil
}
func (m *mockSvc) UpdateTraffic(_ context.Context, _ models.UpdateTrafficRequest) (models.UpdateTrafficResponse, error) {
	return models.UpdateTrafficResponse{}, nil
}
func (m *mockSvc) ListTopics(_ context.Context, _ models.ListTopicsRequest) (models.ListTopicsResponse, error) {
	return models.ListTopicsResponse{}, nil
}
func (m *mockSvc) InspectTopicHealth(_ context.Context, _ models.InspectTopicHealthRequest) (models.TopicHealthReport, error) {
	return models.TopicHealthReport{}, nil
}
func (m *mockSvc) QueryRecentLogs(_ context.Context, _ models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error) {
	return models.QueryRecentLogsResponse{}, nil
}
func (m *mockSvc) GetMetrics(_ context.Context, _ models.GetMetricsRequest) (models.GetMetricsResponse, error) {
	return models.GetMetricsResponse{}, nil
}
func (m *mockSvc) TestPermissions(_ context.Context, _ models.TestPermissionsRequest) (models.TestPermissionsResponse, error) {
	return models.TestPermissionsResponse{}, nil
}

func TestServerRegistersAllTools(t *testing.T) {
	s := New(&mockSvc{}, slog.Default(), "test")

	expected := []string{
		"gcp_gke_list_clusters",
		"gcp_gke_get_cluster_details",
		"gcp_gke_get_cluster_bottlenecks",
		"gcp_gke_scale_deployment",
		"gcp_cloudrun_list_services",
		"gcp_cloudrun_get_service_details",
		"gcp_cloudrun_update_traffic",
		"gcp_pubsub_list_topics",
		"gcp_pubsub_inspect_topic_health",
		"gcp_logging_query_recent",
		"gcp_monitoring_get_metrics",
		"gcp_iam_test_permissions",
	}

	registered := s.ListTools()
	for _, name := range expected {
		if _, ok := registered[name]; !ok {
			t.Errorf("tool %q not registered", name)
		}
	}

	if got := len(registered); got != len(expected) {
		t.Errorf("expected %d tools, got %d", len(expected), got)
	}
}
