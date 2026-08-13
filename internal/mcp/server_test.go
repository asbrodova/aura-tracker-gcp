package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	protocol "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/internal/anonymize"
	"github.com/asbrodova/aura-tracker-gcp/internal/costreasoning"
	"github.com/asbrodova/aura-tracker-gcp/internal/environments"
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
func (m *mockSvc) ListRevisions(_ context.Context, _ models.ListRevisionsRequest) (models.ListRevisionsResponse, error) {
	return models.ListRevisionsResponse{}, nil
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
func (m *mockSvc) SearchSecurityIAMPolicies(_ context.Context, _ models.SecurityFactsRequest) (models.SecurityIAMPolicyFacts, error) {
	return models.SecurityIAMPolicyFacts{}, nil
}
func (m *mockSvc) ListServiceAccountSecurityFacts(_ context.Context, _ models.SecurityFactsRequest) (models.ServiceAccountSecurityFacts, error) {
	return models.ServiceAccountSecurityFacts{}, nil
}
func (m *mockSvc) ListSecretSecurityFacts(_ context.Context, _ models.SecurityFactsRequest) (models.SecretSecurityFacts, error) {
	return models.SecretSecurityFacts{}, nil
}
func (m *mockSvc) ListPublicServiceSecurityFacts(_ context.Context, _ models.SecurityFactsRequest) (models.PublicServiceSecurityFacts, error) {
	return models.PublicServiceSecurityFacts{}, nil
}
func (m *mockSvc) ListFirewallSecurityFacts(_ context.Context, _ models.SecurityFactsRequest) (models.FirewallSecurityFacts, error) {
	return models.FirewallSecurityFacts{}, nil
}
func (m *mockSvc) ListWorkloadIdentitySecurityFacts(_ context.Context, _ models.SecurityFactsRequest) (models.WorkloadIdentitySecurityFacts, error) {
	return models.WorkloadIdentitySecurityFacts{}, nil
}
func (m *mockSvc) ListSecurityRecommendations(_ context.Context, _ models.SecurityFactsRequest) (models.SecurityRecommendationFacts, error) {
	return models.SecurityRecommendationFacts{}, nil
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
func (m *mockSvc) GetGKEAuraScore(_ context.Context, _ models.GetGKEAuraScoreRequest) (models.GKEAuraReport, error) {
	return models.GKEAuraReport{}, nil
}
func (m *mockSvc) GetGCSAuraScore(_ context.Context, _ models.GetGCSAuraScoreRequest) (models.GCSAuraReport, error) {
	return models.GCSAuraReport{}, nil
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
func (m *mockSvc) ListBucketObjects(_ context.Context, _ models.ListBucketObjectsRequest) (models.ListBucketObjectsResponse, error) {
	return models.ListBucketObjectsResponse{}, nil
}
func (m *mockSvc) ListJobs(_ context.Context, _ models.ListJobsRequest) (models.ListJobsResponse, error) {
	return models.ListJobsResponse{}, nil
}
func (m *mockSvc) GetJobDetails(_ context.Context, _ models.GetJobDetailsRequest) (models.JobDetails, error) {
	return models.JobDetails{}, nil
}
func (m *mockSvc) ListJobExecutions(_ context.Context, _ models.ListJobExecutionsRequest) (models.ListJobExecutionsResponse, error) {
	return models.ListJobExecutionsResponse{}, nil
}
func (m *mockSvc) ListFunctions(_ context.Context, _ models.ListFunctionsRequest) (models.ListFunctionsResponse, error) {
	return models.ListFunctionsResponse{}, nil
}
func (m *mockSvc) GetFunctionDetails(_ context.Context, _ models.GetFunctionDetailsRequest) (models.FunctionDetails, error) {
	return models.FunctionDetails{}, nil
}
func (m *mockSvc) ListTriggers(_ context.Context, _ models.ListTriggersRequest) (models.ListTriggersResponse, error) {
	return models.ListTriggersResponse{}, nil
}
func (m *mockSvc) GetTrigger(_ context.Context, _ models.GetTriggerRequest) (models.TriggerDetails, error) {
	return models.TriggerDetails{}, nil
}
func (m *mockSvc) ListSchedulerJobs(_ context.Context, _ models.ListSchedulerJobsRequest) (models.ListSchedulerJobsResponse, error) {
	return models.ListSchedulerJobsResponse{}, nil
}
func (m *mockSvc) ListWorkflows(_ context.Context, _ models.ListWorkflowsRequest) (models.ListWorkflowsResponse, error) {
	return models.ListWorkflowsResponse{}, nil
}
func (m *mockSvc) ListWorkflowExecutions(_ context.Context, _ models.ListWorkflowExecutionsRequest) (models.ListWorkflowExecutionsResponse, error) {
	return models.ListWorkflowExecutionsResponse{}, nil
}
func (m *mockSvc) ListTaskQueues(_ context.Context, _ models.ListTaskQueuesRequest) (models.ListTaskQueuesResponse, error) {
	return models.ListTaskQueuesResponse{}, nil
}
func (m *mockSvc) ListSecrets(_ context.Context, _ models.ListSecretsRequest) (models.ListSecretsResponse, error) {
	return models.ListSecretsResponse{}, nil
}
func (m *mockSvc) ListSubscriptions(_ context.Context, _ models.ListSubscriptionsRequest) (models.ListSubscriptionsResponse, error) {
	return models.ListSubscriptionsResponse{}, nil
}
func (m *mockSvc) ListVPCConnectors(_ context.Context, _ models.ListVPCConnectorsRequest) (models.ListVPCConnectorsResponse, error) {
	return models.ListVPCConnectorsResponse{}, nil
}
func (m *mockSvc) ListSQLInstances(_ context.Context, _ models.ListSQLInstancesRequest) (models.ListSQLInstancesResponse, error) {
	return models.ListSQLInstancesResponse{}, nil
}
func (m *mockSvc) ListMetricDescriptors(_ context.Context, _ models.ListMetricDescriptorsRequest) (models.ListMetricDescriptorsResponse, error) {
	return models.ListMetricDescriptorsResponse{}, nil
}
func (m *mockSvc) ListTraceServices(_ context.Context, _ models.ListTraceServicesRequest) (models.ListTraceServicesResponse, error) {
	return models.ListTraceServicesResponse{}, nil
}
func (m *mockSvc) ExportServerlessGraph(_ context.Context, _ models.ExportServerlessGraphRequest) (models.ServerlessGraph, error) {
	return models.ServerlessGraph{}, nil
}
func (m *mockSvc) ListGKEWorkloads(_ context.Context, _ models.ListGKEWorkloadsRequest) (models.ListGKEWorkloadsResponse, error) {
	return models.ListGKEWorkloadsResponse{}, nil
}
func (m *mockSvc) GetGKEWorkloadDetails(_ context.Context, _ models.GetGKEWorkloadDetailsRequest) (models.GKEWorkloadDetails, error) {
	return models.GKEWorkloadDetails{}, nil
}
func (m *mockSvc) ListGKEServices(_ context.Context, _ models.ListGKEServicesRequest) (models.ListGKEServicesResponse, error) {
	return models.ListGKEServicesResponse{}, nil
}
func (m *mockSvc) ListGKEIngresses(_ context.Context, _ models.ListGKEIngressesRequest) (models.ListGKEIngressesResponse, error) {
	return models.ListGKEIngressesResponse{}, nil
}
func (m *mockSvc) ListGKENetworkPolicies(_ context.Context, _ models.ListGKENetworkPoliciesRequest) (models.ListGKENetworkPoliciesResponse, error) {
	return models.ListGKENetworkPoliciesResponse{}, nil
}
func (m *mockSvc) GetGKEMeshTopology(_ context.Context, _ models.GetGKEMeshTopologyRequest) (models.GKEMeshTopologyResponse, error) {
	return models.GKEMeshTopologyResponse{}, nil
}
func (m *mockSvc) ListLoadBalancers(_ context.Context, _ models.ListLoadBalancersRequest) (models.ListLoadBalancersResponse, error) {
	return models.ListLoadBalancersResponse{}, nil
}
func (m *mockSvc) ListURLMaps(_ context.Context, _ models.ListURLMapsRequest) (models.ListURLMapsResponse, error) {
	return models.ListURLMapsResponse{}, nil
}
func (m *mockSvc) ListNEGs(_ context.Context, _ models.ListNEGsRequest) (models.ListNEGsResponse, error) {
	return models.ListNEGsResponse{}, nil
}
func (m *mockSvc) ListAPIGateways(_ context.Context, _ models.ListAPIGatewaysRequest) (models.ListAPIGatewaysResponse, error) {
	return models.ListAPIGatewaysResponse{}, nil
}
func (m *mockSvc) ListVPCNetworks(_ context.Context, _ models.ListVPCNetworksRequest) (models.ListVPCNetworksResponse, error) {
	return models.ListVPCNetworksResponse{}, nil
}
func (m *mockSvc) ListVPCSubnets(_ context.Context, _ models.ListVPCSubnetsRequest) (models.ListVPCSubnetsResponse, error) {
	return models.ListVPCSubnetsResponse{}, nil
}
func (m *mockSvc) ListPSCEndpoints(_ context.Context, _ models.ListPSCEndpointsRequest) (models.ListPSCEndpointsResponse, error) {
	return models.ListPSCEndpointsResponse{}, nil
}
func (m *mockSvc) ListSpannerInstances(_ context.Context, _ models.ListSpannerInstancesRequest) (models.ListSpannerInstancesResponse, error) {
	return models.ListSpannerInstancesResponse{}, nil
}
func (m *mockSvc) ListAlloyDBClusters(_ context.Context, _ models.ListAlloyDBClustersRequest) (models.ListAlloyDBClustersResponse, error) {
	return models.ListAlloyDBClustersResponse{}, nil
}
func (m *mockSvc) ListFirestoreDatabases(_ context.Context, _ models.ListFirestoreDatabasesRequest) (models.ListFirestoreDatabasesResponse, error) {
	return models.ListFirestoreDatabasesResponse{}, nil
}
func (m *mockSvc) ListMemorystoreInstances(_ context.Context, _ models.ListMemorystoreInstancesRequest) (models.ListMemorystoreInstancesResponse, error) {
	return models.ListMemorystoreInstancesResponse{}, nil
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
		"gcp_cloudrun_list_jobs",
		"gcp_cloudrun_get_job_details",
		"gcp_cloudrun_list_job_executions",
		"gcp_pubsub_list_topics",
		"gcp_pubsub_inspect_topic_health",
		"gcp_pubsub_list_subscriptions",
		"gcp_logging_query_recent",
		"gcp_monitoring_get_metrics",
		"gcp_iam_test_permissions",
		"gcp_iam_get_resource_bindings",
		"gcp_iam_list_service_accounts",
		"gcp_get_service_topology",
		"gcp_get_aura_score",
		"gcp_project_aura_summary",
		"gcp_gke_get_aura_score",
		"gcp_gcs_get_aura_score",
		"gcp_storage_list_buckets",
		"gcp_storage_get_bucket_metadata",
		"gcp_storage_list_bucket_objects",
		"gcp_functions_list",
		"gcp_functions_get_details",
		"gcp_eventarc_list_triggers",
		"gcp_eventarc_get_trigger",
		"gcp_scheduler_list_jobs",
		"gcp_workflows_list",
		"gcp_workflows_list_executions",
		"gcp_tasks_list_queues",
		"gcp_secretmanager_list",
		"gcp_vpc_list_connectors",
		"gcp_cloudsql_list_instances",
		"gcp_monitoring_list_metric_descriptors",
		"gcp_trace_list_services",
		"gcp_monitoring_list_alert_policies",
		"gcp_monitoring_list_uptime_checks",
		"gcp_monitoring_list_slos",
		"gcp_monitoring_list_dashboards",
		"gcp_trace_list_dependency_edges",
		"gcp_observability_coverage",
		"gcp_export_architecture_graph",
		"gcp_generate_architecture_diagram",
		"gcp_export_serverless_graph",
		"gcp_gke_list_workloads",
		"gcp_gke_get_workload_details",
		"gcp_gke_list_services",
		"gcp_gke_list_ingresses",
		"gcp_gke_list_network_policies",
		"gcp_gke_get_mesh_topology",
		"gcp_compute_list_loadbalancers",
		"gcp_compute_list_url_maps",
		"gcp_compute_list_negs",
		"gcp_apigateway_list",
		"gcp_vpc_list_networks",
		"gcp_vpc_list_subnets",
		"gcp_psc_list_endpoints",
		"gcp_spanner_list_instances",
		"gcp_alloydb_list_clusters",
		"gcp_firestore_list_databases",
		"gcp_memorystore_list_instances",
		"gcp_artifactregistry_list_repos",
		"gcp_artifactregistry_list_images",
		"gcp_cloudbuild_list_triggers",
		"gcp_servicedirectory_list",
		"gcp_tag_list_resources",
		"gcp_incident_diagnose",
		"gcp_project_security_audit",
		"gcp_compare_environments",
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

// TestToolsListProtocolCompliance sends a raw JSON-RPC request via HandleMessage
// and verifies the wire format is valid MCP: correct jsonrpc version, matching id,
// and a non-empty tools array. This catches regressions in the mcp-go wiring.
func TestToolsListProtocolCompliance(t *testing.T) {
	s := New(&mockSvc{}, slog.Default(), "test")

	req := json.RawMessage(`{"jsonrpc":"2.0","id":42,"method":"tools/list","params":{}}`)
	raw, err := json.Marshal(s.HandleMessage(context.Background(), req))
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var resp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
		Error *struct{ Code int } `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nraw: %s", err, raw)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: code=%d, raw=%s", resp.Error.Code, raw)
	}
	if resp.JSONRPC != "2.0" {
		t.Errorf("jsonrpc: want 2.0, got %q", resp.JSONRPC)
	}
	if resp.ID != 42 {
		t.Errorf("id: want 42, got %d", resp.ID)
	}
	if len(resp.Result.Tools) == 0 {
		t.Error("result.tools must be non-empty")
	}
	t.Logf("OK — %d tools registered", len(resp.Result.Tools))
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

func TestResourceContentsSafetyLimit(t *testing.T) {
	withinLimit := []protocol.ResourceContents{protocol.TextResourceContents{URI: "gcp://test/resource", Text: "small"}}
	if _, err := enforceResourceContentsLimit(withinLimit); err != nil {
		t.Fatalf("small resource rejected: %v", err)
	}
	oversized := []protocol.ResourceContents{protocol.TextResourceContents{
		URI: "gcp://test/resource", Text: strings.Repeat("x", maxMCPResourceBytes+1),
	}}
	if _, err := enforceResourceContentsLimit(oversized); err == nil || !strings.Contains(err.Error(), "safety limit") {
		t.Fatalf("oversized resource error = %v", err)
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

func TestIncidentModuleRegistersToolAndPromptTogether(t *testing.T) {
	s := New(&mockSvc{}, slog.Default(), "test",
		WithModules(map[string]bool{ModuleIncident: true}),
	)

	registered := s.ListTools()
	if len(registered) != 1 {
		t.Fatalf("incident module tool count = %d, want 1", len(registered))
	}
	if _, ok := registered["gcp_incident_diagnose"]; !ok {
		t.Error("incident module did not register gcp_incident_diagnose")
	}

	msg := json.RawMessage(`{"jsonrpc":"2.0","id":8,"method":"prompts/list","params":{}}`)
	raw, _ := json.Marshal(s.HandleMessage(context.Background(), msg))
	if !strings.Contains(string(raw), "incident-response-helper") {
		t.Fatalf("incident prompt was not registered with its module: %s", raw)
	}
}

func TestCostModuleIsOptInAndFilterable(t *testing.T) {
	withoutCost := New(&mockSvc{}, slog.Default(), "test")
	if _, ok := withoutCost.ListTools()["gcp_cost_explain"]; ok {
		t.Fatal("cost explanation tool must not register without explicit configuration")
	}

	withCost := New(&mockSvc{}, slog.Default(), "test",
		WithCostReasoning(costreasoning.Config{}),
		WithModules(map[string]bool{ModuleCost: true}),
	)
	registered := withCost.ListTools()
	if len(registered) != 1 {
		t.Fatalf("cost module tool count = %d, want 1", len(registered))
	}
	if _, ok := registered["gcp_cost_explain"]; !ok {
		t.Fatal("cost module did not register gcp_cost_explain")
	}
}

func TestIncidentPromptIsHiddenWhenModuleDisabled(t *testing.T) {
	s := New(&mockSvc{}, slog.Default(), "test",
		WithModules(map[string]bool{ModuleGKE: true}),
	)
	msg := json.RawMessage(`{"jsonrpc":"2.0","id":9,"method":"prompts/list","params":{}}`)
	raw, _ := json.Marshal(s.HandleMessage(context.Background(), msg))
	if strings.Contains(string(raw), "incident-response-helper") {
		t.Fatalf("incident prompt should be hidden when incident module is disabled: %s", raw)
	}
}

func TestBigQueryOptimizationPromptRequiresMonitoringModule(t *testing.T) {
	s := New(&mockSvc{}, slog.Default(), "test", WithModules(map[string]bool{}))
	msg := json.RawMessage(`{"jsonrpc":"2.0","id":10,"method":"prompts/list","params":{}}`)
	raw, _ := json.Marshal(s.HandleMessage(context.Background(), msg))
	if strings.Contains(string(raw), "optimize-bigquery-costs") {
		t.Fatalf("monitoring-dependent prompt was registered without monitoring: %s", raw)
	}

	s = New(&mockSvc{}, slog.Default(), "test", WithModules(map[string]bool{ModuleMonitoring: true}))
	raw, _ = json.Marshal(s.HandleMessage(context.Background(), msg))
	if !strings.Contains(string(raw), "optimize-bigquery-costs") {
		t.Fatalf("optimization prompt missing with monitoring enabled: %s", raw)
	}
}

func TestProtocolErrorsDoNotReflectCallerControlledIdentifiers(t *testing.T) {
	s := New(&mockSvc{}, slog.Default(), "test")
	const sentinel = "admin@example.com"
	tests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"admin@example.com","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"prompts/get","params":{"name":"admin@example.com","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"unknown://admin@example.com"}}`,
		`{"jsonrpc":"2.0","id":4,"method":"admin@example.com","params":{}}`,
	}
	for _, request := range tests {
		response := handleJSON(t, s, request)
		if strings.Contains(response, sentinel) {
			t.Fatalf("protocol error reflected caller input: %s", response)
		}
		if !strings.Contains(response, `"error"`) {
			t.Fatalf("guarded request did not return an error: %s", response)
		}
	}
}

func TestProtocolGuardAllowsRegisteredResourceTemplates(t *testing.T) {
	s := New(&mockSvc{}, slog.Default(), "test", WithDefaultProjectID("test-project"))
	response := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"gcp://test-project/storage/example-bucket"}}`)
	if strings.Contains(response, "requested resource is unavailable") {
		t.Fatalf("registered resource template was blocked: %s", response)
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

func TestPromptDescriptionCannotBypassAnonymizer(t *testing.T) {
	scrubber, err := anonymize.NewLocalScrubber(anonymize.Config{})
	if err != nil {
		t.Fatal(err)
	}
	s := New(&mockSvc{}, slog.Default(), "test", WithAnonymizer(scrubber))
	response := handleJSON(t, s, `{"jsonrpc":"2.0","id":5,"method":"prompts/get","params":{"name":"audit-security-posture","arguments":{"project_id":"test-project","focus":"admin@example.com"}}}`)
	if strings.Contains(response, "admin@example.com") {
		t.Fatalf("prompt description bypassed anonymizer: %s", response)
	}
	if !strings.Contains(response, "EMAIL") {
		t.Fatalf("scrub token missing from prompt response: %s", response)
	}
}
func (m *mockSvc) ListArtifactRegistryRepos(_ context.Context, _ models.ListArtifactRegistryReposRequest) (models.ListArtifactRegistryReposResponse, error) {
	return models.ListArtifactRegistryReposResponse{}, nil
}
func (m *mockSvc) ListArtifactRegistryImages(_ context.Context, _ models.ListArtifactRegistryImagesRequest) (models.ListArtifactRegistryImagesResponse, error) {
	return models.ListArtifactRegistryImagesResponse{}, nil
}
func (m *mockSvc) ListCloudBuildTriggers(_ context.Context, _ models.ListCloudBuildTriggersRequest) (models.ListCloudBuildTriggersResponse, error) {
	return models.ListCloudBuildTriggersResponse{}, nil
}
func (m *mockSvc) ListServiceDirectoryNamespaces(_ context.Context, _ models.ListServiceDirectoryNamespacesRequest) (models.ListServiceDirectoryNamespacesResponse, error) {
	return models.ListServiceDirectoryNamespacesResponse{}, nil
}
func (m *mockSvc) GetResourceIAMBindings(_ context.Context, _ models.GetResourceIAMBindingsRequest) (models.GetResourceIAMBindingsResponse, error) {
	return models.GetResourceIAMBindingsResponse{}, nil
}
func (m *mockSvc) ListServiceAccounts(_ context.Context, _ models.ListServiceAccountsRequest) (models.ListServiceAccountsResponse, error) {
	return models.ListServiceAccountsResponse{}, nil
}
func (m *mockSvc) ListAlertPolicies(_ context.Context, _ models.ListAlertPoliciesRequest) (models.ListAlertPoliciesResponse, error) {
	return models.ListAlertPoliciesResponse{}, nil
}
func (m *mockSvc) ListUptimeChecks(_ context.Context, _ models.ListUptimeChecksRequest) (models.ListUptimeChecksResponse, error) {
	return models.ListUptimeChecksResponse{}, nil
}
func (m *mockSvc) ListSLOs(_ context.Context, _ models.ListSLOsRequest) (models.ListSLOsResponse, error) {
	return models.ListSLOsResponse{}, nil
}
func (m *mockSvc) ListDashboards(_ context.Context, _ models.ListDashboardsRequest) (models.ListDashboardsResponse, error) {
	return models.ListDashboardsResponse{}, nil
}
func (m *mockSvc) ListTraceDependencyEdges(_ context.Context, _ models.ListTraceDependencyEdgesRequest) (models.ListTraceDependencyEdgesResponse, error) {
	return models.ListTraceDependencyEdgesResponse{}, nil
}
func (m *mockSvc) GetObservabilityCoverage(_ context.Context, _ models.GetObservabilityCoverageRequest) (models.ObservabilityCoverageResponse, error) {
	return models.ObservabilityCoverageResponse{}, nil
}
func (m *mockSvc) ExportArchitectureGraph(_ context.Context, _ models.ExportArchitectureGraphRequest) (models.ServerlessGraph, error) {
	return models.ServerlessGraph{}, nil
}
func (m *mockSvc) ListTaggedResources(_ context.Context, _ models.ListTaggedResourcesRequest) (models.ListTaggedResourcesResponse, error) {
	return models.ListTaggedResourcesResponse{}, nil
}
func (m *mockSvc) ExportRecommendationsToBQ(_ context.Context, _ models.ExportRecommendationsToBQRequest) (models.ExportRecommendationsToBQResponse, error) {
	return models.ExportRecommendationsToBQResponse{}, nil
}

func (m *mockSvc) CollectCostFacts(_ context.Context, _ models.CollectCostFactsRequest) (models.BillingCostFacts, error) {
	return models.BillingCostFacts{}, nil
}

func (m *mockSvc) ListCostRecommendations(_ context.Context, _ models.ListCostRecommendationsRequest) (models.ListCostRecommendationsResponse, error) {
	return models.ListCostRecommendationsResponse{}, nil
}

func (m *mockSvc) ListCreatedAssets(_ context.Context, _ models.ListCreatedAssetsRequest) (models.ListCreatedAssetsResponse, error) {
	return models.ListCreatedAssetsResponse{}, nil
}

type environmentCaptureSvc struct {
	*mockSvc
	logProjects     []string
	datasetProjects []string
	logError        error
}

type oversizedLogSvc struct{ *mockSvc }

func (s *oversizedLogSvc) QueryRecentLogs(context.Context, models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error) {
	return models.QueryRecentLogsResponse{
		Entries:      []models.LogEntry{{Message: strings.Repeat("x", maxMCPToolResultBytes+1024)}},
		TotalFetched: 1,
	}, nil
}

func TestServerWithholdsOversizedToolResults(t *testing.T) {
	s := New(&oversizedLogSvc{mockSvc: &mockSvc{}}, slog.Default(), "test", WithModules(map[string]bool{ModuleLogging: true}))
	response := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"gcp_logging_query_recent","arguments":{}}}`)
	if !strings.Contains(response, "safety limit") || len(response) > 4096 {
		t.Fatalf("oversized result was not replaced with a bounded error: length=%d response=%s", len(response), response)
	}
}

