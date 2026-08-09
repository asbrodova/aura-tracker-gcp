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
}

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
				"Mutation: dry_run=true returns a plan; confirm_plan_id=<id> performs the write. "+
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
			mcp.Description("Step 1: list recommendations and return a confirmation plan without writing to BigQuery."),
			mcp.DefaultBool(false),
		),
		mcp.WithString("confirm_plan_id",
			mcp.Description("Step 2: plan_id from a prior dry_run call. Providing this executes the export."),
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

func (t *RecommenderExportTools) exportHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ExportRecommendationsToBQRequest) (*mcp.CallToolResult, error) {
	if args.Dataset == "" {
		args.Dataset = t.defaultDataset
	}
	t.log.InfoContext(ctx, "gcp_export_recommendations_to_bq",
		"project", args.ProjectID,
		"dataset", args.Dataset,
		"table", args.Table,
	)
	resp, err := t.svc.ExportRecommendationsToBQ(ctx, args)
	if err != nil {
		return handleServiceError("gcp_export_recommendations_to_bq", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_export_recommendations_to_bq: marshal: %w", err)
	}
	return result, nil
}
