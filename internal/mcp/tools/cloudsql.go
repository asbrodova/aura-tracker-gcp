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

// CloudSQLTools provides MCP tool definitions and handlers for Cloud SQL operations.
type CloudSQLTools struct {
	svc ports.GCPService
	log *slog.Logger
}

func NewCloudSQLTools(svc ports.GCPService, log *slog.Logger) *CloudSQLTools {
	return &CloudSQLTools{svc: svc, log: log}
}

func (t *CloudSQLTools) Name() string { return "cloudsql" }

func (t *CloudSQLTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.ListSQLInstances(),
	}
}

func (t *CloudSQLTools) ListSQLInstances() server.ServerTool {
	tool := mcp.NewTool("gcp_cloudsql_list_instances",
		mcp.WithDescription("List Cloud SQL instances in a GCP project including database version, region, state, and machine tier."),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Cloud SQL Instances",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listSQLInstancesHandler),
	}
}

func (t *CloudSQLTools) listSQLInstancesHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListSQLInstancesRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_cloudsql_list_instances", "project", args.ProjectID)
	resp, err := t.svc.ListSQLInstances(ctx, args)
	if err != nil {
		return handleServiceError("gcp_cloudsql_list_instances", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_cloudsql_list_instances: marshal: %w", err)
	}
	return result, nil
}
