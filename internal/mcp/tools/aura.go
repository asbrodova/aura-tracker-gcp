package tools

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

// AuraTools provides MCP tool definitions and handlers for Aura Score operations.
type AuraTools struct {
	svc ports.GCPService
	log *slog.Logger
}

func NewAuraTools(svc ports.GCPService, log *slog.Logger) *AuraTools {
	return &AuraTools{svc: svc, log: log}
}

func (t *AuraTools) Name() string { return "aura" }

func (t *AuraTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.GetAuraScore(),
		t.ProjectAuraSummary(),
	}
}

func (t *AuraTools) GetAuraScore() server.ServerTool {
	tool := mcp.NewTool("gcp_get_aura_score",
		mcp.WithDescription(
			"Return an Aura Score (0-100) combining Cloud Monitoring health signals and utilization " +
				"efficiency for a resource. Cached 5 min. Bands: green ≥80, yellow 50-79, red <50.",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("resource_kind", mcp.Required(),
			mcp.Description("Resource type: cloud_run | bigquery | cloud_sql"),
			mcp.Enum("cloud_run", "bigquery", "cloud_sql"),
		),
		mcp.WithString("resource_name", mcp.Required(),
			mcp.Description("Resource name — Cloud Run service name, Cloud SQL instance name, or BigQuery dataset ID"),
		),
		mcp.WithString("region", mcp.Required(),
			mcp.Description("GCP region (e.g. us-central1). Use empty string for BigQuery (global)."),
		),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Get Aura Score",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.getAuraScoreHandler),
	}
}

func (t *AuraTools) getAuraScoreHandler(ctx context.Context, _ mcp.CallToolRequest, args models.GetAuraScoreRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_get_aura_score",
		"project", args.ProjectID,
		"kind", args.ResourceKind,
		"name", args.ResourceName,
		"region", args.Region,
	)
	report, err := t.svc.GetAuraScore(ctx, args)
	if err != nil {
		return handleServiceError("gcp_get_aura_score", err)
	}
	result, err := mcp.NewToolResultJSON(report)
	if err != nil {
		return nil, fmt.Errorf("gcp_get_aura_score: marshal: %w", err)
	}
	return result, nil
}

func (t *AuraTools) ProjectAuraSummary() server.ServerTool {
	tool := mcp.NewTool("gcp_project_aura_summary",
		mcp.WithDescription(
			"Score all Cloud Run, Cloud SQL, and BigQuery resources with Aura Scores, sorted worst-first. " +
				"Includes a pre-formatted summary block.",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("region",
			mcp.Description("Limit discovery to a specific region (e.g. us-central1). Leave empty to cover all regions."),
			mcp.DefaultString(""),
		),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Get Project Aura Summary",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.projectAuraSummaryHandler),
	}
}

func (t *AuraTools) projectAuraSummaryHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ProjectAuraSummaryRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_project_aura_summary",
		"project", args.ProjectID,
		"region", args.Region,
	)
	summary, err := t.svc.GetProjectAuraSummary(ctx, args)
	if err != nil {
		return handleServiceError("gcp_project_aura_summary", err)
	}
	result, err := mcp.NewToolResultJSON(summary)
	if err != nil {
		return nil, fmt.Errorf("gcp_project_aura_summary: marshal: %w", err)
	}
	return result, nil
}
