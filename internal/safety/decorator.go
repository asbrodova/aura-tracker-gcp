// Package safety implements a port-level safety decorator that enforces
// two-step HITL confirmation for all mutation methods on ports.GCPService.
// Read-only methods are transparent pass-throughs.
package safety

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/asbrodova/aura-tracker-gcp/internal/gcp"
	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

// scaleDeploymentPlan holds the dry-run state for a pending ScaleDeployment.
type scaleDeploymentPlan struct {
	OriginalRequest models.ScaleDeploymentRequest
	DryRunResponse  models.ScaleDeploymentResponse
}

// updateTrafficPlan holds the dry-run state for a pending UpdateTraffic.
type updateTrafficPlan struct {
	OriginalRequest models.UpdateTrafficRequest
	DryRunResponse  models.UpdateTrafficResponse
}

// SafetyDecorator wraps a ports.GCPService and enforces a two-step dry-run →
// confirm flow for all mutation methods. Construct with NewSafetyDecorator.
// Safe for concurrent use.
type SafetyDecorator struct {
	inner ports.GCPService
	store *PlanStore
	log   *slog.Logger
}

// Compile-time check: SafetyDecorator must implement ports.GCPService.
var _ ports.GCPService = (*SafetyDecorator)(nil)

// NewSafetyDecorator wraps inner with safety enforcement.
func NewSafetyDecorator(inner ports.GCPService, log *slog.Logger) *SafetyDecorator {
	return &SafetyDecorator{inner: inner, store: NewPlanStore(), log: log}
}

// --- Mutation methods ---

func (d *SafetyDecorator) ScaleDeployment(ctx context.Context, req models.ScaleDeploymentRequest) (models.ScaleDeploymentResponse, error) {
	const op = "safety.ScaleDeployment"

	// Path 1: dry-run — delegate to inner, store a plan, inject plan_id.
	if req.DryRun {
		preview, err := d.inner.ScaleDeployment(ctx, req)
		if err != nil || preview.NoChangeNeeded {
			return preview, err
		}
		planID := uuid.New().String()
		d.store.put(planID, scaleDeploymentPlan{OriginalRequest: req, DryRunResponse: preview})
		ttl := d.store.expiresIn(planID)
		preview.PlanID = planID
		preview.ExpiresIn = ttl.String()
		preview.Description = fmt.Sprintf(
			"DRY RUN: would resize node pool %q from %d to %d nodes. "+
				"Confirm with confirm_plan_id=%q within %s.",
			req.NodePoolName, preview.PreviousCount, req.NodeCount, planID, ttl,
		)
		d.log.InfoContext(ctx, "safety: plan created",
			"op", "ScaleDeployment",
			"plan_id", planID,
			"node_pool", req.NodePoolName,
			"from", preview.PreviousCount,
			"to", req.NodeCount,
			"expires_in", ttl,
		)
		return preview, nil
	}

	// Path 2: confirm — consume the stored plan and execute with original params.
	if req.ConfirmPlanID != "" {
		raw, ok := d.store.take(req.ConfirmPlanID)
		if !ok {
			return models.ScaleDeploymentResponse{}, &gcp.ConfirmationRequiredError{
				Op: op,
				Message: fmt.Sprintf(
					"plan_id %q not found or expired (TTL=%s). Run with dry_run=true first.",
					req.ConfirmPlanID, PlanTTL,
				),
			}
		}
		plan, ok := raw.(scaleDeploymentPlan)
		if !ok {
			return models.ScaleDeploymentResponse{}, fmt.Errorf("%s: internal: wrong plan type", op)
		}
		execReq := plan.OriginalRequest
		execReq.DryRun = false
		execReq.ConfirmPlanID = ""
		resp, err := d.inner.ScaleDeployment(ctx, execReq)
		if err != nil {
			return models.ScaleDeploymentResponse{}, err
		}
		d.log.WarnContext(ctx, "safety: mutation confirmed",
			"op", "ScaleDeployment",
			"plan_id", req.ConfirmPlanID,
			"project_id", execReq.ProjectID,
			"location", execReq.Location,
			"cluster_name", execReq.ClusterName,
			"node_pool", execReq.NodePoolName,
			"previous_count", resp.PreviousCount,
			"requested_count", resp.RequestedCount,
		)
		return resp, nil
	}

	// Path 3: neither flag — block with actionable instructions.
	return models.ScaleDeploymentResponse{}, &gcp.ConfirmationRequiredError{
		Op: op,
		Message: fmt.Sprintf(
			"mutations require two-step confirmation. "+
				"Step 1: call gcp_gke_scale_deployment with dry_run=true to receive a plan_id. "+
				"Step 2: call again with confirm_plan_id=<plan_id> (within %s).",
			PlanTTL,
		),
	}
}

