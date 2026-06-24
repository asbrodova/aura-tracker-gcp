package gcp

import (
	"fmt"
	"strings"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/asbrodova/aura-tracker-gcp/ports"
)

// RetriableError aliases ports.RetriableError for adapter-internal use.
type RetriableError = ports.RetriableError

// PermissionDeniedError aliases ports.PermissionDeniedError for adapter-internal use.
type PermissionDeniedError = ports.PermissionDeniedError

// NotFoundError aliases ports.NotFoundError for adapter-internal use.
type NotFoundError = ports.NotFoundError

// ConfirmationRequiredError aliases ports.ConfirmationRequiredError for adapter-internal use.
type ConfirmationRequiredError = ports.ConfirmationRequiredError

// wrapGCPError maps gRPC status codes to typed errors.
// The op argument should be a dot-separated call path, e.g. "gke.ListClusters".
func wrapGCPError(op string, err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.PermissionDenied:
		return &PermissionDeniedError{
			Op:                 op,
			Err:                err,
			MissingPermissions: extractMissingPermissions(err),
		}
	case codes.NotFound:
		return &NotFoundError{Op: op, Err: err}
	case codes.ResourceExhausted, codes.Unavailable:
		return &RetriableError{Op: op, Err: err}
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}

// extractMissingPermissions parses the gRPC status details to find any IAM
// permission names that caused the PermissionDenied error. GCP APIs typically
// embed these in an ErrorInfo.Metadata["permission"] field.
func extractMissingPermissions(err error) []string {
	st, ok := status.FromError(err)
	if !ok {
		return nil
	}
	var perms []string
	for _, detail := range st.Details() {
		switch d := detail.(type) {
		case *errdetails.ErrorInfo:
			if p, ok := d.Metadata["permission"]; ok && p != "" {
				perms = append(perms, p)
			}
		case *errdetails.PreconditionFailure:
			for _, v := range d.Violations {
				if strings.HasPrefix(v.Subject, "Permission") && v.Description != "" {
					perms = append(perms, v.Description)
				}
			}
		}
	}
	return perms
}
