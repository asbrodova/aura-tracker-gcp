package models

import (
	"encoding/json"
	"testing"
)

func TestClusterBottleneckReport_JSONRoundTrip(t *testing.T) {
	original := ClusterBottleneckReport{
		ProjectID:   "my-project",
		ClusterName: "my-cluster",
		Location:    "us-central1",
		GeneratedAt: "2024-01-01T00:00:00Z",
		Severity:    SeverityCritical,
		Bottlenecks: []ResourceBottleneck{
			{
				Resource:    "cluster/my-cluster",
				MetricName:  "cpu_allocatable_utilization",
				PeakValue:   0.92,
				Threshold:   0.90,
				Description: "CPU critical",
			},
		},
		CPUMetrics:    []MetricPoint{{Timestamp: "2024-01-01T00:00:00Z", Value: 0.92}},
		MemoryMetrics: []MetricPoint{{Timestamp: "2024-01-01T00:00:00Z", Value: 0.50}},
		LogSummary: LogSummary{
			ErrorCount:   5,
			WarningCount: 10,
			TopMessages:  []string{"OOMKilled"},
		},
		Summary: "cluster has CRITICAL issues",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded ClusterBottleneckReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.ProjectID != original.ProjectID {
		t.Errorf("ProjectID mismatch: %q vs %q", decoded.ProjectID, original.ProjectID)
	}
	if decoded.Severity != original.Severity {
		t.Errorf("Severity mismatch: %q vs %q", decoded.Severity, original.Severity)
	}
	if len(decoded.Bottlenecks) != len(original.Bottlenecks) {
		t.Errorf("Bottlenecks len mismatch: %d vs %d", len(decoded.Bottlenecks), len(original.Bottlenecks))
	}
	if decoded.LogSummary.ErrorCount != original.LogSummary.ErrorCount {
		t.Errorf("ErrorCount mismatch: %d vs %d", decoded.LogSummary.ErrorCount, original.LogSummary.ErrorCount)
	}
}

func TestScaleDeploymentRequest_JSONTags(t *testing.T) {
	req := ScaleDeploymentRequest{
		ProjectID:    "proj",
		NodePoolName: "default-pool",
		NodeCount:    3,
		DryRun:       true,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map failed: %v", err)
	}

	for _, key := range []string{"project_id", "node_pool_name", "node_count", "dry_run"} {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON key %q not found in marshalled output", key)
		}
	}
}
