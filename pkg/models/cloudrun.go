package models

type ListServicesRequest struct {
	ProjectID string `json:"project_id"`
	Region    string `json:"region"`
}

type ServiceSummary struct {
	Name         string            `json:"name"`
	Region       string            `json:"region"`
	URL          string            `json:"url"`
	LastModified string            `json:"last_modified"`
	Labels       map[string]string `json:"labels,omitempty"`
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

type ListRevisionsRequest struct {
	ProjectID   string `json:"project_id"`
	Region      string `json:"region"`
	ServiceName string `json:"service_name"`
	Limit       int    `json:"limit,omitempty"`
	ShowDeleted bool   `json:"show_deleted,omitempty"`
}

type RevisionCondition struct {
	Type               string `json:"type"`
	State              string `json:"state"`
	Reason             string `json:"reason,omitempty"`
	Severity           string `json:"severity,omitempty"`
	Message            string `json:"message,omitempty"`
	LastTransitionTime string `json:"last_transition_time,omitempty"`
}

type RevisionContainer struct {
	Name             string            `json:"name,omitempty"`
	Image            string            `json:"image"`
	ResourceLimits   map[string]string `json:"resource_limits,omitempty"`
	EnvironmentNames []string          `json:"environment_names,omitempty"`
	SecretReferences []string          `json:"secret_references,omitempty"`
	CPUIdle          bool              `json:"cpu_idle"`
	StartupCPUBoost  bool              `json:"startup_cpu_boost"`
}

// RevisionSummary is a safe operational snapshot of an immutable Cloud Run
// revision. Configuration values contribute to ConfigFingerprint but are never
// returned directly.
type RevisionSummary struct {
	Name                          string              `json:"name"`
	ServiceName                   string              `json:"service_name"`
	Region                        string              `json:"region"`
	CreateTime                    string              `json:"create_time,omitempty"`
	UpdateTime                    string              `json:"update_time,omitempty"`
	DeleteTime                    string              `json:"delete_time,omitempty"`
	Creator                       string              `json:"creator,omitempty"`
	ServiceAccount                string              `json:"service_account,omitempty"`
	MaxInstanceRequestConcurrency int32               `json:"max_instance_request_concurrency,omitempty"`
	TimeoutSeconds                int64               `json:"timeout_seconds,omitempty"`
	MinInstances                  int32               `json:"min_instances,omitempty"`
	MaxInstances                  int32               `json:"max_instances,omitempty"`
	VPCConnector                  string              `json:"vpc_connector,omitempty"`
	VPCEgress                     string              `json:"vpc_egress,omitempty"`
	ExecutionEnvironment          string              `json:"execution_environment,omitempty"`
	Reconciling                   bool                `json:"reconciling"`
	Ready                         bool                `json:"ready"`
	ConfigFingerprint             string              `json:"config_fingerprint"`
	Containers                    []RevisionContainer `json:"containers"`
	Conditions                    []RevisionCondition `json:"conditions,omitempty"`
	Labels                        map[string]string   `json:"labels,omitempty"`
}

type ListRevisionsResponse struct {
	Revisions []RevisionSummary `json:"revisions"`
	Truncated bool              `json:"truncated"`
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
	ExpectedEtag  string          `json:"-"`
}

type UpdateTrafficResponse struct {
	DryRun         bool            `json:"dry_run"`
	ServiceName    string          `json:"service_name"`
	Before         []TrafficTarget `json:"before"`
	After          []TrafficTarget `json:"after"`
	NoChangeNeeded bool            `json:"no_change_needed"`
	Description    string          `json:"description"`
	PlanID         string          `json:"plan_id,omitempty"`
	ExpiresIn      string          `json:"expires_in,omitempty"`
	StateVersion   string          `json:"-"`
}
