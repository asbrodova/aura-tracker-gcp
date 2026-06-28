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
	svc ports.RecommenderExportService
	log *slog.Logger
}

func NewRecommenderExportTools(svc ports.RecommenderExportService, log *slog.Logger) *RecommenderExportTools {
	return &RecommenderExportTools{svc: svc, log: log}
}

func (t *RecommenderExportTools) Name() string { return "recommender_export" }

func (t *RecommenderExportTools) GetTools() []server.ServerTool {
	return []server.ServerTool{t.ExportRecommendationsToBQ()}
}

func (t *RecommenderExportTools) ExportRecommendationsToBQ() server.ServerTool {
	tool := mcp.NewTool("gcp_export_recommendations_to_bq",
		mcp.WithDescription(
			"Export all active GCP Recommender recommendations (Cloud Run idle, Cloud SQL idle, "+
				"Cloud SQL over-provisioned) to a BigQuery table via streaming insert. "+
				"Creates the table automatically if it does not exist. "+
				"Requires RECOMMENDER_BQ_EXPORT_ENABLED=true and the bigquery module to be active. "+
				"Note: GCP's native Recommender export requires a paid support tier; this tool works "+
				"for all tiers using the Recommender API directly.",
		),
		mcp.WithString("dataset", mcp.Required(),
			mcp.Description("BigQuery dataset name to write recommendations into (must already exist in the project)"),
		),
		mcp.WithString("project_id",
			mcp.Description("GCP project ID. Omit to use the server default."),
		),
		mcp.WithString("table",
			mcp.Description("BigQuery table name. Defaults to 'gcp_recommendations' if omitted."),
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
