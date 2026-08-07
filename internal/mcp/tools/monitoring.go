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
	svc ports.MonitoringService
	log *slog.Logger
}

func NewMonitoringTools(svc ports.MonitoringService, log *slog.Logger) *MonitoringTools {
	return &MonitoringTools{svc: svc, log: log}
}

func (t *MonitoringTools) Name() string { return "monitoring" }

func (t *MonitoringTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.GetMetrics(),
		t.ListMetricDescriptors(),
		t.ListTraceServices(),
		t.ListAlertPolicies(),
		t.ListUptimeChecks(),
		t.ListSLOs(),
		t.ListDashboards(),
		t.ListTraceDependencyEdges(),
	}
}

func (t *MonitoringTools) GetMetrics() server.ServerTool {
	tool := mcp.NewTool("gcp_monitoring_get_metrics",
		mcp.WithDescription("Fetch labeled Cloud Monitoring time series. Supports multiple series, metric/resource filters, explicit aligners and reducers, and distribution summaries for reliable incident analysis."),
		mcp.WithString("metric_type", mcp.Required(), mcp.Description("Full metric type, e.g. run.googleapis.com/request_count")),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithObject("resource_labels",
			mcp.Description("Exact monitored-resource labels, e.g. {\"service_name\":\"payments-api\",\"location\":\"us-central1\"}."),
			mcp.AdditionalProperties(map[string]any{"type": "string"}),
			mcp.MaxProperties(20),
		),
		mcp.WithObject("metric_labels",
			mcp.Description("Exact metric labels, e.g. {\"response_code_class\":\"5xx\"}."),
			mcp.AdditionalProperties(map[string]any{"type": "string"}),
			mcp.MaxProperties(20),
		),
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
		mcp.WithString("per_series_aligner",
			mcp.Description("Optional point aligner. Default ALIGN_NONE preserves raw values. Use a percentile aligner for DISTRIBUTION metrics."),
			mcp.Enum("ALIGN_NONE", "ALIGN_MEAN", "ALIGN_SUM", "ALIGN_MIN", "ALIGN_MAX", "ALIGN_COUNT", "ALIGN_DELTA", "ALIGN_RATE", "ALIGN_PERCENTILE_50", "ALIGN_PERCENTILE_95", "ALIGN_PERCENTILE_99"),
		),
		mcp.WithString("cross_series_reducer",
			mcp.Description("Optional reducer across series. Requires a non-NONE aligner. Default REDUCE_NONE."),
			mcp.Enum("REDUCE_NONE", "REDUCE_MEAN", "REDUCE_SUM", "REDUCE_MIN", "REDUCE_MAX", "REDUCE_COUNT", "REDUCE_PERCENTILE_50", "REDUCE_PERCENTILE_95", "REDUCE_PERCENTILE_99"),
		),
		mcp.WithArray("group_by_fields",
			mcp.Description("Labels to retain during reduction, e.g. resource.labels.revision_name or metric.labels.response_code_class."),
			mcp.WithStringItems(),
			mcp.MaxItems(20),
		),
		mcp.WithNumber("max_time_series",
			mcp.Description("Maximum labeled series to return (1–1000). Default: 100."),
			mcp.Min(1),
			mcp.Max(1000),
		),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Get Cloud Monitoring Metrics",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
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

func (t *MonitoringTools) ListMetricDescriptors() server.ServerTool {
	tool := mcp.NewTool("gcp_monitoring_list_metric_descriptors",
		mcp.WithDescription("List Cloud Monitoring metric descriptors for a project, optionally filtered by a prefix"),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("filter", mcp.Description(`Optional filter expression, e.g. metric.type = starts_with("custom.")`)),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Metric Descriptors",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listMetricDescriptorsHandler),
	}
}

func (t *MonitoringTools) listMetricDescriptorsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListMetricDescriptorsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_monitoring_list_metric_descriptors", "project", args.ProjectID)
	resp, err := t.svc.ListMetricDescriptors(ctx, args)
	if err != nil {
		return handleServiceError("gcp_monitoring_list_metric_descriptors", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_monitoring_list_metric_descriptors: marshal: %w", err)
	}
	return result, nil
}

func (t *MonitoringTools) ListTraceServices() server.ServerTool {
	tool := mcp.NewTool("gcp_trace_list_services",
		mcp.WithDescription("List services that have sent traces to Cloud Trace. Uses the Cloud Trace API by default; set TRACE_BACKEND=monitoring to use the Monitoring metric proxy instead."),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Trace Services",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listTraceServicesHandler),
	}
}

func (t *MonitoringTools) listTraceServicesHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListTraceServicesRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_trace_list_services", "project", args.ProjectID)
	resp, err := t.svc.ListTraceServices(ctx, args)
	if err != nil {
		return handleServiceError("gcp_trace_list_services", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_trace_list_services: marshal: %w", err)
	}
	return result, nil
}

func (t *MonitoringTools) ListAlertPolicies() server.ServerTool {
	tool := mcp.NewTool("gcp_monitoring_list_alert_policies",
		mcp.WithDescription(
			"List Cloud Monitoring alert policies in a project. "+
				"Returns policy name, display name, enabled status, severity, and condition display names. "+
				"Use filter to narrow results, e.g. 'enabled=\"true\"'.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("filter", mcp.Description("Optional filter expression")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Alert Policies",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool: tool,
		Handler: mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, args models.ListAlertPoliciesRequest) (*mcp.CallToolResult, error) {
			resp, err := t.svc.ListAlertPolicies(ctx, args)
			if err != nil {
				return handleServiceError("gcp_monitoring_list_alert_policies", err)
			}
			result, err := mcp.NewToolResultJSON(resp)
			if err != nil {
				return nil, fmt.Errorf("gcp_monitoring_list_alert_policies: marshal: %w", err)
			}
			return result, nil
		}),
	}
}

func (t *MonitoringTools) ListUptimeChecks() server.ServerTool {
	tool := mcp.NewTool("gcp_monitoring_list_uptime_checks",
		mcp.WithDescription(
			"List Cloud Monitoring uptime check configurations in a project. "+
				"Returns name, display name, check period, timeout, and checker type.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Uptime Checks",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool: tool,
		Handler: mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, args models.ListUptimeChecksRequest) (*mcp.CallToolResult, error) {
			resp, err := t.svc.ListUptimeChecks(ctx, args)
			if err != nil {
				return handleServiceError("gcp_monitoring_list_uptime_checks", err)
			}
			result, err := mcp.NewToolResultJSON(resp)
			if err != nil {
				return nil, fmt.Errorf("gcp_monitoring_list_uptime_checks: marshal: %w", err)
			}
			return result, nil
		}),
	}
}

