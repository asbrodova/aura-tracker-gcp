package gcp

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func flowPayload(srcWorkload, dstWorkload string, services []any) map[string]any {
	dest := map[string]any{
		"cluster": map[string]any{"cluster_name": "prod", "cluster_location": "us-central1"},
		"pod":     map[string]any{"pod_namespace": "shop", "pod_workload": map[string]any{"workload_name": dstWorkload, "workload_type": "DEPLOYMENT"}},
	}
	if services != nil {
		dest["service"] = services
	}
	return map[string]any{
		"connection": map[string]any{"dest_port": float64(8080), "protocol": float64(6)},
		"bytes_sent": "2048",
		"src_gke_details": map[string]any{
			"cluster": map[string]any{"cluster_name": "prod", "cluster_location": "us-central1"},
			"pod":     map[string]any{"pod_namespace": "shop", "pod_workload": map[string]any{"workload_name": srcWorkload}},
		},
		"dest_gke_details": dest,
	}
}

func TestFlowLogEdgeFromPayload(t *testing.T) {
	edge, ok := flowLogEdgeFromPayload(flowPayload("web", "cart", []any{map[string]any{"service_name": "cart", "service_namespace": "shop"}}))
	if !ok {
		t.Fatal("payload rejected")
	}
	if edge.Source.Workload != "web" || edge.Destination.Service != "cart" || edge.Destination.Workload != "cart" ||
		edge.Port != 8080 || edge.Protocol != 6 || edge.Bytes != 2048 || edge.Records != 1 || edge.Source.Location != "us-central1" {
		t.Fatalf("edge = %+v", edge)
	}

	ambiguous, ok := flowLogEdgeFromPayload(flowPayload("web", "cart", []any{map[string]any{"service_name": "a"}, map[string]any{"service_name": "b"}}))
	if !ok || ambiguous.Destination.Service != "" {
		t.Fatalf("several selecting Services should leave the Service unset: %+v ok=%t", ambiguous, ok)
	}
	if _, ok := flowLogEdgeFromPayload(flowPayload("", "cart", nil)); ok {
		t.Fatal("a record without a source workload was accepted")
	}
	if _, ok := flowLogEdgeFromPayload(map[string]any{"connection": map[string]any{}}); ok {
		t.Fatal("a non-GKE record was accepted")
	}
}

func TestFlowAggregateMergesAndCapsConnections(t *testing.T) {
	aggregate := newFlowAggregate()
	for i := 0; i < 3; i++ {
		if !aggregate.add(flowPayload("web", "cart", nil)) {
			t.Fatal("a known connection was refused")
		}
	}
	edges := aggregate.edges()
	if len(edges) != 1 || edges[0].Records != 3 || edges[0].Bytes != 3*2048 {
		t.Fatalf("edges = %+v", edges)
	}
	for i := 0; len(aggregate.byKey) < flowLogMaxEdges; i++ {
		aggregate.add(flowPayload("web", "dest-"+strconv.Itoa(i), nil))
	}
	if aggregate.add(flowPayload("web", "one-too-many", nil)) {
		t.Fatal("a connection beyond the cap was accepted")
	}
	if !aggregate.add(flowPayload("web", "cart", nil)) {
		t.Fatal("records for a known connection must still count after the cap")
	}
}

func TestFlowLogFilterIsScopedAndEscaped(t *testing.T) {
	start := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	filter := flowLogFilter("my-project", start, start.Add(24*time.Hour))
	for _, want := range []string{
		`logName="projects/my-project/logs/compute.googleapis.com%2Fvpc_flows"`,
		`timestamp>="2026-11-18T00:00:00Z"`,
		`jsonPayload.src_gke_details.pod.pod_workload.workload_name:*`,
		`jsonPayload.dest_gke_details.service.service_name:*`,
	} {
		if !strings.Contains(filter, want) {
			t.Errorf("filter %q missing %q", filter, want)
		}
	}
	if !strings.Contains(flowLogFilter(`x" OR logName:*`, start, start), `projects/x\" OR logName:*/logs`) {
		t.Error("project ID is not escaped")
	}
}
