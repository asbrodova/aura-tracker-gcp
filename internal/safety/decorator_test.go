package safety

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/internal/gcp"
	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// stubService is a minimal fake ports.GCPService for safety tests.
// Only the two mutation methods have meaningful implementations; all
// read methods return zero values.
type stubService struct {
	scaleResp models.ScaleDeploymentResponse
	scaleErr  error
	trafficResp models.UpdateTrafficResponse
	trafficErr  error
}

func (s *stubService) ScaleDeployment(_ context.Context, req models.ScaleDeploymentRequest) (models.ScaleDeploymentResponse, error) {
	if s.scaleErr != nil {
		return models.ScaleDeploymentResponse{}, s.scaleErr
	}
	if req.DryRun {
		return models.ScaleDeploymentResponse{
			DryRun:         true,
			NodePoolName:   req.NodePoolName,
			PreviousCount:  3,
			RequestedCount: req.NodeCount,
		}, nil
	}
	return s.scaleResp, nil
}

func (s *stubService) UpdateTraffic(_ context.Context, req models.UpdateTrafficRequest) (models.UpdateTrafficResponse, error) {
	if s.trafficErr != nil {
		return models.UpdateTrafficResponse{}, s.trafficErr
	}
	if req.DryRun {
		return models.UpdateTrafficResponse{
			DryRun:      true,
			ServiceName: req.ServiceName,
			Before:      []models.TrafficTarget{{Revision: "v1", Percent: 100}},
			After:       req.Traffic,
		}, nil
	}
	return s.trafficResp, nil
}

