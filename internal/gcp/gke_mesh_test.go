package gcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func TestIstioMeshQueryUsesDocumentedLabelsAndLocation(t *testing.T) {
	t.Parallel()
	filter := buildIstioMeshFilter(`us-central1`, `cluster"blue`)
	if !strings.Contains(filter, `resource.labels.location="us-central1"`) {
		t.Fatalf("filter does not scope location: %s", filter)
	}
	if !strings.Contains(filter, `resource.labels.cluster_name="cluster\"blue"`) {
		t.Fatalf("filter does not escape cluster name: %s", filter)
	}
	want := []string{
		"metric.labels.source_workload_name",
		"metric.labels.source_workload_namespace",
		"metric.labels.destination_service_name",
		"metric.labels.destination_service_namespace",
	}
	if len(istioMeshGroupByFields) != len(want) {
		t.Fatalf("group-by fields = %v", istioMeshGroupByFields)
	}
	for i := range want {
		if istioMeshGroupByFields[i] != want[i] {
			t.Fatalf("group-by fields = %v", istioMeshGroupByFields)
		}
	}
}

func TestMeshRequestRateUnits(t *testing.T) {
	t.Parallel()
	if got := alignedRatesToRequestsPerMinute(4, 2); got != 120 {
		t.Fatalf("aligned rate RPM = %v, want 120", got)
	}
	start := time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC)
	if got := logCountToRequestsPerMinute(120, start, start.Add(2*time.Hour)); got != 1 {
		t.Fatalf("log-derived RPM = %v, want 1", got)
	}
}

func TestGetGKEMeshTopologyValidatesLookbackAtAdapterBoundary(t *testing.T) {
	adapter := &gcpAdapter{callTimeout: time.Second}
	_, err := adapter.GetGKEMeshTopology(context.Background(), models.GetGKEMeshTopologyRequest{
		ProjectID: "project", Location: "us-central1", ClusterName: "cluster", LookbackHours: maxMeshLookbackHours + 1,
	})
	if err == nil || !strings.Contains(err.Error(), "lookback_hours") {
		t.Fatalf("GetGKEMeshTopology error = %v", err)
	}
}