func (s *environmentCaptureSvc) QueryRecentLogs(_ context.Context, req models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error) {
	s.logProjects = append(s.logProjects, req.ProjectID)
	if s.logError != nil {
		return models.QueryRecentLogsResponse{}, s.logError
	}
	return models.QueryRecentLogsResponse{
		Entries: []models.LogEntry{{
			Message: "response from " + req.ProjectID,
			LogName: "projects/" + req.ProjectID + "/logs/application",
		}},
		TotalFetched:  1,
		AppliedFilter: `resource.labels.project_id="` + req.ProjectID + `"`,
	}, nil
}

func (s *environmentCaptureSvc) ListDatasets(_ context.Context, req models.ListDatasetsRequest) (models.ListDatasetsResponse, error) {
	s.datasetProjects = append(s.datasetProjects, req.ProjectID)
	return models.ListDatasetsResponse{ProjectID: req.ProjectID, Datasets: []models.DatasetSummary{}}, nil
}

func testEnvironmentRegistry(t *testing.T) *environments.Registry {
	t.Helper()
	registry, err := environments.NewRegistry([]environments.Environment{
		{ProjectID: "private-dev-123", Alias: "dev", Default: true},
		{ProjectID: "private-prod-345", Alias: "prod"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func handleJSON(t *testing.T, s *server.MCPServer, message string) string {
	t.Helper()
	raw, err := json.Marshal(s.HandleMessage(context.Background(), json.RawMessage(message)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestEnvironmentAliasesRouteCaseInsensitivelyAndMaskOutputs(t *testing.T) {
	svc := &environmentCaptureSvc{mockSvc: &mockSvc{}}
	s := New(svc, slog.Default(), "test",
		WithModules(map[string]bool{ModuleLogging: true}),
		WithEnvironments(testEnvironmentRegistry(t)),
	)

	for _, selector := range []string{"PROD", "private-prod-345"} {
		response := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"gcp_logging_query_recent","arguments":{"project_id":"`+selector+`"}}}`)
		if strings.Contains(response, "private-prod-345") || strings.Contains(response, "private-dev-123") {
			t.Fatalf("project ID leaked for selector %q: %s", selector, response)
		}
		if !strings.Contains(response, "prod") {
			t.Fatalf("prod alias missing for selector %q: %s", selector, response)
		}
	}
	if got := svc.logProjects; len(got) != 2 || got[0] != "private-prod-345" || got[1] != "private-prod-345" {
		t.Fatalf("captured log projects = %#v", got)
	}

	defaultResponse := handleJSON(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"gcp_logging_query_recent","arguments":{}}}`)
	if strings.Contains(defaultResponse, "private-dev-123") || !strings.Contains(defaultResponse, "dev") {
		t.Fatalf("default response was not alias-safe: %s", defaultResponse)
	}
	if svc.logProjects[len(svc.logProjects)-1] != "private-dev-123" {
		t.Fatalf("default project = %q", svc.logProjects[len(svc.logProjects)-1])
	}
}

type driftCaptureSvc struct {
	*mockSvc
	mu       sync.Mutex
	projects []string
}

func (s *driftCaptureSvc) ListServices(_ context.Context, req models.ListServicesRequest) (models.ListServicesResponse, error) {
	s.mu.Lock()
	s.projects = append(s.projects, req.ProjectID)
	s.mu.Unlock()
	services := []models.ServiceSummary{{Name: "api", Region: "us-central1"}}
	if req.ProjectID == "private-dev-123" {
		services = append(services, models.ServiceSummary{Name: "worker", Region: "us-central1"})
	}
	return models.ListServicesResponse{Services: services}, nil
}

func (s *driftCaptureSvc) GetServiceDetails(_ context.Context, req models.GetServiceDetailsRequest) (models.ServiceDetails, error) {
	return models.ServiceDetails{
		ServiceSummary: models.ServiceSummary{Name: req.ServiceName, Region: req.Region},
		LatestRevision: req.ServiceName + "-00001",
		Traffic:        []models.TrafficTarget{{Revision: req.ServiceName + "-00001", Percent: 100}},
		Labels:         map[string]string{"environment": req.ProjectID},
	}, nil
}

func (s *driftCaptureSvc) ListRevisions(_ context.Context, req models.ListRevisionsRequest) (models.ListRevisionsResponse, error) {
	minimum := int32(0)
	if req.ProjectID == "private-prod-345" {
		minimum = 3
	}
	return models.ListRevisionsResponse{Revisions: []models.RevisionSummary{{
		Name: req.ServiceName + "-00001", ServiceName: req.ServiceName, Region: req.Region,
		Ready: true, MinInstances: minimum,
	}}}, nil
}

func TestEnvironmentDriftResolvesTwoAliasesAndNamesMissingEnvironment(t *testing.T) {
	svc := &driftCaptureSvc{mockSvc: &mockSvc{}}
	s := New(svc, slog.Default(), "test", WithModules(map[string]bool{ModuleDrift: true}), WithEnvironments(testEnvironmentRegistry(t)))
	response := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"gcp_compare_environments","arguments":{"environment_a":"DEV","environment_b":"private-prod-345","components":["cloudrun"]}}}`)
	if strings.Contains(response, "private-dev-123") || strings.Contains(response, "private-prod-345") {
		t.Fatalf("project ID leaked: %s", response)
	}
	if !strings.Contains(response, `\"missing_in\":\"prod\"`) || !strings.Contains(response, "missing in prod") {
		t.Fatalf("alias-specific missing result absent: %s", response)
	}
	if !strings.Contains(response, `\"environment_a\":\"dev\"`) || !strings.Contains(response, `\"environment_b\":\"prod\"`) {
		t.Fatalf("environment aliases absent: %s", response)
	}
	if !strings.Contains(response, `\"resources_only_in\":[{\"environment\":\"dev\",\"resources\":1},{\"environment\":\"prod\",\"resources\":0}]`) {
		t.Fatalf("alias-specific summary totals absent: %s", response)
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.projects) != 2 || svc.projects[0] == svc.projects[1] {
		t.Fatalf("collected projects = %#v", svc.projects)
	}
}

func TestEnvironmentDriftRejectsSameResolvedEnvironment(t *testing.T) {
	svc := &driftCaptureSvc{mockSvc: &mockSvc{}}
	s := New(svc, slog.Default(), "test", WithModules(map[string]bool{ModuleDrift: true}), WithEnvironments(testEnvironmentRegistry(t)))
	response := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"gcp_compare_environments","arguments":{"environment_a":"dev","environment_b":"private-dev-123","components":["cloudrun"]}}}`)
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if !strings.Contains(response, "different configured environments") || len(svc.projects) != 0 {
		t.Fatalf("response=%s projects=%#v", response, svc.projects)
	}
}

func TestEnvironmentUnknownSelectorIsSafeAndDoesNotCallService(t *testing.T) {
	svc := &environmentCaptureSvc{mockSvc: &mockSvc{}}
	s := New(svc, slog.Default(), "test",
		WithModules(map[string]bool{ModuleLogging: true}),
		WithEnvironments(testEnvironmentRegistry(t)),
	)
	response := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"gcp_logging_query_recent","arguments":{"project_id":"unknown-secret-project"}}}`)
	if strings.Contains(response, "unknown-secret-project") {
		t.Fatalf("unknown selector was echoed: %s", response)
	}
	if !strings.Contains(response, "dev") || !strings.Contains(response, "prod") {
		t.Fatalf("safe choices missing: %s", response)
	}
	if len(svc.logProjects) != 0 {
		t.Fatalf("service was called: %#v", svc.logProjects)
	}
}