func (d *SafetyDecorator) UpdateTraffic(ctx context.Context, req models.UpdateTrafficRequest) (models.UpdateTrafficResponse, error) {
	const op = "safety.UpdateTraffic"

	if req.DryRun {
		preview, err := d.inner.UpdateTraffic(ctx, req)
		if err != nil {
			return preview, err
		}
		planID := uuid.New().String()
		d.store.put(planID, updateTrafficPlan{OriginalRequest: req, DryRunResponse: preview})
		ttl := d.store.expiresIn(planID)
		preview.PlanID = planID
		preview.ExpiresIn = ttl.String()
		preview.Description = fmt.Sprintf(
			"DRY RUN: would update traffic split for service %q. "+
				"Confirm with confirm_plan_id=%q within %s.",
			req.ServiceName, planID, ttl,
		)
		d.log.InfoContext(ctx, "safety: plan created",
			"op", "UpdateTraffic",
			"plan_id", planID,
			"service", req.ServiceName,
			"before", preview.Before,
			"after", preview.After,
			"expires_in", ttl,
		)
		return preview, nil
	}

	if req.ConfirmPlanID != "" {
		raw, ok := d.store.take(req.ConfirmPlanID)
		if !ok {
			return models.UpdateTrafficResponse{}, &gcp.ConfirmationRequiredError{
				Op: op,
				Message: fmt.Sprintf(
					"plan_id %q not found or expired (TTL=%s). Run with dry_run=true first.",
					req.ConfirmPlanID, PlanTTL,
				),
			}
		}
		plan, ok := raw.(updateTrafficPlan)
		if !ok {
			return models.UpdateTrafficResponse{}, fmt.Errorf("%s: internal: wrong plan type", op)
		}
		execReq := plan.OriginalRequest
		execReq.DryRun = false
		execReq.ConfirmPlanID = ""
		resp, err := d.inner.UpdateTraffic(ctx, execReq)
		if err != nil {
			return models.UpdateTrafficResponse{}, err
		}
		d.log.WarnContext(ctx, "safety: mutation confirmed",
			"op", "UpdateTraffic",
			"plan_id", req.ConfirmPlanID,
			"project_id", execReq.ProjectID,
			"region", execReq.Region,
			"service_name", execReq.ServiceName,
			"traffic_before", resp.Before,
			"traffic_after", resp.After,
		)
		return resp, nil
	}

	return models.UpdateTrafficResponse{}, &gcp.ConfirmationRequiredError{
		Op: op,
		Message: fmt.Sprintf(
			"mutations require two-step confirmation. "+
				"Step 1: call gcp_cloudrun_update_traffic with dry_run=true to receive a plan_id. "+
				"Step 2: call again with confirm_plan_id=<plan_id> (within %s).",
			PlanTTL,
		),
	}
}

// --- Read-only pass-throughs ---

func (d *SafetyDecorator) ListClusters(ctx context.Context, req models.ListClustersRequest) (models.ListClustersResponse, error) {
	return d.inner.ListClusters(ctx, req)
}

