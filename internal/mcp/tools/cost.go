package tools

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

type CostExplainer interface {
	Explain(context.Context, models.ExplainCostRequest) (models.ExplainCostResponse, error)
}

type CostTools struct {
	explainer CostExplainer
	log       *slog.Logger
}

func NewCostTools(explainer CostExplainer, log *slog.Logger) *CostTools {
	if log == nil {
		log = slog.Default()
	}
	return &CostTools{explainer: explainer, log: log}
}

func (t *CostTools) Name() string { return "cost" }

func (t *CostTools) GetTools() []server.ServerTool {
	return []server.ServerTool{t.ExplainCosts()}
}

func (t *CostTools) ExplainCosts() server.ServerTool {
	tool := mcp.NewTool("gcp_cost_explain",
		mcp.WithDescription("Explain why GCP usage costs changed using the detailed Cloud Billing export. Returns current and baseline net cost, historical comparison, top spend and increase contributors, deterministic cost drivers, newly billed or confirmed-new resources, idle recommendations, traffic anomalies, confidence, and coverage gaps."),
		mcp.WithString("project_id", mcp.Description("Billed GCP workload project. Omit to use the server default.")),
		mcp.WithString("period", mcp.Description("Analysis period; defaults to the last 7 complete local days."), mcp.Enum("last_7_complete_days", "last_30_complete_days", "month_to_date", "custom")),
		mcp.WithString("comparison", mcp.Description("Baseline to compare against: previous_period (default; the equal period right before), year_over_year (the same dates one year earlier), or custom (baseline_start_date to baseline_end_date). A baseline of a different length is scaled to the current period's length, so daily rates are compared."), mcp.Enum("previous_period", "year_over_year", "custom")),
		mcp.WithString("start_date", mcp.Description("Inclusive YYYY-MM-DD start date. Required only when period=custom; custom periods are limited to 366 complete days.")),
		mcp.WithString("end_date", mcp.Description("Inclusive YYYY-MM-DD end date. Required only when period=custom; must be before today and no more than 366 days after start_date.")),
		mcp.WithString("baseline_start_date", mcp.Description("Inclusive YYYY-MM-DD start of the baseline. Required only when comparison=custom.")),
		mcp.WithString("baseline_end_date", mcp.Description("Inclusive YYYY-MM-DD end of the baseline. Required only when comparison=custom; the baseline must end before the current period starts and span at most 366 days.")),
		mcp.WithString("timezone", mcp.Description("IANA timezone for complete-day boundaries, e.g. UTC or America/Los_Angeles. Defaults to server configuration.")),
		mcp.WithString("detail_level", mcp.Description("Output detail: summary, standard, or detailed (default: standard)."), mcp.Enum("summary", "standard", "detailed")),
		mcp.WithNumber("max_results", mcp.Description("Maximum findings per ranked section, 1-25 (default: 10)."), mcp.Min(1), mcp.Max(25)),
		mcp.WithBoolean("include_idle", mcp.Description("Include active cost-optimization and idle-resource recommendations (default: true)."), mcp.DefaultBool(true)),
		mcp.WithBoolean("include_traffic", mcp.Description("Detect traffic-like billing increases and corroborate them with Cloud Monitoring for Cloud Run services, Cloud Functions, Cloud Storage buckets, Pub/Sub topics and subscriptions, and Cloud SQL egress (default: true)."), mcp.DefaultBool(true)),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title: "Explain GCP Cost Changes", ReadOnlyHint: boolPtr(true), DestructiveHint: boolPtr(false),
			IdempotentHint: boolPtr(true), OpenWorldHint: boolPtr(true),
		}),
	)
	return server.ServerTool{Tool: tool, Handler: mcp.NewTypedToolHandler(t.explainHandler)}
}

func (t *CostTools) explainHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ExplainCostRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_cost_explain", "project_id", args.ProjectID, "period", args.Period, "comparison", args.Comparison)
	response, err := t.explainer.Explain(ctx, args)
	if err != nil {
		return handleServiceError("gcp_cost_explain", err)
	}
	result, err := mcp.NewToolResultJSON(response)
	if err != nil {
		return nil, fmt.Errorf("gcp_cost_explain: marshal: %w", err)
	}
	return result, nil
}
