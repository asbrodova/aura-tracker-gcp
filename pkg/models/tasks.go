package models

// TaskQueueSummary is a lightweight view of a Cloud Tasks queue.
type TaskQueueSummary struct {
	Name        string                `json:"name"`
	Region      string                `json:"region"`
	State       string                `json:"state,omitempty"`
	RateLimits  *TaskQueueRateLimits  `json:"rate_limits,omitempty"`
	RetryConfig *TaskQueueRetryConfig `json:"retry_config,omitempty"`
	// LoggingSamplingRatio is the fraction of task operations written to
	// Cloud Logging (0 disables logging). Omitted when logging is not configured.
	LoggingSamplingRatio *float64 `json:"logging_sampling_ratio,omitempty"`
}

// TaskQueueRateLimits is how fast the queue dispatches tasks.
type TaskQueueRateLimits struct {
	MaxDispatchesPerSecond  float64 `json:"max_dispatches_per_second,omitempty"`
	MaxBurstSize            int32   `json:"max_burst_size,omitempty"`
	MaxConcurrentDispatches int32   `json:"max_concurrent_dispatches,omitempty"`
}

// TaskQueueRetryConfig is how the queue retries failed tasks. MaxAttempts -1
// means unlimited attempts. Durations use Go duration syntax, such as "1h0m0s".
type TaskQueueRetryConfig struct {
	MaxAttempts      int32  `json:"max_attempts"`
	MaxRetryDuration string `json:"max_retry_duration,omitempty"`
	MinBackoff       string `json:"min_backoff,omitempty"`
	MaxBackoff       string `json:"max_backoff,omitempty"`
	MaxDoublings     int32  `json:"max_doublings,omitempty"`
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