func (d *SafetyDecorator) ListGKEWorkloads(ctx context.Context, req models.ListGKEWorkloadsRequest) (models.ListGKEWorkloadsResponse, error) {
	return d.inner.ListGKEWorkloads(ctx, req)
}

func (d *SafetyDecorator) GetGKEWorkloadDetails(ctx context.Context, req models.GetGKEWorkloadDetailsRequest) (models.GKEWorkloadDetails, error) {
	return d.inner.GetGKEWorkloadDetails(ctx, req)
}

func (d *SafetyDecorator) ListGKEServices(ctx context.Context, req models.ListGKEServicesRequest) (models.ListGKEServicesResponse, error) {
	return d.inner.ListGKEServices(ctx, req)
}

func (d *SafetyDecorator) ListGKEIngresses(ctx context.Context, req models.ListGKEIngressesRequest) (models.ListGKEIngressesResponse, error) {
	return d.inner.ListGKEIngresses(ctx, req)
}

func (d *SafetyDecorator) ListGKENetworkPolicies(ctx context.Context, req models.ListGKENetworkPoliciesRequest) (models.ListGKENetworkPoliciesResponse, error) {
	return d.inner.ListGKENetworkPolicies(ctx, req)
}

func (d *SafetyDecorator) GetGKEMeshTopology(ctx context.Context, req models.GetGKEMeshTopologyRequest) (models.GKEMeshTopologyResponse, error) {
	return d.inner.GetGKEMeshTopology(ctx, req)
}

func (d *SafetyDecorator) ListLoadBalancers(ctx context.Context, req models.ListLoadBalancersRequest) (models.ListLoadBalancersResponse, error) {
	return d.inner.ListLoadBalancers(ctx, req)
}

func (d *SafetyDecorator) ListURLMaps(ctx context.Context, req models.ListURLMapsRequest) (models.ListURLMapsResponse, error) {
	return d.inner.ListURLMaps(ctx, req)
}

func (d *SafetyDecorator) ListNEGs(ctx context.Context, req models.ListNEGsRequest) (models.ListNEGsResponse, error) {
	return d.inner.ListNEGs(ctx, req)
}

func (d *SafetyDecorator) ListAPIGateways(ctx context.Context, req models.ListAPIGatewaysRequest) (models.ListAPIGatewaysResponse, error) {
	return d.inner.ListAPIGateways(ctx, req)
}

func (d *SafetyDecorator) ListVPCNetworks(ctx context.Context, req models.ListVPCNetworksRequest) (models.ListVPCNetworksResponse, error) {
	return d.inner.ListVPCNetworks(ctx, req)
}

func (d *SafetyDecorator) ListVPCSubnets(ctx context.Context, req models.ListVPCSubnetsRequest) (models.ListVPCSubnetsResponse, error) {
	return d.inner.ListVPCSubnets(ctx, req)
}

func (d *SafetyDecorator) ListPSCEndpoints(ctx context.Context, req models.ListPSCEndpointsRequest) (models.ListPSCEndpointsResponse, error) {
	return d.inner.ListPSCEndpoints(ctx, req)
}

func (d *SafetyDecorator) GetClusterDetails(ctx context.Context, req models.GetClusterDetailsRequest) (models.ClusterDetails, error) {
	return d.inner.GetClusterDetails(ctx, req)
}

func (d *SafetyDecorator) GetClusterBottlenecks(ctx context.Context, req models.GetClusterBottlenecksRequest) (models.ClusterBottleneckReport, error) {
	return d.inner.GetClusterBottlenecks(ctx, req)
}

func (d *SafetyDecorator) ListServices(ctx context.Context, req models.ListServicesRequest) (models.ListServicesResponse, error) {
	return d.inner.ListServices(ctx, req)
}

func (d *SafetyDecorator) ListSubscriptions(ctx context.Context, req models.ListSubscriptionsRequest) (models.ListSubscriptionsResponse, error) {
	return d.inner.ListSubscriptions(ctx, req)
}

