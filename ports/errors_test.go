package ports

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWrappedErrorsUnwrapAndNameOperation(t *testing.T) {
	cause := errors.New("rpc failed")
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&RetriableError{Op: "op.retry", Err: cause}, "op.retry: retriable: rpc failed"},
		{&PermissionDeniedError{Op: "op.denied", Err: cause, MissingPermissions: []string{"run.services.list"}}, "op.denied: permission denied: rpc failed"},
		{&NotFoundError{Op: "op.missing", Err: cause}, "op.missing: not found: rpc failed"},
	} {
		if got := tc.err.Error(); got != tc.want {
			t.Errorf("Error() = %q, want %q", got, tc.want)
		}
		if !errors.Is(tc.err, cause) {
			t.Errorf("%T does not unwrap to its cause", tc.err)
		}
	}
}

func TestUnwrappedErrorMessages(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&CostSourceNotConfiguredError{ProjectID: "prod"}, "cost reasoning is not configured for environment prod"},
		{&ConfirmationRequiredError{Op: "safety.ScaleDeployment", Message: "run dry_run first"}, "safety.ScaleDeployment: confirmation required: run dry_run first"},
		{&RecommenderQuotaExhaustedError{Op: "recommender.list", RetryAt: time.Now()}, "recommender.list: recommender daily quota exhausted"},
	} {
		if got := tc.err.Error(); got != tc.want {
			t.Errorf("Error() = %q, want %q", got, tc.want)
		}
	}
}

func TestErrorsAsFindsTypedErrorThroughWrapping(t *testing.T) {
	wrapped := errors.Join(errors.New("context"), &PermissionDeniedError{Op: "op", Err: errors.New("denied"), MissingPermissions: []string{"a.b.c"}})
	var denied *PermissionDeniedError
	if !errors.As(wrapped, &denied) || strings.Join(denied.MissingPermissions, ",") != "a.b.c" {
		t.Fatalf("errors.As did not find PermissionDeniedError in %v", wrapped)
	}
}
