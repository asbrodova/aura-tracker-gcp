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
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
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
	return result, nil
}

func (t *AuraTools) ProjectAuraSummary() server.ServerTool {
	tool := mcp.NewTool("gcp_project_aura_summary",
		mcp.WithDescription(
			"Score all Cloud Run, Cloud SQL, BigQuery, and GKE resources with Aura Scores, sorted worst-first. "+
				"Includes a pre-formatted summary block.",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
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
	text := summary.Summary + fmt.Sprintf(
		"\n\nTotal: %d  🔴 Critical: %d  🟡 Warning: %d  🟢 Healthy: %d",
		summary.TotalCount, summary.CriticalCount, summary.WarningCount, summary.HealthyCount,
	)
	return mcp.NewToolResultText(text), nil
}

func (t *AuraTools) GKEAuraScore() server.ServerTool {
	tool := mcp.NewTool("gcp_gke_get_aura_score",
		mcp.WithDescription(
			"Deep Aura Score for a single GKE cluster. Returns health signals (CPU, memory, "+
				"pod restarts, control-plane status, version drift vs. release channel) plus a "+
				"per-node-pool autoscaling audit. Score 0–100; bands: green ≥80, yellow 50–79, red <50. "+
				"Not cached — use for fresh deep-dives after gcp_project_aura_summary flags a cluster.",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
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
	return result, nil
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
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
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
	return result, nil
}
