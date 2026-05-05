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

// WorkflowsTools provides MCP tool definitions and handlers for Cloud Workflows operations.
type WorkflowsTools struct {
	svc ports.GCPService
	log *slog.Logger
}

func NewWorkflowsTools(svc ports.GCPService, log *slog.Logger) *WorkflowsTools {
	return &WorkflowsTools{svc: svc, log: log}
}

func (t *WorkflowsTools) Name() string { return "workflows" }

func (t *WorkflowsTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.ListWorkflows(),
		t.ListWorkflowExecutions(),
	}
}

func (t *WorkflowsTools) ListWorkflows() server.ServerTool {
	tool := mcp.NewTool("gcp_workflows_list",
		mcp.WithDescription("List Cloud Workflows in a GCP project. Use region='-' or omit for all regions."),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("region", mcp.Description("GCP region (e.g. us-central1). Omit or use '-' for all regions.")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Cloud Workflows",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listWorkflowsHandler),
	}
}

func (t *WorkflowsTools) listWorkflowsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListWorkflowsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_workflows_list", "project", args.ProjectID, "region", args.Region)
	resp, err := t.svc.ListWorkflows(ctx, args)
	if err != nil {
		return handleServiceError("gcp_workflows_list", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_workflows_list: marshal: %w", err)
	}
	return result, nil
}

func (t *WorkflowsTools) ListWorkflowExecutions() server.ServerTool {
	tool := mcp.NewTool("gcp_workflows_list_executions",
		mcp.WithDescription("List recent executions of a Cloud Workflow including state, start/end times, and execution ID"),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("region", mcp.Required(), mcp.Description("GCP region")),
		mcp.WithString("workflow_name", mcp.Required(), mcp.Description("Workflow name")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of executions to return (default 20)")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Workflow Executions",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listWorkflowExecutionsHandler),
	}
}

func (t *WorkflowsTools) listWorkflowExecutionsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListWorkflowExecutionsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_workflows_list_executions", "project", args.ProjectID, "region", args.Region, "workflow", args.WorkflowName)
	resp, err := t.svc.ListWorkflowExecutions(ctx, args)
	if err != nil {
		return handleServiceError("gcp_workflows_list_executions", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_workflows_list_executions: marshal: %w", err)
	}
	return result, nil
}
