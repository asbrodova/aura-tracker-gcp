package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
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
func (m *mockSvc) GetServiceTopology(_ context.Context, _ models.GetServiceTopologyRequest) (models.ServiceTopologyReport, error) {
	return models.ServiceTopologyReport{}, nil
}
func (m *mockSvc) GetAuraScore(_ context.Context, _ models.GetAuraScoreRequest) (models.AuraReport, error) {
	return models.AuraReport{}, nil
}
func (m *mockSvc) GetProjectAuraSummary(_ context.Context, _ models.ProjectAuraSummaryRequest) (models.ProjectAuraSummaryResponse, error) {
	return models.ProjectAuraSummaryResponse{}, nil
}
func (m *mockSvc) ListDatasets(_ context.Context, _ models.ListDatasetsRequest) (models.ListDatasetsResponse, error) {
	return models.ListDatasetsResponse{}, nil
}
func (m *mockSvc) ListTables(_ context.Context, _ models.ListTablesRequest) (models.ListTablesResponse, error) {
	return models.ListTablesResponse{}, nil
}
func (m *mockSvc) GetTableSchema(_ context.Context, _ models.GetTableSchemaRequest) (models.TableSchemaResponse, error) {
	return models.TableSchemaResponse{}, nil
}
func (m *mockSvc) ListBuckets(_ context.Context, _ models.ListBucketsRequest) (models.ListBucketsResponse, error) {
	return models.ListBucketsResponse{}, nil
}
func (m *mockSvc) GetBucketMetadata(_ context.Context, _ models.GetBucketMetadataRequest) (models.BucketMetadataResponse, error) {
	return models.BucketMetadataResponse{}, nil
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
		"gcp_get_service_topology",
		"gcp_get_aura_score",
		"gcp_project_aura_summary",
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

func TestServerRegistersResourcesAndPrompts(t *testing.T) {
	s := New(&mockSvc{}, slog.Default(), "test")

	// Verify resources/list responds without error and returns the 4 static resources.
	msg := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"resources/list","params":{}}`)
	resp := s.HandleMessage(context.Background(), msg)
	raw, _ := json.Marshal(resp)
	rawStr := string(raw)
	if strings.Contains(rawStr, `"error"`) {
		t.Errorf("resources/list returned error: %s", rawStr)
	}
	for _, uri := range []string{"bigquery/datasets", "cloudrun/services", "storage/buckets", "iam/my-permissions"} {
		if !strings.Contains(rawStr, uri) {
			t.Errorf("resources/list missing resource with URI containing %q", uri)
		}
	}

	// Verify resources/templates/list returns the 6 templates.
	msg = json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"resources/templates/list","params":{}}`)
	resp = s.HandleMessage(context.Background(), msg)
	raw, _ = json.Marshal(resp)
	rawStr = string(raw)
	if strings.Contains(rawStr, `"error"`) {
		t.Errorf("resources/templates/list returned error: %s", rawStr)
	}
	for _, tmpl := range []string{
		"bigquery/{dataset}/tables",
		"bigquery/{dataset}/{table}/schema",
		"cloudrun/{region}/{service}",
		"cloudrun/{region}/{service}/revisions",
		"storage/{bucket}",
		"storage/{bucket}/objects",
	} {
		if !strings.Contains(rawStr, tmpl) {
			t.Errorf("resources/templates/list missing template containing %q", tmpl)
		}
	}

	// Verify prompts/list returns 3 prompts.
	msg = json.RawMessage(`{"jsonrpc":"2.0","id":3,"method":"prompts/list","params":{}}`)
	resp = s.HandleMessage(context.Background(), msg)
	raw, _ = json.Marshal(resp)
	rawStr = string(raw)
	if strings.Contains(rawStr, `"error"`) {
		t.Errorf("prompts/list returned error: %s", rawStr)
	}
	for _, name := range []string{"audit-security-posture", "optimize-bigquery-costs", "incident-response-helper"} {
		if !strings.Contains(rawStr, name) {
			t.Errorf("prompts/list missing prompt %q", name)
		}
	}
}

func TestFilteredRegistration(t *testing.T) {
	s := New(&mockSvc{}, slog.Default(), "test",
		WithModules(map[string]bool{"gke": true}),
	)

	registered := s.ListTools()
	gkeTools := []string{
		"gcp_gke_list_clusters",
		"gcp_gke_get_cluster_details",
		"gcp_gke_get_cluster_bottlenecks",
		"gcp_gke_scale_deployment",
	}
	if got := len(registered); got != len(gkeTools) {
		t.Errorf("expected %d tools with gke module, got %d", len(gkeTools), got)
	}
	for _, name := range gkeTools {
		if _, ok := registered[name]; !ok {
			t.Errorf("expected tool %q to be registered", name)
		}
	}
}

func TestNoneModulesRegistersZeroTools(t *testing.T) {
	s := New(&mockSvc{}, slog.Default(), "test",
		WithModules(map[string]bool{}),
	)

	if got := len(s.ListTools()); got != 0 {
		t.Errorf("expected 0 tools with empty module set, got %d", got)
	}
}

func TestPromptHandlerRequiresProjectID(t *testing.T) {
	s := New(&mockSvc{}, slog.Default(), "test")

	// Calling audit-security-posture without project_id should return an error result.
	msg := json.RawMessage(`{"jsonrpc":"2.0","id":4,"method":"prompts/get","params":{"name":"audit-security-posture","arguments":{}}}`)
	resp := s.HandleMessage(context.Background(), msg)
	raw, _ := json.Marshal(resp)
	if !strings.Contains(string(raw), "error") {
		t.Errorf("expected error for missing project_id, got: %s", string(raw))
	}
}
