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
		mcp.WithDescription("Diagnose an active Cloud Run production incident by correlating recent deployments, error rate, p99 latency, revision traffic, audit/IAM changes, logs, dependencies, and optional platform-health events. Returns ranked hypotheses with transparent evidence and read-only investigation steps."),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("environment", mcp.Description("Environment label to infer when service_name is omitted (default: production). Matches env/environment/stage labels.")),
		mcp.WithString("service_name", mcp.Description("Cloud Run service to diagnose. Omit to discover services carrying the requested environment label.")),
		mcp.WithString("region", mcp.Description("Cloud Run region. Omit to discover the service across regions.")),
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
