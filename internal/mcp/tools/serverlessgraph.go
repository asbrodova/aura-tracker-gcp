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

// ServerlessGraphTools provides the gcp_export_serverless_graph tool.
type ServerlessGraphTools struct {
	svc ports.ServerlessGraphService
	log *slog.Logger
}

func NewServerlessGraphTools(svc ports.ServerlessGraphService, log *slog.Logger) *ServerlessGraphTools {
	return &ServerlessGraphTools{svc: svc, log: log}
}

func (t *ServerlessGraphTools) Name() string { return "serverlessgraph" }

func (t *ServerlessGraphTools) GetTools() []server.ServerTool {
	return []server.ServerTool{t.ExportServerlessGraph()}
}

func (t *ServerlessGraphTools) ExportServerlessGraph() server.ServerTool {
	tool := mcp.NewTool("gcp_export_serverless_graph",
		mcp.WithDescription("Export a full serverless/event-driven resource graph for a GCP project. "+
			"Includes Cloud Run services, Cloud Run jobs, Cloud Functions (Gen 1 + 2), Eventarc triggers, "+
			"Cloud Scheduler jobs, Workflows, Cloud Tasks queues, Pub/Sub topics and subscriptions, "+
			"Secret Manager secrets, Serverless VPC connectors, and Cloud SQL instances — "+
			"connected by dependency edges (triggers, routes_to, dead_letters_to, reads_secret, etc.)."),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("region",
			mcp.Description("Filter output to a single region (e.g. us-central1). Omit or use '-' for all regions."),
		),
		mcp.WithNumber("max_nodes",
			mcp.Description("Hard cap on returned nodes. Truncation is noted in errors[]. Default: no limit."),
			mcp.Min(1),
			mcp.Max(10000),
		),
		mcp.WithBoolean("include_references",
			mcp.Description("When true, scans Cloud Run service specs for Secret Manager references and adds reads_secret edges. Default: false."),
		),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Export Serverless Graph",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.exportServerlessGraphHandler),
	}
}

func (t *ServerlessGraphTools) exportServerlessGraphHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ExportServerlessGraphRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_export_serverless_graph",
		"project", args.ProjectID,
		"region", args.Region,
		"max_nodes", args.MaxNodes,
		"include_references", args.IncludeReferences,
	)
	resp, err := t.svc.ExportServerlessGraph(ctx, args)
	if err != nil {
		return handleServiceError("gcp_export_serverless_graph", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_export_serverless_graph: marshal: %w", err)
	}
	return result, nil
}
