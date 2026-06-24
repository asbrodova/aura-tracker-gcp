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

// SchedulerTools provides MCP tool definitions and handlers for Cloud Scheduler operations.
type SchedulerTools struct {
	svc ports.SchedulerService
	log *slog.Logger
}

func NewSchedulerTools(svc ports.SchedulerService, log *slog.Logger) *SchedulerTools {
	return &SchedulerTools{svc: svc, log: log}
}

func (t *SchedulerTools) Name() string { return "scheduler" }

func (t *SchedulerTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.ListSchedulerJobs(),
	}
}

func (t *SchedulerTools) ListSchedulerJobs() server.ServerTool {
	tool := mcp.NewTool("gcp_scheduler_list_jobs",
		mcp.WithDescription(
			"List Cloud Scheduler jobs in a GCP project. "+
				"Each job shows its schedule (cron), timezone, state, and target (HTTP URI, Pub/Sub topic, or App Engine endpoint). "+
				"Use region='-' or omit for all regions.",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("region", mcp.Description("GCP region (e.g. us-central1). Omit or use '-' for all regions.")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Cloud Scheduler Jobs",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listSchedulerJobsHandler),
	}
}

func (t *SchedulerTools) listSchedulerJobsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListSchedulerJobsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_scheduler_list_jobs", "project", args.ProjectID, "region", args.Region)
	resp, err := t.svc.ListSchedulerJobs(ctx, args)
	if err != nil {
		return handleServiceError("gcp_scheduler_list_jobs", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_scheduler_list_jobs: marshal: %w", err)
	}
	return result, nil
}
