package models

type ListServicesRequest struct {
	ProjectID string `json:"project_id"`
	Region    string `json:"region"`
}

type ServiceSummary struct {
	Name         string `json:"name"`
	Region       string `json:"region"`
	URL          string `json:"url"`
	LastModified string `json:"last_modified"`
}

type ListServicesResponse struct {
	Services []ServiceSummary `json:"services"`
}

type GetServiceDetailsRequest struct {
	ProjectID   string `json:"project_id"`
	Region      string `json:"region"`
	ServiceName string `json:"service_name"`
}

type TrafficTarget struct {
	Revision string `json:"revision"`
	Percent  int32  `json:"percent"`
	Tag      string `json:"tag,omitempty"`
}

type ServiceDetails struct {
	ServiceSummary
	Traffic        []TrafficTarget   `json:"traffic"`
	LatestRevision string            `json:"latest_revision"`
	Labels         map[string]string `json:"labels,omitempty"`
	// WrapsFunction is set when this Cloud Run service is the runtime backing a
	// Cloud Function Gen 2. Value is the function URN.
	WrapsFunction string `json:"wraps_function,omitempty"`
}

// --- Cloud Run Jobs ---

type ListJobsRequest struct {
	ProjectID string `json:"project_id"`
	Region    string `json:"region,omitempty"`
}

type JobSummary struct {
	Name            string            `json:"name"`
	Region          string            `json:"region"`
	LastModified    string            `json:"last_modified,omitempty"`
	TaskCount       int32             `json:"task_count,omitempty"`
	Parallelism     int32             `json:"parallelism,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	LatestExecution string            `json:"latest_execution,omitempty"`
}

type ListJobsResponse struct {
	Jobs []JobSummary `json:"jobs"`
}

type GetJobDetailsRequest struct {
	ProjectID string `json:"project_id"`
	Region    string `json:"region"`
	JobName   string `json:"job_name"`
}

type JobDetails struct {
	JobSummary
	Image          string `json:"image,omitempty"`
	MaxRetries     int32  `json:"max_retries,omitempty"`
	TimeoutSeconds int64  `json:"timeout_seconds,omitempty"`
	ServiceAccount string `json:"service_account,omitempty"`
}

type ListJobExecutionsRequest struct {
	ProjectID string `json:"project_id"`
	Region    string `json:"region"`
	JobName   string `json:"job_name"`
	Limit     int    `json:"limit,omitempty"`
}

type JobExecutionSummary struct {
	Name           string `json:"name"`
	StartTime      string `json:"start_time,omitempty"`
	CompletionTime string `json:"completion_time,omitempty"`
	RunningCount   int32  `json:"running_count,omitempty"`
	SucceededCount int32  `json:"succeeded_count,omitempty"`
	FailedCount    int32  `json:"failed_count,omitempty"`
}

type ListJobExecutionsResponse struct {
	Executions []JobExecutionSummary `json:"executions"`
}

type UpdateTrafficRequest struct {
	ProjectID     string          `json:"project_id"`
	Region        string          `json:"region"`
	ServiceName   string          `json:"service_name"`
	Traffic       []TrafficTarget `json:"traffic"`
	DryRun        bool            `json:"dry_run"`
	ConfirmPlanID string          `json:"confirm_plan_id,omitempty"`
}

type UpdateTrafficResponse struct {
	DryRun      bool            `json:"dry_run"`
	ServiceName string          `json:"service_name"`
	Before      []TrafficTarget `json:"before"`
	After       []TrafficTarget `json:"after"`
	Description string          `json:"description"`
	PlanID      string          `json:"plan_id,omitempty"`
	ExpiresIn   string          `json:"expires_in,omitempty"`
}
