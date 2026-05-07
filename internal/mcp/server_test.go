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
