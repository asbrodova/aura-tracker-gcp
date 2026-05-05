package models

// SchedulerJobSummary is a lightweight view of a Cloud Scheduler job.
type SchedulerJobSummary struct {
	Name        string `json:"name"`
	Region      string `json:"region"`
	Schedule    string `json:"schedule,omitempty"`
	TimeZone    string `json:"time_zone,omitempty"`
	State       string `json:"state,omitempty"`
	TargetKind  string `json:"target_kind"`  // http | pubsub | app_engine
	TargetRef   string `json:"target_ref"`   // URI or topic name
	Description string `json:"description,omitempty"`
}

// ListSchedulerJobsRequest lists Cloud Scheduler jobs.
type ListSchedulerJobsRequest struct {
	ProjectID string `json:"project_id"`
	Region    string `json:"region,omitempty"` // omit or "-" for all regions
}

// ListSchedulerJobsResponse holds the list result with any per-region errors.
type ListSchedulerJobsResponse struct {
	Jobs   []SchedulerJobSummary `json:"jobs"`
	Errors []ToolError           `json:"errors,omitempty"`
}
