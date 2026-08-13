package gcp

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"google.golang.org/api/googleapi"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWrapGCPError_Nil(t *testing.T) {
	if got := wrapGCPError("op", nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestWrapGCPError_PermissionDenied(t *testing.T) {
	grpcErr := status.Error(codes.PermissionDenied, "access denied")
	err := wrapGCPError("gke.ListClusters", grpcErr)

	var pde *PermissionDeniedError
	if !errors.As(err, &pde) {
		t.Fatalf("expected *PermissionDeniedError, got %T: %v", err, err)
	}
	if pde.Op != "gke.ListClusters" {
		t.Errorf("unexpected Op: %q", pde.Op)
	}
}

func TestWrapGCPError_NotFound(t *testing.T) {
	grpcErr := status.Error(codes.NotFound, "cluster not found")
	err := wrapGCPError("gke.GetClusterDetails", grpcErr)

	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected *NotFoundError, got %T: %v", err, err)
	}
	if nfe.Op != "gke.GetClusterDetails" {
		t.Errorf("unexpected Op: %q", nfe.Op)
	}
}

func TestWrapGCPError_Other(t *testing.T) {
	grpcErr := status.Error(codes.InvalidArgument, "invalid request")
	err := wrapGCPError("gke.ListClusters", grpcErr)

	var pde *PermissionDeniedError
	if errors.As(err, &pde) {
		t.Fatal("should not be a PermissionDeniedError")
	}
	var nfe *NotFoundError
	if errors.As(err, &nfe) {
		t.Fatal("should not be a NotFoundError")
	}
	if err == nil {
		t.Fatal("expected non-nil error")
	}
}

func TestWrapGCPError_RESTStatusClassification(t *testing.T) {
	tests := []struct {
		code int
		want any
	}{
		{code: 401, want: &PermissionDeniedError{}},
		{code: 403, want: &PermissionDeniedError{}},
		{code: 404, want: &NotFoundError{}},
		{code: 408, want: &RetriableError{}},
		{code: 429, want: &RetriableError{}},
		{code: 500, want: &RetriableError{}},
		{code: 503, want: &RetriableError{}},
		{code: 599, want: &RetriableError{}},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("HTTP_%d", test.code), func(t *testing.T) {
			original := &googleapi.Error{Code: test.code, Message: "request failed"}
			err := wrapGCPError("rest.Call", fmt.Errorf("outer: %w", original))
			switch test.want.(type) {
			case *PermissionDeniedError:
				var target *PermissionDeniedError
				if !errors.As(err, &target) {
					t.Fatalf("got %T, want PermissionDeniedError", err)
				}
			case *NotFoundError:
				var target *NotFoundError
				if !errors.As(err, &target) {
					t.Fatalf("got %T, want NotFoundError", err)
				}
			case *RetriableError:
				var target *RetriableError
				if !errors.As(err, &target) {
					t.Fatalf("got %T, want RetriableError", err)
				}
			}
			if !errors.Is(err, original) {
				t.Fatal("typed error did not preserve the REST error in its unwrap chain")
			}
		})
	}
}

func TestWrapGCPError_RESTExtractsMissingPermissions(t *testing.T) {
	restErr := &googleapi.Error{
		Code:    403,
		Message: "Permission 'storage.buckets.list' denied on resource",
		Details: []any{map[string]any{
			"@type":  "type.googleapis.com/google.rpc.ErrorInfo",
			"reason": "IAM_PERMISSION_DENIED",
			"metadata": map[string]any{
				"permission":  "run.services.get",
				"permissions": []any{"artifactregistry.repositories.list", "storage.buckets.list"},
			},
		}},
		Errors: []googleapi.ErrorItem{{Reason: "forbidden", Message: "Permission iam.serviceAccounts.list denied"}},
	}
	err := wrapGCPError("rest.Call", restErr)
	var denied *PermissionDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("got %T, want PermissionDeniedError", err)
	}
	want := []string{"artifactregistry.repositories.list", "iam.serviceAccounts.list", "run.services.get", "storage.buckets.list"}
	if !reflect.DeepEqual(denied.MissingPermissions, want) {
		t.Fatalf("permissions = %v, want %v", denied.MissingPermissions, want)
	}
}

func TestWrapGCPError_GRPCExtractsMissingPermissionsAndTransientCodes(t *testing.T) {
	base := status.New(codes.PermissionDenied, "denied")
	withDetails, err := base.WithDetails(&errdetails.ErrorInfo{Metadata: map[string]string{"permission": "container.clusters.list"}})
	if err != nil {
		t.Fatal(err)
	}
	wrapped := wrapGCPError("grpc.Call", withDetails.Err())
	var denied *PermissionDeniedError
	if !errors.As(wrapped, &denied) || !reflect.DeepEqual(denied.MissingPermissions, []string{"container.clusters.list"}) {
		t.Fatalf("permission error = %#v", wrapped)
	}

	for _, code := range []codes.Code{codes.ResourceExhausted, codes.Unavailable, codes.DeadlineExceeded, codes.Aborted, codes.Internal} {
		var retriable *RetriableError
		if err := wrapGCPError("grpc.Call", status.Error(code, "transient")); !errors.As(err, &retriable) {
			t.Errorf("gRPC %s mapped to %T, want RetriableError", code, err)
		}
	}
}

func TestPermissionDeniedError_Unwrap(t *testing.T) {
	sentinel := errors.New("sentinel")
	pde := &PermissionDeniedError{Op: "test", Err: sentinel}
	if !errors.Is(pde, sentinel) {
		t.Error("Unwrap should expose inner error")
	}
}

func TestNotFoundError_Unwrap(t *testing.T) {
	sentinel := errors.New("sentinel")
	nfe := &NotFoundError{Op: "test", Err: sentinel}
	if !errors.Is(nfe, sentinel) {
		t.Error("Unwrap should expose inner error")
	}
}