// Satisfy the full interface with zero-value returns.
func (s *stubService) ListClusters(_ context.Context, _ models.ListClustersRequest) (models.ListClustersResponse, error) {
	return models.ListClustersResponse{}, nil
}
func (s *stubService) GetClusterDetails(_ context.Context, _ models.GetClusterDetailsRequest) (models.ClusterDetails, error) {
	return models.ClusterDetails{}, nil
}
func (s *stubService) GetClusterBottlenecks(_ context.Context, _ models.GetClusterBottlenecksRequest) (models.ClusterBottleneckReport, error) {
	return models.ClusterBottleneckReport{}, nil
}
func (s *stubService) ListServices(_ context.Context, _ models.ListServicesRequest) (models.ListServicesResponse, error) {
	return models.ListServicesResponse{}, nil
}
func (s *stubService) GetServiceDetails(_ context.Context, _ models.GetServiceDetailsRequest) (models.ServiceDetails, error) {
	return models.ServiceDetails{}, nil
}
func (s *stubService) ListTopics(_ context.Context, _ models.ListTopicsRequest) (models.ListTopicsResponse, error) {
	return models.ListTopicsResponse{}, nil
}
func (s *stubService) InspectTopicHealth(_ context.Context, _ models.InspectTopicHealthRequest) (models.TopicHealthReport, error) {
	return models.TopicHealthReport{}, nil
}
func (s *stubService) QueryRecentLogs(_ context.Context, _ models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error) {
	return models.QueryRecentLogsResponse{}, nil
}
func (s *stubService) GetMetrics(_ context.Context, _ models.GetMetricsRequest) (models.GetMetricsResponse, error) {
	return models.GetMetricsResponse{}, nil
}
func (s *stubService) TestPermissions(_ context.Context, _ models.TestPermissionsRequest) (models.TestPermissionsResponse, error) {
	return models.TestPermissionsResponse{}, nil
}
func (s *stubService) GetServiceTopology(_ context.Context, _ models.GetServiceTopologyRequest) (models.ServiceTopologyReport, error) {
	return models.ServiceTopologyReport{}, nil
}
func (s *stubService) GetAuraScore(_ context.Context, _ models.GetAuraScoreRequest) (models.AuraReport, error) {
	return models.AuraReport{}, nil
}
func (s *stubService) GetProjectAuraSummary(_ context.Context, _ models.ProjectAuraSummaryRequest) (models.ProjectAuraSummaryResponse, error) {
	return models.ProjectAuraSummaryResponse{}, nil
}
func (s *stubService) ListDatasets(_ context.Context, _ models.ListDatasetsRequest) (models.ListDatasetsResponse, error) {
	return models.ListDatasetsResponse{}, nil
}
func (s *stubService) ListTables(_ context.Context, _ models.ListTablesRequest) (models.ListTablesResponse, error) {
	return models.ListTablesResponse{}, nil
}
func (s *stubService) GetTableSchema(_ context.Context, _ models.GetTableSchemaRequest) (models.TableSchemaResponse, error) {
	return models.TableSchemaResponse{}, nil
}
func (s *stubService) ListBuckets(_ context.Context, _ models.ListBucketsRequest) (models.ListBucketsResponse, error) {
	return models.ListBucketsResponse{}, nil
}
func (s *stubService) GetBucketMetadata(_ context.Context, _ models.GetBucketMetadataRequest) (models.BucketMetadataResponse, error) {
	return models.BucketMetadataResponse{}, nil
}
func (s *stubService) ListJobs(_ context.Context, _ models.ListJobsRequest) (models.ListJobsResponse, error) {
	return models.ListJobsResponse{}, nil
}
func (s *stubService) GetJobDetails(_ context.Context, _ models.GetJobDetailsRequest) (models.JobDetails, error) {
	return models.JobDetails{}, nil
}
func (s *stubService) ListJobExecutions(_ context.Context, _ models.ListJobExecutionsRequest) (models.ListJobExecutionsResponse, error) {
	return models.ListJobExecutionsResponse{}, nil
}
func (s *stubService) ListFunctions(_ context.Context, _ models.ListFunctionsRequest) (models.ListFunctionsResponse, error) {
	return models.ListFunctionsResponse{}, nil
}
func (s *stubService) GetFunctionDetails(_ context.Context, _ models.GetFunctionDetailsRequest) (models.FunctionDetails, error) {
	return models.FunctionDetails{}, nil
}
func (s *stubService) ListTriggers(_ context.Context, _ models.ListTriggersRequest) (models.ListTriggersResponse, error) {
	return models.ListTriggersResponse{}, nil
}
func (s *stubService) GetTrigger(_ context.Context, _ models.GetTriggerRequest) (models.TriggerDetails, error) {
	return models.TriggerDetails{}, nil
}
func (s *stubService) ListSchedulerJobs(_ context.Context, _ models.ListSchedulerJobsRequest) (models.ListSchedulerJobsResponse, error) {
	return models.ListSchedulerJobsResponse{}, nil
}
func (s *stubService) ListWorkflows(_ context.Context, _ models.ListWorkflowsRequest) (models.ListWorkflowsResponse, error) {
	return models.ListWorkflowsResponse{}, nil
}
func (s *stubService) ListWorkflowExecutions(_ context.Context, _ models.ListWorkflowExecutionsRequest) (models.ListWorkflowExecutionsResponse, error) {
	return models.ListWorkflowExecutionsResponse{}, nil
}
func (s *stubService) ListTaskQueues(_ context.Context, _ models.ListTaskQueuesRequest) (models.ListTaskQueuesResponse, error) {
	return models.ListTaskQueuesResponse{}, nil
}
func (s *stubService) ListSecrets(_ context.Context, _ models.ListSecretsRequest) (models.ListSecretsResponse, error) {
	return models.ListSecretsResponse{}, nil
}
func (s *stubService) ListSubscriptions(_ context.Context, _ models.ListSubscriptionsRequest) (models.ListSubscriptionsResponse, error) {
	return models.ListSubscriptionsResponse{}, nil
}
func (s *stubService) ListVPCConnectors(_ context.Context, _ models.ListVPCConnectorsRequest) (models.ListVPCConnectorsResponse, error) {
	return models.ListVPCConnectorsResponse{}, nil
}
func (s *stubService) ListSQLInstances(_ context.Context, _ models.ListSQLInstancesRequest) (models.ListSQLInstancesResponse, error) {
	return models.ListSQLInstancesResponse{}, nil
}
func (s *stubService) ListMetricDescriptors(_ context.Context, _ models.ListMetricDescriptorsRequest) (models.ListMetricDescriptorsResponse, error) {
	return models.ListMetricDescriptorsResponse{}, nil
}
func (s *stubService) ListTraceServices(_ context.Context, _ models.ListTraceServicesRequest) (models.ListTraceServicesResponse, error) {
	return models.ListTraceServicesResponse{}, nil
}
func (s *stubService) ExportServerlessGraph(_ context.Context, _ models.ExportServerlessGraphRequest) (models.ServerlessGraph, error) {
	return models.ServerlessGraph{}, nil
}
func (s *stubService) ListGKEWorkloads(_ context.Context, _ models.ListGKEWorkloadsRequest) (models.ListGKEWorkloadsResponse, error) {
	return models.ListGKEWorkloadsResponse{}, nil
}
func (s *stubService) GetGKEWorkloadDetails(_ context.Context, _ models.GetGKEWorkloadDetailsRequest) (models.GKEWorkloadDetails, error) {
	return models.GKEWorkloadDetails{}, nil
}
func (s *stubService) ListGKEServices(_ context.Context, _ models.ListGKEServicesRequest) (models.ListGKEServicesResponse, error) {
	return models.ListGKEServicesResponse{}, nil
}
func (s *stubService) ListGKEIngresses(_ context.Context, _ models.ListGKEIngressesRequest) (models.ListGKEIngressesResponse, error) {
	return models.ListGKEIngressesResponse{}, nil
}
func (s *stubService) ListGKENetworkPolicies(_ context.Context, _ models.ListGKENetworkPoliciesRequest) (models.ListGKENetworkPoliciesResponse, error) {
	return models.ListGKENetworkPoliciesResponse{}, nil
}
func (s *stubService) GetGKEMeshTopology(_ context.Context, _ models.GetGKEMeshTopologyRequest) (models.GKEMeshTopologyResponse, error) {
	return models.GKEMeshTopologyResponse{}, nil
}
func (s *stubService) ListLoadBalancers(_ context.Context, _ models.ListLoadBalancersRequest) (models.ListLoadBalancersResponse, error) {
	return models.ListLoadBalancersResponse{}, nil
}
func (s *stubService) ListURLMaps(_ context.Context, _ models.ListURLMapsRequest) (models.ListURLMapsResponse, error) {
	return models.ListURLMapsResponse{}, nil
}
func (s *stubService) ListNEGs(_ context.Context, _ models.ListNEGsRequest) (models.ListNEGsResponse, error) {
	return models.ListNEGsResponse{}, nil
}
func (s *stubService) ListAPIGateways(_ context.Context, _ models.ListAPIGatewaysRequest) (models.ListAPIGatewaysResponse, error) {
	return models.ListAPIGatewaysResponse{}, nil
}
func (s *stubService) ListVPCNetworks(_ context.Context, _ models.ListVPCNetworksRequest) (models.ListVPCNetworksResponse, error) {
	return models.ListVPCNetworksResponse{}, nil
}
func (s *stubService) ListVPCSubnets(_ context.Context, _ models.ListVPCSubnetsRequest) (models.ListVPCSubnetsResponse, error) {
	return models.ListVPCSubnetsResponse{}, nil
}
func (s *stubService) ListPSCEndpoints(_ context.Context, _ models.ListPSCEndpointsRequest) (models.ListPSCEndpointsResponse, error) {
	return models.ListPSCEndpointsResponse{}, nil
}
func (s *stubService) ListSpannerInstances(_ context.Context, _ models.ListSpannerInstancesRequest) (models.ListSpannerInstancesResponse, error) {
	return models.ListSpannerInstancesResponse{}, nil
}
func (s *stubService) ListAlloyDBClusters(_ context.Context, _ models.ListAlloyDBClustersRequest) (models.ListAlloyDBClustersResponse, error) {
	return models.ListAlloyDBClustersResponse{}, nil
}
func (s *stubService) ListFirestoreDatabases(_ context.Context, _ models.ListFirestoreDatabasesRequest) (models.ListFirestoreDatabasesResponse, error) {
	return models.ListFirestoreDatabasesResponse{}, nil
}
func (s *stubService) ListMemorystoreInstances(_ context.Context, _ models.ListMemorystoreInstancesRequest) (models.ListMemorystoreInstancesResponse, error) {
	return models.ListMemorystoreInstancesResponse{}, nil
}

