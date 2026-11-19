package models

// WorkflowSummary is a lightweight view of a Cloud Workflow.
type WorkflowSummary struct {
	Name                  string            `json:"name"`
	Region                string            `json:"region"`
	State                 string            `json:"state,omitempty"`
	RevisionID            string            `json:"revision_id,omitempty"`
	Description           string            `json:"description,omitempty"`
	LastModified          string            `json:"last_modified,omitempty"`
	Labels                map[string]string `json:"labels,omitempty"`
	ServiceAccount        string            `json:"service_account,omitempty"`
	CallLogLevel          string            `json:"call_log_level,omitempty"`
	ExecutionHistoryLevel string            `json:"execution_history_level,omitempty"`
	CryptoKeyName         string            `json:"crypto_key_name,omitempty"`
	// UserEnvVarNames lists the names of the workflow's environment
	// variables. Their values are never returned.
	UserEnvVarNames []string `json:"user_env_var_names,omitempty"`
	// SourceSHA256 is the SHA-256 of the workflow definition, so two
	// environments can be compared without returning the source itself.
	SourceSHA256 string `json:"source_sha256,omitempty"`
}

// WorkflowExecutionSummary is a lightweight view of a Workflow execution.
type WorkflowExecutionSummary struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	StartTime string `json:"start_time,omitempty"`
	EndTime   string `json:"end_time,omitempty"`
}

// ListWorkflowsRequest lists Cloud Workflows.
type ListWorkflowsRequest struct {
	ProjectID string `json:"project_id"`
	Region    string `json:"region,omitempty"` // omit or "-" for all regions
	// RequireSourceHash fetches each workflow whose definition the list call
	// did not return, so SourceSHA256 is always set. Internal callers only
	// (environment drift); it is not part of the tool schema.
	RequireSourceHash bool `json:"-"`
}

// ListWorkflowsResponse holds the list result with any per-region errors.
type ListWorkflowsResponse struct {
	Workflows []WorkflowSummary `json:"workflows"`
	Errors    []ToolError       `json:"errors,omitempty"`
	Truncated bool              `json:"truncated,omitempty"`
}

// ListWorkflowExecutionsRequest lists executions for a workflow.
type ListWorkflowExecutionsRequest struct {
	ProjectID    string `json:"project_id"`
	Region       string `json:"region"`
	WorkflowName string `json:"workflow_name"`
	Limit        int    `json:"limit,omitempty"` // default 20
}

// ListWorkflowExecutionsResponse holds the execution list.
type ListWorkflowExecutionsResponse struct {
	Executions []WorkflowExecutionSummary `json:"executions"`
}
