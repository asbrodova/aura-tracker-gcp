package tools

import (
	"errors"
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/internal/gcp"
)

func TestHandleServiceError_PermissionDenied(t *testing.T) {
	pde := &gcp.PermissionDeniedError{Op: "gke.ListClusters", Err: errors.New("denied")}

	result, err := handleServiceError("gcp_gke_list_clusters", pde)
	if err != nil {
		t.Fatalf("expected nil Go error, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil CallToolResult")
	}
	if !result.IsError {
		t.Error("expected IsError=true for permission denied")
	}
}

func TestHandleServiceError_NotFound(t *testing.T) {
	nfe := &gcp.NotFoundError{Op: "gke.GetClusterDetails", Err: errors.New("not found")}

	result, err := handleServiceError("gcp_gke_get_cluster_details", nfe)
	if err != nil {
		t.Fatalf("expected nil Go error, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil CallToolResult")
	}
	if !result.IsError {
		t.Error("expected IsError=true for not found")
	}
}

func TestHandleServiceError_Unexpected(t *testing.T) {
	unexpected := errors.New("connection reset by peer")

	result, err := handleServiceError("gcp_gke_list_clusters", unexpected)
	if err == nil {
		t.Fatal("expected non-nil Go error for unexpected error")
	}
	if result != nil {
		t.Error("expected nil result for unexpected error")
	}
}

func TestHandleServiceError_WrappedPermissionDenied(t *testing.T) {
	pde := &gcp.PermissionDeniedError{Op: "op", Err: errors.New("x")}
	wrapped := errors.Join(errors.New("outer"), pde)

	result, goErr := handleServiceError("op", wrapped)
	if goErr != nil {
		t.Fatalf("expected nil Go error for wrapped PermissionDenied, got: %v", goErr)
	}
	if result == nil || !result.IsError {
		t.Error("expected IsError=true for wrapped PermissionDenied")
	}
}