type iamScopeCaptureSvc struct {
	*mockSvc
	requests []models.GetResourceIAMBindingsRequest
}

func (s *iamScopeCaptureSvc) GetResourceIAMBindings(_ context.Context, req models.GetResourceIAMBindingsRequest) (models.GetResourceIAMBindingsResponse, error) {
	s.requests = append(s.requests, req)
	return models.GetResourceIAMBindingsResponse{URN: req.URN, Bindings: []models.IAMBinding{}}, nil
}

func TestEnvironmentScopeRejectsCrossProjectURNBeforeServiceCall(t *testing.T) {
	svc := &iamScopeCaptureSvc{mockSvc: &mockSvc{}}
	s := New(svc, slog.Default(), "test",
		WithModules(map[string]bool{ModuleIAM: true}),
		WithEnvironments(testEnvironmentRegistry(t)),
	)

	response := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"gcp_iam_get_resource_bindings","arguments":{"project_id":"dev","urn":"urn:gcp:project:-:private-prod-345:private-prod-345"}}}`)
	if len(svc.requests) != 0 || !strings.Contains(response, "outside the selected configured environment") {
		t.Fatalf("requests=%+v response=%s", svc.requests, response)
	}

	response = handleJSON(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"gcp_iam_get_resource_bindings","arguments":{"project_id":"prod","urn":"urn:gcp:project:-:private-prod-345:private-prod-345"}}}`)
	if len(svc.requests) != 1 || svc.requests[0].ProjectID != "private-prod-345" {
		t.Fatalf("requests=%+v response=%s", svc.requests, response)
	}
	if strings.Contains(response, "private-prod-345") {
		t.Fatalf("project ID leaked: %s", response)
	}
}

