package tools

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// IncidentDiagnoser is implemented by diagnostics.Engine. Keeping the MCP
// module dependent on this small interface preserves the transport boundary.
type IncidentDiagnoser interface {
	Diagnose(context.Context, models.DiagnoseIncidentRequest) (models.DiagnoseIncidentResponse, error)
}

type IncidentTools struct {
	diagnoser IncidentDiagnoser
	log       *slog.Logger
}

func NewIncidentTools(diagnoser IncidentDiagnoser, log *slog.Logger) *IncidentTools {
	if log == nil {
		log = slog.Default()
	}
	return &IncidentTools{diagnoser: diagnoser, log: log}
}

func (t *IncidentTools) Name() string { return "incident" }

func (t *IncidentTools) GetTools() []server.ServerTool {
	return []server.ServerTool{t.DiagnoseProductionIncident()}
}

func (t *IncidentTools) DiagnoseProductionIncident() server.ServerTool {
	tool := mcp.NewTool("gcp_incident_diagnose",
		mcp.WithDescription("Diagnose an active production incident on a Cloud Run service, a Cloud Function, or a GKE workload. For Cloud Run (and Gen 2 functions, which run on it) it correlates recent deployments, error rate, p99 latency, revision traffic, audit/IAM changes, logs and dependencies; for Gen 1 functions, executions, failed executions, execution time, deployments and logs; for GKE workloads, ready replicas, container restarts, Kubernetes warning events (scheduling, image pulls, probes, OOM kills), rollouts and container logs. Optional platform-health events. Returns ranked hypotheses with transparent evidence and read-only investigation steps."),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("environment", mcp.Description("Environment label to infer when service_name is omitted (default: production). Matches env/environment/stage labels.")),
		mcp.WithString("resource_type", mcp.Description("What to diagnose (default: cloud_run_service). cloud_function and gke_workload require service_name."), mcp.Enum("cloud_run_service", "cloud_function", "gke_workload")),
		mcp.WithString("service_name", mcp.Description("Cloud Run service, Cloud Function, or GKE workload name. For Cloud Run, omit to discover services carrying the requested environment label.")),
		mcp.WithString("region", mcp.Description("Region of the service or function (omit to search all regions), or the GKE cluster location (required for gke_workload).")),
		mcp.WithString("cluster_name", mcp.Description("GKE cluster of the workload. Required for gke_workload.")),
		mcp.WithString("namespace", mcp.Description("Kubernetes namespace of the workload (default: default). gke_workload only.")),
		mcp.WithString("workload_kind", mcp.Description("Kind of the GKE workload (default: Deployment)."), mcp.Enum("Deployment", "StatefulSet", "DaemonSet")),
		mcp.WithNumber("lookback_minutes", mcp.Description("Active incident window, 5-720 minutes (default: 60)."), mcp.Min(5), mcp.Max(720)),
		mcp.WithNumber("baseline_minutes", mcp.Description("Comparison window immediately before the incident window (default: 240; total windows capped at 1440 minutes)."), mcp.Min(5), mcp.Max(1440)),
		mcp.WithNumber("max_services", mcp.Description("Maximum inferred services to analyze, 1-25 (default: 10)."), mcp.Min(1), mcp.Max(25)),
		mcp.WithNumber("max_dependencies", mcp.Description("Maximum dependencies to health-check per service, 1-25 (default: 10)."), mcp.Min(1), mcp.Max(25)),
		mcp.WithString("detail_level", mcp.Description("Output detail: summary, standard, or detailed (default: standard)."), mcp.Enum("summary", "standard", "detailed")),
		mcp.WithBoolean("include_platform_health", mcp.Description("Also query Personalized Service Health log events when available (default: false)."), mcp.DefaultBool(false)),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Diagnose Production Incident",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{Tool: tool, Handler: mcp.NewTypedToolHandler(t.diagnoseHandler)}
}

func (t *IncidentTools) diagnoseHandler(ctx context.Context, _ mcp.CallToolRequest, args models.DiagnoseIncidentRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_incident_diagnose",
		"project", args.ProjectID,
		"resource_type", args.ResourceType,
		"service", args.ServiceName,
		"region", args.Region,
		"environment", args.Environment,
	)
	response, err := t.diagnoser.Diagnose(ctx, args)
	if err != nil {
		return handleServiceError("gcp_incident_diagnose", err)
	}
	result, err := mcp.NewToolResultJSON(response)
	if err != nil {
		return nil, fmt.Errorf("gcp_incident_diagnose: marshal: %w", err)
	}
	return result, nil
}
