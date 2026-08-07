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
	svc ports.LoggingService
	log *slog.Logger
}

func NewLoggingTools(svc ports.LoggingService, log *slog.Logger) *LoggingTools {
	return &LoggingTools{svc: svc, log: log}
}

func (t *LoggingTools) Name() string { return "logging" }

func (t *LoggingTools) GetTools() []server.ServerTool {
	return []server.ServerTool{t.QueryRecent()}
}

func (t *LoggingTools) QueryRecent() server.ServerTool {
	tool := mcp.NewTool("gcp_logging_query_recent",
		mcp.WithDescription("Fetch recent Cloud Logging entries using either a native filter or structured resource selectors. Returns resource labels, request status/latency, trace IDs, and bounded payload text for incident correlation."),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("filter", mcp.Description("Optional native Cloud Logging filter. A bounded timestamp clause is always appended. Maximum 4096 bytes.")),
		mcp.WithString("resource_type", mcp.Description("Monitored resource type for structured selection, e.g. k8s_cluster or cloud_run_revision.")),
		mcp.WithString("resource_name", mcp.Description("Convenience resource identifier. Maps to cluster_name for GKE and service_name for Cloud Run. For other resources use resource_labels.")),
		mcp.WithObject("resource_labels",
			mcp.Description("Exact monitored-resource labels to match, e.g. {\"service_name\":\"payments-api\",\"location\":\"us-central1\"}."),
			mcp.AdditionalProperties(map[string]any{"type": "string"}),
			mcp.MaxProperties(10),
		),
		mcp.WithString("min_severity",
			mcp.Description("Minimum severity. Defaults to WARNING for structured queries; no implicit severity is added to native filters."),
			mcp.Enum("DEFAULT", "DEBUG", "INFO", "NOTICE", "WARNING", "ERROR", "CRITICAL", "ALERT", "EMERGENCY"),
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
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Query Recent Cloud Logs",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.queryRecentHandler),
	}
}

func (t *LoggingTools) queryRecentHandler(ctx context.Context, _ mcp.CallToolRequest, args models.QueryRecentLogsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_logging_query_recent",
		"project", args.ProjectID,
		"native_filter", args.Filter != "",
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
