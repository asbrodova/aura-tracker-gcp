package gcp

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"google.golang.org/api/googleapi"
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

var permissionNamePattern = regexp.MustCompile(`(?i)\bpermissions?\s+['\"]?([a-z][a-z0-9_]*(?:\.[a-z0-9_]+){2,})`)

// wrapGCPError maps REST and gRPC status codes to typed errors.
// The op argument should be a dot-separated call path, e.g. "gke.ListClusters".
func wrapGCPError(op string, err error) error {
	if err == nil {
		return nil
	}
	var restErr *googleapi.Error
	if errors.As(err, &restErr) {
		switch {
		case restErr.Code == http.StatusUnauthorized || restErr.Code == http.StatusForbidden:
			return &PermissionDeniedError{Op: op, Err: err, MissingPermissions: extractMissingPermissions(err)}
		case restErr.Code == http.StatusNotFound:
			return &NotFoundError{Op: op, Err: err}
		case restErr.Code == http.StatusRequestTimeout || restErr.Code == http.StatusTooManyRequests || restErr.Code >= 500:
			return &RetriableError{Op: op, Err: err}
		}
	}
	switch status.Code(err) {
	case codes.PermissionDenied, codes.Unauthenticated:
		return &PermissionDeniedError{
			Op:                 op,
			Err:                err,
			MissingPermissions: extractMissingPermissions(err),
		}
	case codes.NotFound:
		return &NotFoundError{Op: op, Err: err}
	case codes.ResourceExhausted, codes.Unavailable, codes.DeadlineExceeded, codes.Aborted, codes.Internal:
		return &RetriableError{Op: op, Err: err}
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}

// extractMissingPermissions parses gRPC status details and REST error metadata
// to find IAM permission names that caused a permission-denied response.
func extractMissingPermissions(err error) []string {
	seen := make(map[string]struct{})
	add := func(permission string) {
		permission = strings.TrimSpace(permission)
		if permission != "" {
			seen[permission] = struct{}{}
		}
	}
	st, ok := status.FromError(err)
	if ok {
		for _, detail := range st.Details() {
			switch d := detail.(type) {
			case *errdetails.ErrorInfo:
				add(d.Metadata["permission"])
				for _, permission := range strings.Split(d.Metadata["permissions"], ",") {
					add(permission)
				}
			case *errdetails.PreconditionFailure:
				for _, violation := range d.Violations {
					if strings.HasPrefix(strings.ToLower(violation.Subject), "permission") {
						addPermissionsFromMessage(violation.Subject+" "+violation.Description, add)
					}
				}
			}
		}
	}
	var restErr *googleapi.Error
	if errors.As(err, &restErr) {
		collectRESTPermissionMetadata(restErr.Details, add)
		addPermissionsFromMessage(restErr.Message, add)
		for _, item := range restErr.Errors {
			addPermissionsFromMessage(item.Message, add)
		}
	}
	permissions := make([]string, 0, len(seen))
	for permission := range seen {
		permissions = append(permissions, permission)
	}
	sort.Strings(permissions)
	return permissions
}

func addPermissionsFromMessage(message string, add func(string)) {
	for _, match := range permissionNamePattern.FindAllStringSubmatch(message, -1) {
		if len(match) > 1 {
			add(match[1])
		}
	}
}

func collectRESTPermissionMetadata(value any, add func(string)) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			collectRESTPermissionMetadata(item, add)
		}
	case map[string]any:
		for key, item := range typed {
			switch strings.ToLower(key) {
			case "permission":
				if permission, ok := item.(string); ok {
					add(permission)
				}
			case "permissions":
				switch permissions := item.(type) {
				case string:
					for _, permission := range strings.Split(permissions, ",") {
						add(permission)
					}
				case []any:
					for _, permission := range permissions {
						if text, ok := permission.(string); ok {
							add(text)
						}
					}
				}
			default:
				collectRESTPermissionMetadata(item, add)
			}
		}
	}
}
