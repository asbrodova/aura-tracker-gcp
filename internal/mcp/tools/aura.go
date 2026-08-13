package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

// auraDashboardInstruction is appended to every Aura tool result so the LLM
// presents scores as a ranked visual dashboard rather than plain prose.
const auraDashboardInstruction = `
CRITICAL FORMATTING INSTRUCTION FOR LLM: Present the data above as a ranked visual dashboard. Do not use paragraphs. Use this strict markdown template:

## 🌐 Aura Tracker Health & Cost Summary

**Resource Rankings (by Aura Score):**

[Emoji] **[Resource Type]**: [Resource Name] | Aura: **[Score]**/100
> Details: [Brief status]
> Action: [Estimated savings or fix required]

Emoji rules by Aura Score:
- 🟢 80–100 (Healthy)
- 🟡 50–79 (Warning / Over-provisioned)
- 🔴 0–49 (Critical / Idle)
- ⚪ N/A (Telemetry unavailable; do not classify as healthy or critical)
`

func auraEmoji(band models.AuraBand) string {
	switch band {
	case models.AuraBandGreen:
		return "🟢"
	case models.AuraBandYellow:
		return "🟡"
	case models.AuraBandUnavailable:
		return "⚪"
	default:
		return "🔴"
	}
}

func auraKindLabel(k models.ResourceKind) string {
	switch k {
	case models.ResourceKindCloudRun:
		return "Cloud Run"
	case models.ResourceKindBigQuery:
		return "BigQuery"
	case models.ResourceKindCloudSQL:
		return "Cloud SQL"
	case models.ResourceKindGKE:
		return "GKE"
	case models.ResourceKindGCS:
		return "GCS"
	default:
		return string(k)
	}
}

func auraStatusLabel(band models.AuraBand, reasons []string) string {
	switch band {
	case models.AuraBandGreen:
		return "Healthy"
	case models.AuraBandYellow:
		s := "Warning"
		if len(reasons) > 0 {
			s += " — " + reasons[0]
		}
		return s
	case models.AuraBandUnavailable:
		return "Unavailable"
	default:
		s := "Critical"
		if len(reasons) > 0 {
			s += " — " + reasons[0]
		}
		return s
	}
}

func auraSummaryPresentation(report models.AuraReport) (string, string) {
	if report.CoverageStatus == "unavailable" {
		status := "Unavailable"
		if len(report.Reasons) > 0 {
			status += " — " + report.Reasons[0]
		}
		return "⚪ N/A", status
	}
	return fmt.Sprintf("%s %d", auraEmoji(report.Band), report.Score), auraStatusLabel(report.Band, report.Reasons)
}

func appendAuraInstruction(r *mcp.CallToolResult) *mcp.CallToolResult {
	r.Content = append(r.Content, mcp.TextContent{
		Type: "text",
		Text: auraDashboardInstruction,
	})
	return r
}

// AuraTools provides MCP tool definitions and handlers for Aura Score operations.
type AuraTools struct {
	svc ports.AuraService
	log *slog.Logger
}

func NewAuraTools(svc ports.AuraService, log *slog.Logger) *AuraTools {
	return &AuraTools{svc: svc, log: log}
}

func (t *AuraTools) Name() string { return "aura" }

func (t *AuraTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.GetAuraScore(),
		t.ProjectAuraSummary(),
		t.GKEAuraScore(),
		t.GCSAuraScore(),
	}
}

func (t *AuraTools) GetAuraScore() server.ServerTool {
	tool := mcp.NewTool("gcp_get_aura_score",
		mcp.WithDescription(
			"Return an Aura Score (0-100) combining Cloud Monitoring health signals and utilization "+
				"efficiency for a resource. Cached 5 min. Bands: green ≥80, yellow 50-79, red <50.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("resource_kind", mcp.Required(),
			mcp.Description("Resource type: cloud_run | bigquery | cloud_sql | gke | gcs"),
			mcp.Enum("cloud_run", "bigquery", "cloud_sql", "gke", "gcs"),
		),
		mcp.WithString("resource_name", mcp.Required(),
			mcp.Description("Resource name — Cloud Run service name, Cloud SQL instance name, BigQuery dataset ID, GKE cluster name, or GCS bucket name"),
		),
		mcp.WithString("region", mcp.Required(),
			mcp.Description("GCP region (e.g. us-central1). Use empty string for BigQuery (global). For GKE, use the cluster location."),
		),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Get Aura Score",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.getAuraScoreHandler),
	}
}

func (t *AuraTools) getAuraScoreHandler(ctx context.Context, _ mcp.CallToolRequest, args models.GetAuraScoreRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_get_aura_score",
		"project", args.ProjectID,
		"kind", args.ResourceKind,
		"name", args.ResourceName,
		"region", args.Region,
	)
	report, err := t.svc.GetAuraScore(ctx, args)
	if err != nil {
		return handleServiceError("gcp_get_aura_score", err)
	}
	result, err := mcp.NewToolResultJSON(report)
	if err != nil {
		return nil, fmt.Errorf("gcp_get_aura_score: marshal: %w", err)
	}
	result = appendAuraInstruction(result)
	if report.RecommenderNote != "" {
		result.Content = append([]mcp.Content{mcp.TextContent{Type: "text", Text: "⚠️ " + report.RecommenderNote}}, result.Content...)
	}
	return result, nil
}

func (t *AuraTools) ProjectAuraSummary() server.ServerTool {
	tool := mcp.NewTool("gcp_project_aura_summary",
		mcp.WithDescription(
			"Score all Cloud Run, Cloud SQL, BigQuery, and GKE resources with Aura Scores, sorted worst-first. "+
				"Includes a pre-formatted summary block.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("region",
			mcp.Description("Limit discovery to a specific region (e.g. us-central1). Leave empty to cover all regions."),
			mcp.DefaultString(""),
		),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Get Project Aura Summary",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.projectAuraSummaryHandler),
	}
}

