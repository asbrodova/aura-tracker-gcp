package ports

import (
	"fmt"
	"time"
)

// RetriableError wraps a transient GCP error (quota exhaustion, service
// unavailable) that the caller may safely retry after a back-off.
type RetriableError struct {
	Op  string
	Err error
}

func (e *RetriableError) Error() string {
	return fmt.Sprintf("%s: retriable: %v", e.Op, e.Err)
}

func (e *RetriableError) Unwrap() error { return e.Err }

// PermissionDeniedError wraps a GCP PermissionDenied error.
// MCP tool handlers check for this type and surface it as a user-visible tool error.
// MissingPermissions contains the IAM permissions extracted from the gRPC error detail.
type PermissionDeniedError struct {
	Op                 string
	Err                error
	MissingPermissions []string
}

func (e *PermissionDeniedError) Error() string {
	return fmt.Sprintf("%s: permission denied: %v", e.Op, e.Err)
}

func (e *PermissionDeniedError) Unwrap() error { return e.Err }

// NotFoundError wraps a GCP NotFound error.
type NotFoundError struct {
	Op  string
	Err error
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s: not found: %v", e.Op, e.Err)
}

func (e *NotFoundError) Unwrap() error { return e.Err }

// ConfirmationRequiredError is returned by the SafetyDecorator when a mutation
// is attempted without a valid plan confirmation.
type ConfirmationRequiredError struct {
	Op      string
	Message string
}

func (e *ConfirmationRequiredError) Error() string {
	return fmt.Sprintf("%s: confirmation required: %s", e.Op, e.Message)
}

// RecommenderQuotaExhaustedError is returned when the Cloud Recommender API
// rejects a read because a rate or daily quota window is exhausted. Callers
// must not retry before RetryAt.
type RecommenderQuotaExhaustedError struct {
	Op            string
	RecommenderID string
	RetryAt       time.Time
	Window        string
}

func (e *RecommenderQuotaExhaustedError) Error() string {
	return fmt.Sprintf("%s: recommender daily quota exhausted", e.Op)
}
