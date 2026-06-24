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

// CoverageTools provides MCP tool definitions and handlers for observability coverage.
type CoverageTools struct {
	svc ports.CoverageService
	log *slog.Logger
}

func NewCoverageTools(svc ports.CoverageService, log *slog.Logger) *CoverageTools {
	return &CoverageTools{svc: svc, log: log}
}

func (t *CoverageTools) Name() string { return "coverage" }

func (t *CoverageTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.GetObservabilityCoverage(),
	}
}

func (t *CoverageTools) GetObservabilityCoverage() server.ServerTool {
	tool := mcp.NewTool("gcp_observability_coverage",
		mcp.WithDescription(
			"Roll up observability signal coverage for Cloud Run services in a project. "+
				"For each service, reports whether metrics, traces, logs, and alert policies are present, "+
				"computes a 0–1 coverage score, and returns project-wide recommendations for gaps. "+
				"Coverage score = HasMetrics×0.35 + HasTraces×0.35 + HasLogs×0.20 + HasAlerts×0.10 (+0.10 bonus for OTel sidecar).",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Observability Coverage Rollup",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool: tool,
		Handler: mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, args models.GetObservabilityCoverageRequest) (*mcp.CallToolResult, error) {
			t.log.InfoContext(ctx, "gcp_observability_coverage", "project", args.ProjectID)
			resp, err := t.svc.GetObservabilityCoverage(ctx, args)
			if err != nil {
				return handleServiceError("gcp_observability_coverage", err)
			}
			result, err := mcp.NewToolResultJSON(resp)
			if err != nil {
				return nil, fmt.Errorf("gcp_observability_coverage: marshal: %w", err)
			}
			return result, nil
		}),
	}
}
