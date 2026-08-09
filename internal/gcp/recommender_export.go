package gcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/bigquery"
	"cloud.google.com/go/recommender/apiv1/recommenderpb"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// recommenderIDs lists all recommender types queried for the BQ export.
var recommenderIDs = []string{
	recommenderIDCloudRunIdle,
	recommenderIDCloudSQLIdle,
	recommenderIDCloudSQLOverpro,
}

// recommendationRow is the BigQuery row schema for exported recommendations.
type recommendationRow struct {
	ResourceName      string    `bigquery:"resource_name"`
	RecommenderID     string    `bigquery:"recommender_id"`
	Subtype           string    `bigquery:"subtype"`
	Description       string    `bigquery:"description"`
	MonthlySavingsUSD float64   `bigquery:"monthly_savings_usd"`
	Priority          string    `bigquery:"priority"`
	ExportedAt        time.Time `bigquery:"exported_at"`
}

// Save implements bigquery.ValueSaver for streaming insert.
func (r *recommendationRow) Save() (map[string]bigquery.Value, string, error) {
	return map[string]bigquery.Value{
		"resource_name":       r.ResourceName,
		"recommender_id":      r.RecommenderID,
		"subtype":             r.Subtype,
		"description":         r.Description,
		"monthly_savings_usd": r.MonthlySavingsUSD,
		"priority":            r.Priority,
		"exported_at":         r.ExportedAt,
	}, "", nil
}

// ExportRecommendationsToBQ fetches all active recommendations across all supported
// recommender types and writes them to a BigQuery table via streaming insert.
func (a *gcpAdapter) ExportRecommendationsToBQ(ctx context.Context, req models.ExportRecommendationsToBQRequest) (models.ExportRecommendationsToBQResponse, error) {
	if a.rec == nil {
		return models.ExportRecommendationsToBQResponse{}, fmt.Errorf("recommender client not initialised: ensure RECOMMENDER_ENABLED is not set to 'false'")
	}
	if a.bq == nil {
		return models.ExportRecommendationsToBQResponse{}, fmt.Errorf("bigquery client not initialised: ensure the bigquery module is active")
	}
	if req.Dataset == "" {
		return models.ExportRecommendationsToBQResponse{}, fmt.Errorf("dataset is required")
	}

	if err := a.rateWait(ctx, "recommender.ExportRecommendationsToBQ"); err != nil {
		return models.ExportRecommendationsToBQResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	tableName := req.Table
	if tableName == "" {
		tableName = "gcp_recommendations"
	}

	projectID := req.ProjectID
	tbl := a.bq.DatasetInProject(projectID, req.Dataset).Table(tableName)

	// Create the table with inferred schema; ignore 409 (already exists).
	schema, err := bigquery.InferSchema(recommendationRow{})
	if err != nil {
		return models.ExportRecommendationsToBQResponse{}, fmt.Errorf("recommender.ExportRecommendationsToBQ: infer schema: %w", err)
	}
	if createErr := tbl.Create(ctx, &bigquery.TableMetadata{Schema: schema}); createErr != nil {
		var apiErr *googleapi.Error
		if !errors.As(createErr, &apiErr) || apiErr.Code != 409 {
			return models.ExportRecommendationsToBQResponse{}, wrapGCPError("recommender.ExportRecommendationsToBQ", createErr)
		}
	}

	exportedAt := time.Now().UTC()
	var rows []*recommendationRow

	// The "-" wildcard location fetches recommendations across all GCP regions.
	for _, recID := range recommenderIDs {
		parent := fmt.Sprintf("projects/%s/locations/-/recommenders/%s", projectID, recID)
		it := a.rec.ListRecommendations(ctx, &recommenderpb.ListRecommendationsRequest{
			Parent: parent,
			Filter: `stateInfo.state = "ACTIVE"`,
		})
		for {
			rec, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return models.ExportRecommendationsToBQResponse{}, wrapGCPError("recommender.ExportRecommendationsToBQ", err)
			}
			priority := "UNSPECIFIED"
			if rec.Priority != recommenderpb.Recommendation_PRIORITY_UNSPECIFIED {
				priority = rec.Priority.String()
			}
			rows = append(rows, &recommendationRow{
				ResourceName:      primaryTargetResource(rec),
				RecommenderID:     recID,
				Subtype:           classifyRecommenderID(recID),
				Description:       rec.Description,
				MonthlySavingsUSD: extractMonthlySavings(rec),
				Priority:          priority,
				ExportedAt:        exportedAt,
			})
		}
	}

	if len(rows) > 0 {
		if err := tbl.Inserter().Put(ctx, rows); err != nil {
			return models.ExportRecommendationsToBQResponse{}, wrapGCPError("recommender.ExportRecommendationsToBQ: bq insert", err)
		}
	}

	return models.ExportRecommendationsToBQResponse{
		Table:        fmt.Sprintf("%s.%s.%s", projectID, req.Dataset, tableName),
		RowsInserted: len(rows),
		ExportedAt:   exportedAt.Format(time.RFC3339),
	}, nil
}

// primaryTargetResource extracts the first target resource URN from a recommendation.
func primaryTargetResource(rec *recommenderpb.Recommendation) string {
	if rec.GetContent() == nil {
		return ""
	}
	for _, og := range rec.GetContent().OperationGroups {
		for _, op := range og.Operations {
			if op.Resource != "" {
				return op.Resource
			}
		}
	}
	return ""
}
