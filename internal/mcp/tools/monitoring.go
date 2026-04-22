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

// MonitoringTools provides MCP tool definitions and handlers for Cloud Monitoring operations.
type MonitoringTools struct {
	svc ports.GCPService
	log *slog.Logger
}

func NewMonitoringTools(svc ports.GCPService, log *slog.Logger) *MonitoringTools {
	return &MonitoringTools{svc: svc, log: log}
}

func (t *MonitoringTools) GetMetrics() server.ServerTool {
	tool := mcp.NewTool("gcp_monitoring_get_metrics",
		mcp.WithDescription("Fetch Cloud Monitoring time-series metrics for a GCP resource over a specified time window"),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("metric_type", mcp.Required(), mcp.Description("Full Cloud Monitoring metric type, e.g. kubernetes.io/container/cpu/request_utilization")),
		mcp.WithNumber("lookback_minutes",
			mcp.Description("Time window in minutes (1–1440). Default: 60."),
			mcp.Min(1),
			mcp.Max(1440),
		),
		mcp.WithNumber("alignment_period_seconds",
			mcp.Description("Aggregation alignment period in seconds. Default: 60."),
			mcp.Min(10),
			mcp.Max(86400),
		),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.getMetricsHandler),
	}
}

func (t *MonitoringTools) getMetricsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.GetMetricsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_monitoring_get_metrics",
		"project", args.ProjectID,
		"metric_type", args.MetricType,
		"lookback_minutes", args.LookbackMinutes,
	)
	resp, err := t.svc.GetMetrics(ctx, args)
	if err != nil {
		return handleServiceError("gcp_monitoring_get_metrics", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_monitoring_get_metrics: marshal: %w", err)
	}
	return result, nil
}
