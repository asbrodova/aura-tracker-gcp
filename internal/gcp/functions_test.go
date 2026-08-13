package gcp

import (
	"errors"
	"testing"

	runpb "cloud.google.com/go/run/apiv2/runpb"

	"github.com/asbrodova/aura-tracker-gcp/ports"
)

func TestCollectFilteredRunServicesCountsAcceptedResources(t *testing.T) {
	t.Parallel()
	services := []*runpb.Service{
		{Name: "excluded-1", Labels: map[string]string{"goog-managed-by": "cloudfunctions"}},
		{Name: "run-1"},
		{Name: "excluded-2", Labels: map[string]string{"goog-managed-by": "cloudfunctions"}},
		{Name: "run-2"},
		{Name: "run-3"},
	}
	budget := filteredInventoryBudget{resultLimit: 2, scanLimit: 10}
	var accepted []string
	truncated := false
	for _, service := range services {
		include, stop := budget.consider(!isCloudFunctionRunService(service))
		if stop {
			truncated = true
			break
		}
		if include {
			accepted = append(accepted, service.Name)
		}
	}
	if len(accepted) != 2 || accepted[0] != "run-1" || accepted[1] != "run-2" || !truncated {
		t.Fatalf("accepted = %+v, truncated = %v", accepted, truncated)
	}
}

func TestCollectFilteredRunServicesSignalsIndependentScanCap(t *testing.T) {
	t.Parallel()
	services := []*runpb.Service{
		{Name: "run-1"},
		{Name: "excluded", Labels: map[string]string{"goog-managed-by": "cloudfunctions"}},
		{Name: "run-2"},
	}
	budget := filteredInventoryBudget{resultLimit: 10, scanLimit: 2}
	accepted := 0
	truncated := false
	for _, service := range services {
		include, stop := budget.consider(!isCloudFunctionRunService(service))
		if stop {
			truncated = true
			break
		}
		if include {
			accepted++
		}
	}
	if accepted != 1 || !truncated {
		t.Fatalf("accepted = %+v, truncated = %v", accepted, truncated)
	}
}

func TestFunctionAutoDetectionFallsBackOnlyForTypedNotFound(t *testing.T) {
	t.Parallel()
	if !isNotFoundError(&ports.NotFoundError{Op: "get", Err: errors.New("missing")}) {
		t.Fatal("typed NotFound was not accepted for fallback")
	}
	for _, err := range []error{
		&ports.PermissionDeniedError{Op: "get", Err: errors.New("denied")},
		&ports.RetriableError{Op: "get", Err: errors.New("unavailable")},
		errors.New("run client not initialized"),
	} {
		if isNotFoundError(err) {
			t.Fatalf("%T unexpectedly allowed fallback", err)
		}
	}
}