func (t *MonitoringTools) ListSLOs() server.ServerTool {
	tool := mcp.NewTool("gcp_monitoring_list_slos",
		mcp.WithDescription(
			"List Service Level Objectives (SLOs) for monitoring services in a project. "+
				"Lists all services and their SLOs; optionally filter by service_name. "+
				"Returns SLO name, display name, goal (0–1), and calendar period.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("service_name", mcp.Description("Optional service name suffix to filter results")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List SLOs",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool: tool,
		Handler: mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, args models.ListSLOsRequest) (*mcp.CallToolResult, error) {
			resp, err := t.svc.ListSLOs(ctx, args)
			if err != nil {
				return handleServiceError("gcp_monitoring_list_slos", err)
			}
			result, err := mcp.NewToolResultJSON(resp)
			if err != nil {
				return nil, fmt.Errorf("gcp_monitoring_list_slos: marshal: %w", err)
			}
			return result, nil
		}),
	}
}

func (t *MonitoringTools) ListDashboards() server.ServerTool {
	tool := mcp.NewTool("gcp_monitoring_list_dashboards",
		mcp.WithDescription(
			"List Cloud Monitoring dashboards in a project. "+
				"Returns dashboard name, display name, and etag.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Dashboards",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool: tool,
		Handler: mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, args models.ListDashboardsRequest) (*mcp.CallToolResult, error) {
			resp, err := t.svc.ListDashboards(ctx, args)
			if err != nil {
				return handleServiceError("gcp_monitoring_list_dashboards", err)
			}
			result, err := mcp.NewToolResultJSON(resp)
			if err != nil {
				return nil, fmt.Errorf("gcp_monitoring_list_dashboards: marshal: %w", err)
			}
			return result, nil
		}),
	}
}

func (t *MonitoringTools) ListTraceDependencyEdges() server.ServerTool {
	tool := mcp.NewTool("gcp_trace_list_dependency_edges",
		mcp.WithDescription(
			"Infer service-to-service dependency edges from Cloud Trace span parent/child relationships. "+
				"Scans up to 2000 traces over the lookback window (default 7 days) and returns caller→callee "+
				"pairs with sample counts. Use the results to build a service dependency graph.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithNumber("lookback_hours",
			mcp.Description("Hours of trace data to scan (1–720). Default: 168 (7 days)."),
			mcp.Min(1),
			mcp.Max(720),
		),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Trace Dependency Edges",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool: tool,
		Handler: mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, args models.ListTraceDependencyEdgesRequest) (*mcp.CallToolResult, error) {
			resp, err := t.svc.ListTraceDependencyEdges(ctx, args)
			if err != nil {
				return handleServiceError("gcp_trace_list_dependency_edges", err)
			}
			result, err := mcp.NewToolResultJSON(resp)
			if err != nil {
				return nil, fmt.Errorf("gcp_trace_list_dependency_edges: marshal: %w", err)
			}
			return result, nil
		}),
	}
}
