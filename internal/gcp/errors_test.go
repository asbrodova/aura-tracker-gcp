package gcp

import (
	"errors"
	"testing"

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
	grpcErr := status.Error(codes.Internal, "internal server error")
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
