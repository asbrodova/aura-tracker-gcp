package models

// ExportRecommendationsToBQRequest is the input for gcp_export_recommendations_to_bq.
type ExportRecommendationsToBQRequest struct {
	ProjectID string `json:"project_id"`
	Dataset   string `json:"dataset"`        // required: BigQuery dataset name
	Table     string `json:"table,omitempty"` // default: "gcp_recommendations"
}

// ExportRecommendationsToBQResponse is returned after a successful BQ export.
type ExportRecommendationsToBQResponse struct {
	Table        string `json:"table"`        // fully-qualified "project.dataset.table"
	RowsInserted int    `json:"rows_inserted"`
	ExportedAt   string `json:"exported_at"` // RFC3339 timestamp
}