func TestValidateScopedArgumentsRejectsPathsAndOversizedStrings(t *testing.T) {
	if err := validateScopedArguments(map[string]any{"service_name": "../../projects/other"}, "project"); err == nil {
		t.Fatal("path-like service name was accepted")
	}
	if err := validateScopedArguments(map[string]any{"regions": []any{"us-central1", "../other"}}, "project"); err == nil {
		t.Fatal("path-like region was accepted")
	}
	if err := validateScopedArguments(map[string]any{"filter": strings.Repeat("x", 4097)}, "project"); err == nil {
		t.Fatal("oversized string was accepted")
	}
	if err := validateScopedArguments(map[string]any{"service_name": "payments"}, "project"); err != nil {
		t.Fatalf("valid arguments rejected: %v", err)
	}
}

type duplicateToolModule struct{ name string }

func (m duplicateToolModule) Name() string { return m.name }
func (m duplicateToolModule) GetTools() []server.ServerTool {
	return []server.ServerTool{{Tool: protocol.NewTool("same-tool")}}
}

func TestFilteredRegistryDeduplicatesToolNames(t *testing.T) {
	tools := FilteredRegistry([]ToolModule{duplicateToolModule{"a"}, duplicateToolModule{"b"}}, nil)
	if len(tools) != 1 {
		t.Fatalf("tool count = %d, want 1", len(tools))
	}
}

