package models

// SQLInstanceSummary is a lightweight view of a Cloud SQL instance.
type SQLInstanceSummary struct {
	Name           string `json:"name"`
	DatabaseVersion string `json:"database_version,omitempty"`
	Region         string `json:"region,omitempty"`
	State          string `json:"state,omitempty"`
	Tier           string `json:"tier,omitempty"`
}

// ListSQLInstancesRequest lists Cloud SQL instances in a project.
type ListSQLInstancesRequest struct {
	ProjectID string `json:"project_id"`
}

// ListSQLInstancesResponse holds the list result.
type ListSQLInstancesResponse struct {
	Instances []SQLInstanceSummary `json:"instances"`
}