func (d *SafetyDecorator) GetServiceDetails(ctx context.Context, req models.GetServiceDetailsRequest) (models.ServiceDetails, error) {
	return d.inner.GetServiceDetails(ctx, req)
}

func (d *SafetyDecorator) ListJobs(ctx context.Context, req models.ListJobsRequest) (models.ListJobsResponse, error) {
	return d.inner.ListJobs(ctx, req)
}

func (d *SafetyDecorator) GetJobDetails(ctx context.Context, req models.GetJobDetailsRequest) (models.JobDetails, error) {
	return d.inner.GetJobDetails(ctx, req)
}

func (d *SafetyDecorator) ListJobExecutions(ctx context.Context, req models.ListJobExecutionsRequest) (models.ListJobExecutionsResponse, error) {
	return d.inner.ListJobExecutions(ctx, req)
}

func (d *SafetyDecorator) ListFunctions(ctx context.Context, req models.ListFunctionsRequest) (models.ListFunctionsResponse, error) {
	return d.inner.ListFunctions(ctx, req)
}

func (d *SafetyDecorator) GetFunctionDetails(ctx context.Context, req models.GetFunctionDetailsRequest) (models.FunctionDetails, error) {
	return d.inner.GetFunctionDetails(ctx, req)
}

func (d *SafetyDecorator) ListTriggers(ctx context.Context, req models.ListTriggersRequest) (models.ListTriggersResponse, error) {
	return d.inner.ListTriggers(ctx, req)
}

func (d *SafetyDecorator) GetTrigger(ctx context.Context, req models.GetTriggerRequest) (models.TriggerDetails, error) {
	return d.inner.GetTrigger(ctx, req)
}

func (d *SafetyDecorator) ListSchedulerJobs(ctx context.Context, req models.ListSchedulerJobsRequest) (models.ListSchedulerJobsResponse, error) {
	return d.inner.ListSchedulerJobs(ctx, req)
}

func (d *SafetyDecorator) ListWorkflows(ctx context.Context, req models.ListWorkflowsRequest) (models.ListWorkflowsResponse, error) {
	return d.inner.ListWorkflows(ctx, req)
}

func (d *SafetyDecorator) ListWorkflowExecutions(ctx context.Context, req models.ListWorkflowExecutionsRequest) (models.ListWorkflowExecutionsResponse, error) {
	return d.inner.ListWorkflowExecutions(ctx, req)
}

func (d *SafetyDecorator) ListTaskQueues(ctx context.Context, req models.ListTaskQueuesRequest) (models.ListTaskQueuesResponse, error) {
	return d.inner.ListTaskQueues(ctx, req)
}

func (d *SafetyDecorator) ListSecrets(ctx context.Context, req models.ListSecretsRequest) (models.ListSecretsResponse, error) {
	return d.inner.ListSecrets(ctx, req)
}

func (d *SafetyDecorator) ListVPCConnectors(ctx context.Context, req models.ListVPCConnectorsRequest) (models.ListVPCConnectorsResponse, error) {
	return d.inner.ListVPCConnectors(ctx, req)
}

func (d *SafetyDecorator) ListSQLInstances(ctx context.Context, req models.ListSQLInstancesRequest) (models.ListSQLInstancesResponse, error) {
	return d.inner.ListSQLInstances(ctx, req)
}

func (d *SafetyDecorator) ListTopics(ctx context.Context, req models.ListTopicsRequest) (models.ListTopicsResponse, error) {
	return d.inner.ListTopics(ctx, req)
}

func (d *SafetyDecorator) InspectTopicHealth(ctx context.Context, req models.InspectTopicHealthRequest) (models.TopicHealthReport, error) {
	return d.inner.InspectTopicHealth(ctx, req)
}

func (d *SafetyDecorator) QueryRecentLogs(ctx context.Context, req models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error) {
	return d.inner.QueryRecentLogs(ctx, req)
}