func TestEnvironmentErrorsNeverExposeAliasedProjectID(t *testing.T) {
	svc := &environmentCaptureSvc{
		mockSvc:  &mockSvc{},
		logError: errors.New("request to projects/private-prod-345 failed"),
	}
	s := New(svc, slog.Default(), "test",
		WithModules(map[string]bool{ModuleLogging: true}),
		WithEnvironments(testEnvironmentRegistry(t)),
	)
	response := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"gcp_logging_query_recent","arguments":{"project_id":"prod"}}}`)
	if strings.Contains(response, "private-prod-345") {
		t.Fatalf("project ID leaked through error: %s", response)
	}
}

func TestEnvironmentMetadataResourcesAndPromptsNeverExposeAliasedIDs(t *testing.T) {
	svc := &environmentCaptureSvc{mockSvc: &mockSvc{}}
	s := New(svc, slog.Default(), "test", WithEnvironments(testEnvironmentRegistry(t)))

	responses := []string{
		handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`),
		handleJSON(t, s, `{"jsonrpc":"2.0","id":2,"method":"resources/list","params":{}}`),
		handleJSON(t, s, `{"jsonrpc":"2.0","id":3,"method":"prompts/list","params":{}}`),
		handleJSON(t, s, `{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"gcp://prod/bigquery/datasets"}}`),
		handleJSON(t, s, `{"jsonrpc":"2.0","id":5,"method":"prompts/get","params":{"name":"audit-security-posture","arguments":{"project_id":"private-prod-345"}}}`),
		handleJSON(t, s, `{"jsonrpc":"2.0","id":6,"method":"prompts/get","params":{"name":"audit-security-posture","arguments":{}}}`),
		handleJSON(t, s, `{"jsonrpc":"2.0","id":7,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`),
		handleJSON(t, s, `{"jsonrpc":"2.0","id":8,"method":"prompts/get","params":{"name":"audit-security-posture","arguments":{"project_id":"PrOd"}}}`),
	}
	for index, response := range responses {
		if strings.Contains(response, "private-prod-345") || strings.Contains(response, "private-dev-123") {
			t.Fatalf("response %d leaked a project ID: %s", index, response)
		}
	}
	if !strings.Contains(responses[0], "Available aliases: dev, prod") {
		t.Fatalf("tool schema does not advertise environment aliases: %s", responses[0])
	}
	for _, uri := range []string{"gcp://dev/bigquery/datasets", "gcp://prod/bigquery/datasets"} {
		if !strings.Contains(responses[1], uri) {
			t.Fatalf("resources/list missing %q: %s", uri, responses[1])
		}
	}
	if len(svc.datasetProjects) != 1 || svc.datasetProjects[0] != "private-prod-345" {
		t.Fatalf("resource project = %#v", svc.datasetProjects)
	}
	if !strings.Contains(responses[4], "prod") || !strings.Contains(responses[5], "dev") {
		t.Fatalf("prompt aliases missing: prod=%s default=%s", responses[4], responses[5])
	}
	if !strings.Contains(responses[6], "dev (default), prod") {
		t.Fatalf("initialize instructions missing environments: %s", responses[6])
	}
	if !strings.Contains(responses[7], "prod") {
		t.Fatalf("case-insensitive prompt alias was not resolved: %s", responses[7])
	}
}

