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

// LoggingTools provides MCP tool definitions and handlers for Cloud Logging operations.
type LoggingTools struct {
	svc ports.GCPService
	log *slog.Logger
}

func NewLoggingTools(svc ports.GCPService, log *slog.Logger) *LoggingTools {
	return &LoggingTools{svc: svc, log: log}
}

func (t *LoggingTools) QueryRecent() server.ServerTool {
	tool := mcp.NewTool("gcp_logging_query_recent",
		mcp.WithDescription("Fetch recent Cloud Logging entries for a GCP resource, filtered by severity and time window"),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("resource_type", mcp.Required(), mcp.Description("GCP monitored resource type, e.g. k8s_cluster, cloud_run_revision, pubsub_topic")),
		mcp.WithString("resource_name", mcp.Required(), mcp.Description("Resource name or identifier")),
		mcp.WithString("min_severity",
			mcp.Description("Minimum log severity: DEBUG, INFO, WARNING, ERROR, CRITICAL. Default: WARNING."),
		),
		mcp.WithNumber("max_entries",
			mcp.Description("Maximum number of entries to return (1–500). Default: 50."),
			mcp.Min(1),
			mcp.Max(500),
		),
		mcp.WithNumber("lookback_minutes",
			mcp.Description("How far back to query in minutes (1–1440). Default: 60."),
			mcp.Min(1),
			mcp.Max(1440),
		),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.queryRecentHandler),
	}
}

func (t *LoggingTools) queryRecentHandler(ctx context.Context, _ mcp.CallToolRequest, args models.QueryRecentLogsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_logging_query_recent",
		"project", args.ProjectID,
		"resource_type", args.ResourceType,
		"resource_name", args.ResourceName,
		"min_severity", args.MinSeverity,
	)
	resp, err := t.svc.QueryRecentLogs(ctx, args)
	if err != nil {
		return handleServiceError("gcp_logging_query_recent", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_logging_query_recent: marshal: %w", err)
	}
	return result, nil
}