func (d *SafetyDecorator) GetMetrics(ctx context.Context, req models.GetMetricsRequest) (models.GetMetricsResponse, error) {
	return d.inner.GetMetrics(ctx, req)
}

func (d *SafetyDecorator) ListMetricDescriptors(ctx context.Context, req models.ListMetricDescriptorsRequest) (models.ListMetricDescriptorsResponse, error) {
	return d.inner.ListMetricDescriptors(ctx, req)
}

func (d *SafetyDecorator) ListTraceServices(ctx context.Context, req models.ListTraceServicesRequest) (models.ListTraceServicesResponse, error) {
	return d.inner.ListTraceServices(ctx, req)
}

func (d *SafetyDecorator) TestPermissions(ctx context.Context, req models.TestPermissionsRequest) (models.TestPermissionsResponse, error) {
	return d.inner.TestPermissions(ctx, req)
}

func (d *SafetyDecorator) GetServiceTopology(ctx context.Context, req models.GetServiceTopologyRequest) (models.ServiceTopologyReport, error) {
	return d.inner.GetServiceTopology(ctx, req)
}

func (d *SafetyDecorator) GetAuraScore(ctx context.Context, req models.GetAuraScoreRequest) (models.AuraReport, error) {
	return d.inner.GetAuraScore(ctx, req)
}

func (d *SafetyDecorator) GetProjectAuraSummary(ctx context.Context, req models.ProjectAuraSummaryRequest) (models.ProjectAuraSummaryResponse, error) {
	return d.inner.GetProjectAuraSummary(ctx, req)
}

func (d *SafetyDecorator) ListDatasets(ctx context.Context, req models.ListDatasetsRequest) (models.ListDatasetsResponse, error) {
	return d.inner.ListDatasets(ctx, req)
}

func (d *SafetyDecorator) ListTables(ctx context.Context, req models.ListTablesRequest) (models.ListTablesResponse, error) {
	return d.inner.ListTables(ctx, req)
}

func (d *SafetyDecorator) GetTableSchema(ctx context.Context, req models.GetTableSchemaRequest) (models.TableSchemaResponse, error) {
	return d.inner.GetTableSchema(ctx, req)
}

func (d *SafetyDecorator) ListBuckets(ctx context.Context, req models.ListBucketsRequest) (models.ListBucketsResponse, error) {
	return d.inner.ListBuckets(ctx, req)
}

func (d *SafetyDecorator) GetBucketMetadata(ctx context.Context, req models.GetBucketMetadataRequest) (models.BucketMetadataResponse, error) {
	return d.inner.GetBucketMetadata(ctx, req)
}

func (d *SafetyDecorator) ExportServerlessGraph(ctx context.Context, req models.ExportServerlessGraphRequest) (models.ServerlessGraph, error) {
	return d.inner.ExportServerlessGraph(ctx, req)
}

func (d *SafetyDecorator) ListSpannerInstances(ctx context.Context, req models.ListSpannerInstancesRequest) (models.ListSpannerInstancesResponse, error) {
	return d.inner.ListSpannerInstances(ctx, req)
}

func (d *SafetyDecorator) ListAlloyDBClusters(ctx context.Context, req models.ListAlloyDBClustersRequest) (models.ListAlloyDBClustersResponse, error) {
	return d.inner.ListAlloyDBClusters(ctx, req)
}

func (d *SafetyDecorator) ListFirestoreDatabases(ctx context.Context, req models.ListFirestoreDatabasesRequest) (models.ListFirestoreDatabasesResponse, error) {
	return d.inner.ListFirestoreDatabases(ctx, req)
}

func (d *SafetyDecorator) ListMemorystoreInstances(ctx context.Context, req models.ListMemorystoreInstancesRequest) (models.ListMemorystoreInstancesResponse, error) {
	return d.inner.ListMemorystoreInstances(ctx, req)
}