func (t *AuraTools) projectAuraSummaryHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ProjectAuraSummaryRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_project_aura_summary",
		"project", args.ProjectID,
		"region", args.Region,
	)
	summary, err := t.svc.GetProjectAuraSummary(ctx, args)
	if err != nil {
		return handleServiceError("gcp_project_aura_summary", err)
	}
	var sb strings.Builder
	sb.WriteString("| Resource | Type | Aura | Status |\n")
	sb.WriteString("|----------|------|------|--------|\n")
	for _, r := range summary.Resources {
		aura, status := auraSummaryPresentation(r)
		fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n",
			r.ResourceName,
			auraKindLabel(r.ResourceKind),
			aura,
			status,
		)
	}
	fmt.Fprintf(&sb, "\nTotal: %d  🔴 Critical: %d  🟡 Warning: %d  🟢 Healthy: %d  ⚪ Unavailable: %d",
		summary.TotalCount, summary.CriticalCount, summary.WarningCount, summary.HealthyCount, summary.UnavailableCount,
	)
	for _, warning := range summary.Warnings {
		fmt.Fprintf(&sb, "\n\n⚠️ %s", warning)
	}
	result := mcp.NewToolResultText(sb.String())
	if quotaNote := projectAuraRecommenderNote(summary.Resources); quotaNote != "" {
		result.Content = append([]mcp.Content{mcp.TextContent{Type: "text", Text: "⚠️ " + quotaNote}}, result.Content...)
	}
	return result, nil
}

// projectAuraRecommenderNote condenses per-resource quota state into one
// project-level message. Quota gates are scoped by recommender, so use the
// latest known deadline when describing when full coverage should be restored.
func projectAuraRecommenderNote(resources []models.AuraReport) string {
	var latestRetryAt time.Time
	var fallback string
	for _, resource := range resources {
		if resource.RecommenderNote != "" && fallback == "" {
			fallback = resource.RecommenderNote
		}
		if resource.RecommenderRetryAt.After(latestRetryAt) {
			latestRetryAt = resource.RecommenderRetryAt
		}
	}
	if latestRetryAt.IsZero() {
		return fallback
	}
	return fmt.Sprintf(
		"RECOMMENDER QUOTA EXHAUSTED: Recommender signals are unavailable for some resources; Aura scores use the remaining telemetry. All currently known quota windows should reopen by %s; requests resume automatically as each window resets.",
		latestRetryAt.UTC().Format(time.RFC3339),
	)
}

func (t *AuraTools) GKEAuraScore() server.ServerTool {
	tool := mcp.NewTool("gcp_gke_get_aura_score",
		mcp.WithDescription(
			"Deep Aura Score for a single GKE cluster. Returns health signals (CPU, memory, "+
				"pod restarts, control-plane status, version drift vs. release channel) plus a "+
				"per-node-pool autoscaling audit. Score 0–100; bands: green ≥80, yellow 50–79, red <50. "+
				"Not cached — use for fresh deep-dives after gcp_project_aura_summary flags a cluster.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("location", mcp.Required(), mcp.Description("Cluster region or zone, e.g. us-central1")),
		mcp.WithString("cluster_name", mcp.Required(), mcp.Description("GKE cluster name")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "GKE Cluster Aura Score",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.gkeAuraScoreHandler),
	}
}

func (t *AuraTools) gkeAuraScoreHandler(ctx context.Context, _ mcp.CallToolRequest, args models.GetGKEAuraScoreRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_gke_get_aura_score",
		"project", args.ProjectID,
		"location", args.Location,
		"cluster", args.ClusterName,
	)
	report, err := t.svc.GetGKEAuraScore(ctx, args)
	if err != nil {
		return handleServiceError("gcp_gke_get_aura_score", err)
	}
	result, err := mcp.NewToolResultJSON(report)
	if err != nil {
		return nil, fmt.Errorf("gcp_gke_get_aura_score: marshal: %w", err)
	}
	return appendAuraInstruction(result), nil
}

func (t *AuraTools) GCSAuraScore() server.ServerTool {
	tool := mcp.NewTool("gcp_gcs_get_aura_score",
		mcp.WithDescription(
			"Security and cost-efficiency Aura Score for a GCS bucket. Evaluates: "+
				"Public Access Prevention (PAP), Uniform Bucket-Level Access (UBLA), "+
				"object versioning, lifecycle management rules, and storage class fit. "+
				"Returns a security_posture enum (COMPLIANT | AT_RISK | CRITICAL) and "+
				"actionable reasons. Score 0–100; health sub-score reflects security posture, "+
				"efficiency sub-score reflects cost optimisation.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("bucket_name", mcp.Required(), mcp.Description("GCS bucket name")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "GCS Bucket Aura Score",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.gcsAuraScoreHandler),
	}
}

func (t *AuraTools) gcsAuraScoreHandler(ctx context.Context, _ mcp.CallToolRequest, args models.GetGCSAuraScoreRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_gcs_get_aura_score",
		"project", args.ProjectID,
		"bucket", args.BucketName,
	)
	report, err := t.svc.GetGCSAuraScore(ctx, args)
	if err != nil {
		return handleServiceError("gcp_gcs_get_aura_score", err)
	}
	result, err := mcp.NewToolResultJSON(report)
	if err != nil {
		return nil, fmt.Errorf("gcp_gcs_get_aura_score: marshal: %w", err)
	}
	return appendAuraInstruction(result), nil
}
