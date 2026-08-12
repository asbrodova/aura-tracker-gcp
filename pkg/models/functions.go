package models

// FunctionSummary is a lightweight view of a Cloud Function (Gen 1 or Gen 2).
type FunctionSummary struct {
	Name         string            `json:"name"`
	Region       string            `json:"region"`
	Generation   int               `json:"generation"`
	Runtime      string            `json:"runtime"`
	Status       string            `json:"status"`
	LastModified string            `json:"last_modified,omitempty"`
	URL          string            `json:"url,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
}

// FunctionDetails extends FunctionSummary with full configuration.
type FunctionDetails struct {
	FunctionSummary
	EntryPoint     string `json:"entry_point,omitempty"`
	Memory         string `json:"memory,omitempty"`
	Timeout        string `json:"timeout,omitempty"`
	ServiceAccount string `json:"service_account,omitempty"`
	VPCConnector   string `json:"vpc_connector,omitempty"`
	// Gen 2 only: the underlying Cloud Run service resource name.
	CloudRunService string `json:"cloud_run_service,omitempty"`
}

// ListFunctionsRequest lists Cloud Functions, optionally filtered by generation.
type ListFunctionsRequest struct {
	ProjectID  string `json:"project_id"`
	Region     string `json:"region,omitempty"`
	Generation string `json:"generation,omitempty"` // "1", "2", or "both" (default)
}

// ListFunctionsResponse holds the list result.
type ListFunctionsResponse struct {
	Functions []FunctionSummary `json:"functions"`
	Truncated bool              `json:"truncated,omitempty"`
}

// GetFunctionDetailsRequest fetches full details for a single function.
type GetFunctionDetailsRequest struct {
	ProjectID    string `json:"project_id"`
	Region       string `json:"region"`
	FunctionName string `json:"function_name"`
	Generation   int    `json:"generation,omitempty"` // 1 or 2; 0 = auto-detect
}