func TestLegacyProjectMaskingCoversAllMCPSurfaces(t *testing.T) {
	const projectID = "private-solo-123"
	registry, err := environments.NewRegistry([]environments.Environment{{ProjectID: projectID}})
	if err != nil {
		t.Fatal(err)
	}
	svc := &environmentCaptureSvc{mockSvc: &mockSvc{}}
	s := New(svc, slog.Default(), "test",
		WithModules(map[string]bool{ModuleLogging: true}),
		WithEnvironments(registry),
		WithProjectIDReplacements(map[string]string{projectID: "[GCP_PROJECT_ID]"}),
		WithProjectIDPlaceholder("your-project"),
	)
	responses := []string{
		handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`),
		handleJSON(t, s, `{"jsonrpc":"2.0","id":2,"method":"resources/list","params":{}}`),
		handleJSON(t, s, `{"jsonrpc":"2.0","id":3,"method":"prompts/list","params":{}}`),
		handleJSON(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"gcp_logging_query_recent","arguments":{"project_id":"your-project"}}}`),
		handleJSON(t, s, `{"jsonrpc":"2.0","id":5,"method":"prompts/get","params":{"name":"optimize-bigquery-costs","arguments":{}}}`),
		handleJSON(t, s, `{"jsonrpc":"2.0","id":6,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`),
	}
	for index, response := range responses {
		if strings.Contains(response, projectID) {
			t.Fatalf("response %d leaked masked project: %s", index, response)
		}
	}
	if !strings.Contains(responses[0], "your-project") || !strings.Contains(responses[1], "gcp://your-project/") {
		t.Fatalf("placeholder not exposed safely: tools=%s resources=%s", responses[0], responses[1])
	}
	if !strings.Contains(responses[5], "private default GCP project") || !strings.Contains(responses[5], "your-project") {
		t.Fatalf("initialize instructions leaked or omitted placeholder: %s", responses[5])
	}
	if len(svc.logProjects) != 1 || svc.logProjects[0] != projectID {
		t.Fatalf("internal project routing = %#v", svc.logProjects)
	}
}
