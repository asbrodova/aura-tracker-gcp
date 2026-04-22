package gcp

import (
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PermissionDeniedError wraps a GCP PermissionDenied error.
// MCP tool handlers check for this type and surface it as a user-visible tool error.
type PermissionDeniedError struct {
	Op  string
	Err error
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

// wrapGCPError maps gRPC status codes to typed errors.
// op should be a dot-separated call path, e.g. "gke.ListClusters".
func wrapGCPError(op string, err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.PermissionDenied:
		return &PermissionDeniedError{Op: op, Err: err}
	case codes.NotFound:
		return &NotFoundError{Op: op, Err: err}
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}
