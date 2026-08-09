package models

// ExportRecommendationsToBQRequest is the input for gcp_export_recommendations_to_bq.
type ExportRecommendationsToBQRequest struct {
	ProjectID     string `json:"project_id"`
	Dataset       string `json:"dataset"`         // required: BigQuery dataset name
	Table         string `json:"table,omitempty"` // default: "gcp_recommendations"
	DryRun        bool   `json:"dry_run"`
	ConfirmPlanID string `json:"confirm_plan_id,omitempty"`
	ExecutionID   string `json:"-"`
}

// ExportRecommendationsToBQResponse is returned after a successful BQ export.
type ExportRecommendationsToBQResponse struct {
	DryRun       bool   `json:"dry_run"`
	Table        string `json:"table"` // fully-qualified "project.dataset.table"
	RowsPlanned  int    `json:"rows_planned"`
	RowsInserted int    `json:"rows_inserted"`
	ExportedAt   string `json:"exported_at,omitempty"` // RFC3339 timestamp
	Description  string `json:"description"`
	PlanID       string `json:"plan_id,omitempty"`
	ExpiresIn    string `json:"expires_in,omitempty"`
}