func newTestDecorator(inner *stubService) *SafetyDecorator {
	return NewSafetyDecorator(inner, slog.Default())
}

// --- ScaleDeployment tests ---

func TestScaleDeployment_DryRun_StoresPlan(t *testing.T) {
	d := newTestDecorator(&stubService{})

	resp, err := d.ScaleDeployment(context.Background(), models.ScaleDeploymentRequest{
		NodePoolName: "pool-1",
		NodeCount:    5,
		DryRun:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.PlanID == "" {
		t.Error("expected plan_id in dry-run response")
	}
	if resp.ExpiresIn == "" {
		t.Error("expected expires_in in dry-run response")
	}
	if !resp.DryRun {
		t.Error("expected DryRun=true in response")
	}
}

func TestScaleDeployment_Confirm_ExecutesAndClearsPlan(t *testing.T) {
	inner := &stubService{
		scaleResp: models.ScaleDeploymentResponse{NodePoolName: "pool-1", RequestedCount: 5},
	}
	d := newTestDecorator(inner)

	dryResp, err := d.ScaleDeployment(context.Background(), models.ScaleDeploymentRequest{
		NodePoolName: "pool-1",
		NodeCount:    5,
		DryRun:       true,
	})
	if err != nil {
		t.Fatal("dry-run failed:", err)
	}

	resp, err := d.ScaleDeployment(context.Background(), models.ScaleDeploymentRequest{
		ConfirmPlanID: dryResp.PlanID,
	})
	if err != nil {
		t.Fatal("confirm failed:", err)
	}
	if resp.NodePoolName != "pool-1" {
		t.Errorf("unexpected node pool in response: %q", resp.NodePoolName)
	}

	// Plan must be gone — single-use.
	_, stillThere := d.store.take(dryResp.PlanID)
	if stillThere {
		t.Error("plan still in store after confirmation — not single-use")
	}
}

func TestScaleDeployment_NoFlags_Blocked(t *testing.T) {
	d := newTestDecorator(&stubService{})

	_, err := d.ScaleDeployment(context.Background(), models.ScaleDeploymentRequest{
		NodeCount: 5,
	})
	var cre *gcp.ConfirmationRequiredError
	if !errors.As(err, &cre) {
		t.Errorf("expected ConfirmationRequiredError, got %T: %v", err, err)
	}
}

func TestScaleDeployment_ExpiredPlan_Blocked(t *testing.T) {
	d := newTestDecorator(&stubService{})

	// Inject an already-expired entry directly.
	d.store.mu.Lock()
	d.store.entries["expired-id"] = planEntry{
		payload:   scaleDeploymentPlan{},
		expiresAt: time.Now().Add(-1 * time.Second),
	}
	d.store.mu.Unlock()

	_, err := d.ScaleDeployment(context.Background(), models.ScaleDeploymentRequest{
		ConfirmPlanID: "expired-id",
	})
	var cre *gcp.ConfirmationRequiredError
	if !errors.As(err, &cre) {
		t.Errorf("expected ConfirmationRequiredError for expired plan, got %T: %v", err, err)
	}
}

func TestScaleDeployment_NoChangeNeeded_NoPlan(t *testing.T) {
	inner := &stubService{}
	// Override so DryRun returns NoChangeNeeded.
	d := newTestDecorator(inner)
	// Manually insert a response via a wrapped stub that returns NoChangeNeeded.
	type ncStub struct{ stubService }
	ncs := &ncStub{}
	d2 := NewSafetyDecorator(&stubNoChange{}, slog.Default())

	resp, err := d2.ScaleDeployment(context.Background(), models.ScaleDeploymentRequest{
		NodePoolName: "pool-1",
		NodeCount:    3,
		DryRun:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.PlanID != "" {
		t.Error("expected no plan_id when no change is needed")
	}
	_ = d
	_ = ncs
}

// stubNoChange returns NoChangeNeeded for dry-run ScaleDeployment.
type stubNoChange struct{ stubService }

func (s *stubNoChange) ScaleDeployment(_ context.Context, req models.ScaleDeploymentRequest) (models.ScaleDeploymentResponse, error) {
	if req.DryRun {
		return models.ScaleDeploymentResponse{DryRun: true, NoChangeNeeded: true, Description: "already at desired count"}, nil
	}
	return models.ScaleDeploymentResponse{}, nil
}

// --- UpdateTraffic tests ---

func TestUpdateTraffic_DryRun_StoresPlan(t *testing.T) {
	d := newTestDecorator(&stubService{})

	resp, err := d.UpdateTraffic(context.Background(), models.UpdateTrafficRequest{
		ServiceName: "svc-a",
		Traffic:     []models.TrafficTarget{{Revision: "v2", Percent: 100}},
		DryRun:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.PlanID == "" {
		t.Error("expected plan_id in dry-run response")
	}
}

func TestUpdateTraffic_Confirm_ExecutesAndClearsPlan(t *testing.T) {
	inner := &stubService{
		trafficResp: models.UpdateTrafficResponse{ServiceName: "svc-a"},
	}
	d := newTestDecorator(inner)

	dryResp, _ := d.UpdateTraffic(context.Background(), models.UpdateTrafficRequest{
		ServiceName: "svc-a",
		Traffic:     []models.TrafficTarget{{Revision: "v2", Percent: 100}},
		DryRun:      true,
	})

	resp, err := d.UpdateTraffic(context.Background(), models.UpdateTrafficRequest{
		ConfirmPlanID: dryResp.PlanID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ServiceName != "svc-a" {
		t.Errorf("unexpected service name: %q", resp.ServiceName)
	}
}

func TestUpdateTraffic_NoFlags_Blocked(t *testing.T) {
	d := newTestDecorator(&stubService{})

	_, err := d.UpdateTraffic(context.Background(), models.UpdateTrafficRequest{
		ServiceName: "svc-a",
	})
	var cre *gcp.ConfirmationRequiredError
	if !errors.As(err, &cre) {
		t.Errorf("expected ConfirmationRequiredError, got %T: %v", err, err)
	}
}

// --- Plan replay prevention ---

func TestScaleDeployment_PlanCannotBeConfirmedTwice(t *testing.T) {
	inner := &stubService{
		scaleResp: models.ScaleDeploymentResponse{NodePoolName: "pool-1", RequestedCount: 5},
	}
	d := newTestDecorator(inner)

	dryResp, _ := d.ScaleDeployment(context.Background(), models.ScaleDeploymentRequest{
		NodePoolName: "pool-1", NodeCount: 5, DryRun: true,
	})

	// First confirm succeeds.
	_, err := d.ScaleDeployment(context.Background(), models.ScaleDeploymentRequest{
		ConfirmPlanID: dryResp.PlanID,
	})
	if err != nil {
		t.Fatal("first confirm failed:", err)
	}

	// Second confirm must fail.
	_, err = d.ScaleDeployment(context.Background(), models.ScaleDeploymentRequest{
		ConfirmPlanID: dryResp.PlanID,
	})
	var cre *gcp.ConfirmationRequiredError
	if !errors.As(err, &cre) {
		t.Errorf("expected ConfirmationRequiredError on replay, got %T: %v", err, err)
	}
}
func (s *stubService) ListArtifactRegistryRepos(_ context.Context, _ models.ListArtifactRegistryReposRequest) (models.ListArtifactRegistryReposResponse, error) {
	return models.ListArtifactRegistryReposResponse{}, nil
}
func (s *stubService) ListArtifactRegistryImages(_ context.Context, _ models.ListArtifactRegistryImagesRequest) (models.ListArtifactRegistryImagesResponse, error) {
	return models.ListArtifactRegistryImagesResponse{}, nil
}
func (s *stubService) ListCloudBuildTriggers(_ context.Context, _ models.ListCloudBuildTriggersRequest) (models.ListCloudBuildTriggersResponse, error) {
	return models.ListCloudBuildTriggersResponse{}, nil
}
func (s *stubService) ListServiceDirectoryNamespaces(_ context.Context, _ models.ListServiceDirectoryNamespacesRequest) (models.ListServiceDirectoryNamespacesResponse, error) {
	return models.ListServiceDirectoryNamespacesResponse{}, nil
}
func (s *stubService) GetResourceIAMBindings(_ context.Context, _ models.GetResourceIAMBindingsRequest) (models.GetResourceIAMBindingsResponse, error) {
	return models.GetResourceIAMBindingsResponse{}, nil
}
func (s *stubService) ListServiceAccounts(_ context.Context, _ models.ListServiceAccountsRequest) (models.ListServiceAccountsResponse, error) {
	return models.ListServiceAccountsResponse{}, nil
}
func (s *stubService) ListAlertPolicies(_ context.Context, _ models.ListAlertPoliciesRequest) (models.ListAlertPoliciesResponse, error) {
	return models.ListAlertPoliciesResponse{}, nil
}
func (s *stubService) ListUptimeChecks(_ context.Context, _ models.ListUptimeChecksRequest) (models.ListUptimeChecksResponse, error) {
	return models.ListUptimeChecksResponse{}, nil
}
func (s *stubService) ListSLOs(_ context.Context, _ models.ListSLOsRequest) (models.ListSLOsResponse, error) {
	return models.ListSLOsResponse{}, nil
}
func (s *stubService) ListDashboards(_ context.Context, _ models.ListDashboardsRequest) (models.ListDashboardsResponse, error) {
	return models.ListDashboardsResponse{}, nil
}
func (s *stubService) ListTraceDependencyEdges(_ context.Context, _ models.ListTraceDependencyEdgesRequest) (models.ListTraceDependencyEdgesResponse, error) {
	return models.ListTraceDependencyEdgesResponse{}, nil
}
func (s *stubService) GetObservabilityCoverage(_ context.Context, _ models.GetObservabilityCoverageRequest) (models.ObservabilityCoverageResponse, error) {
	return models.ObservabilityCoverageResponse{}, nil
}
func (s *stubService) ExportArchitectureGraph(_ context.Context, _ models.ExportArchitectureGraphRequest) (models.ServerlessGraph, error) {
	return models.ServerlessGraph{}, nil
}
func (s *stubService) ListTaggedResources(_ context.Context, _ models.ListTaggedResourcesRequest) (models.ListTaggedResourcesResponse, error) {
	return models.ListTaggedResourcesResponse{}, nil
}
