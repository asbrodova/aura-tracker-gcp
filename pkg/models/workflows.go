package models

// WorkflowSummary is a lightweight view of a Cloud Workflow.
type WorkflowSummary struct {
	Name         string            `json:"name"`
	Region       string            `json:"region"`
	State        string            `json:"state,omitempty"`
	RevisionID   string            `json:"revision_id,omitempty"`
	Description  string            `json:"description,omitempty"`
	LastModified string            `json:"last_modified,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
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
}

// ListWorkflowsResponse holds the list result with any per-region errors.
type ListWorkflowsResponse struct {
	Workflows []WorkflowSummary `json:"workflows"`
	Errors    []ToolError       `json:"errors,omitempty"`
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
