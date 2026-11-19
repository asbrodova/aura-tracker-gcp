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

// RecommenderExportTools provides the gcp_export_recommendations_to_bq MCP tool.
type RecommenderExportTools struct {
	svc            ports.RecommenderExportService
	log            *slog.Logger
	defaultDataset string
	skipApproval   bool
}

// DisableMutationApproval makes the export execute without asking the user.
// Only for SAFETY_ENABLED=false (local stdio), where svc is the raw adapter
// without the safety decorator.
func (t *RecommenderExportTools) DisableMutationApproval() { t.skipApproval = true }

func NewRecommenderExportTools(svc ports.RecommenderExportService, log *slog.Logger, defaultDataset ...string) *RecommenderExportTools {
	dataset := ""
	if len(defaultDataset) > 0 {
		dataset = defaultDataset[0]
	}
	return &RecommenderExportTools{svc: svc, log: log, defaultDataset: dataset}
}

func (t *RecommenderExportTools) Name() string { return "recommender_export" }

func (t *RecommenderExportTools) GetTools() []server.ServerTool {
	return []server.ServerTool{t.ExportRecommendationsToBQ()}
}

func (t *RecommenderExportTools) ExportRecommendationsToBQ() server.ServerTool {
	datasetOptions := []mcp.PropertyOption{
		mcp.Description("BigQuery dataset name to write recommendations into (must already exist in the project)"),
	}
	if t.defaultDataset == "" {
		datasetOptions = append(datasetOptions, mcp.Required())
	} else {
		datasetOptions = append(datasetOptions, mcp.DefaultString(t.defaultDataset))
	}
	tool := mcp.NewTool("gcp_export_recommendations_to_bq",
		mcp.WithDescription(
			"Export all active GCP Recommender recommendations (Cloud Run idle, Cloud SQL idle, "+
				"Cloud SQL over-provisioned) to a BigQuery table via streaming insert. "+
				"Creates the table automatically if it does not exist. "+
				"Mutation: the server previews the export and asks the user to approve it in the MCP client; "+
				"nothing is written unless the user accepts. dry_run=true only previews. "+
				"Requires RECOMMENDER_BQ_EXPORT_ENABLED=true and the bigquery module to be active. "+
				"Note: GCP's native Recommender export requires a paid support tier; this tool works "+
				"for all tiers using the Recommender API directly.",
		),
		mcp.WithString("dataset", datasetOptions...),
		mcp.WithString("project_id",
			mcp.Description("GCP project ID. Omit to use the server default."),
		),
		mcp.WithString("table",
			mcp.Description("BigQuery table name. Defaults to 'gcp_recommendations' if omitted."),
		),
		mcp.WithBoolean("dry_run",
			mcp.Description("Only list what would be exported; never asks for approval and never writes. Default: false."),
			mcp.DefaultBool(false),
		),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Export Recommendations to BigQuery",
			ReadOnlyHint:    boolPtr(false),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(false),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.exportHandler),
	}
}

func (t *RecommenderExportTools) exportHandler(ctx context.Context, req mcp.CallToolRequest, args models.ExportRecommendationsToBQRequest) (*mcp.CallToolResult, error) {
	if args.Dataset == "" {
		args.Dataset = t.defaultDataset
	}
	t.log.InfoContext(ctx, "gcp_export_recommendations_to_bq",
		"project", args.ProjectID,
		"dataset", args.Dataset,
		"table", args.Table,
	)
	return runMutation(ctx, req, mutation[models.ExportRecommendationsToBQResponse]{
		tool:          "gcp_export_recommendations_to_bq",
		previewOnly:   args.DryRun,
		confirmPlanID: args.ConfirmPlanID,
		skipApproval:  t.skipApproval,
		preview: func(ctx context.Context) (models.ExportRecommendationsToBQResponse, error) {
			r := args
			r.DryRun, r.ConfirmPlanID = true, ""
			return t.svc.ExportRecommendationsToBQ(ctx, r)
		},
		confirm: func(ctx context.Context, planID string) (models.ExportRecommendationsToBQResponse, error) {
			return t.svc.ExportRecommendationsToBQ(ctx, models.ExportRecommendationsToBQRequest{ConfirmPlanID: planID})
		},
		execute: func(ctx context.Context) (models.ExportRecommendationsToBQResponse, error) {
			r := args
			r.DryRun, r.ConfirmPlanID = false, ""
			return t.svc.ExportRecommendationsToBQ(ctx, r)
		},
		plan: func(r models.ExportRecommendationsToBQResponse) (string, bool) { return r.PlanID, false },
		strip: func(r *models.ExportRecommendationsToBQResponse) {
			r.PlanID, r.ExpiresIn = "", ""
			r.Description = fmt.Sprintf("PREVIEW ONLY: would write %d active recommendations to BigQuery table %q.", r.RowsPlanned, r.Table)
		},
		prompt: func(r models.ExportRecommendationsToBQResponse) string {
			return fmt.Sprintf("Approve this BigQuery write? Insert %d active Recommender recommendations into table %q (the table is created if it does not exist).", r.RowsPlanned, r.Table)
		},
	})
}
