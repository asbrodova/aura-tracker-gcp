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

// TasksTools provides MCP tool definitions and handlers for Cloud Tasks operations.
type TasksTools struct {
	svc ports.GCPService
	log *slog.Logger
}

func NewTasksTools(svc ports.GCPService, log *slog.Logger) *TasksTools {
	return &TasksTools{svc: svc, log: log}
}

func (t *TasksTools) Name() string { return "tasks" }

func (t *TasksTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.ListTaskQueues(),
	}
}

func (t *TasksTools) ListTaskQueues() server.ServerTool {
	tool := mcp.NewTool("gcp_tasks_list_queues",
		mcp.WithDescription("List Cloud Tasks queues in a GCP project. Use region='-' or omit for all regions."),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("region", mcp.Description("GCP region (e.g. us-central1). Omit or use '-' for all regions.")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Cloud Tasks Queues",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listTaskQueuesHandler),
	}
}

func (t *TasksTools) listTaskQueuesHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListTaskQueuesRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_tasks_list_queues", "project", args.ProjectID, "region", args.Region)
	resp, err := t.svc.ListTaskQueues(ctx, args)
	if err != nil {
		return handleServiceError("gcp_tasks_list_queues", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_tasks_list_queues: marshal: %w", err)
	}
	return result, nil
}
