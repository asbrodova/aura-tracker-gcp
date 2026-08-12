package models

// TaskQueueSummary is a lightweight view of a Cloud Tasks queue.
type TaskQueueSummary struct {
	Name   string `json:"name"`
	Region string `json:"region"`
	State  string `json:"state,omitempty"`
}

// ListTaskQueuesRequest lists Cloud Tasks queues.
type ListTaskQueuesRequest struct {
	ProjectID string `json:"project_id"`
	Region    string `json:"region,omitempty"` // omit or "-" for all regions
}

// ListTaskQueuesResponse holds the list result with any per-region errors.
type ListTaskQueuesResponse struct {
	Queues    []TaskQueueSummary `json:"queues"`
	Errors    []ToolError        `json:"errors,omitempty"`
	Truncated bool               `json:"truncated,omitempty"`
}
