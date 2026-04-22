package gcp

import (
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func makePoints(values ...float64) []models.MetricPoint {
	pts := make([]models.MetricPoint, len(values))
	for i, v := range values {
		pts[i] = models.MetricPoint{Value: v, Timestamp: "2024-01-01T00:00:00Z"}
	}
	return pts
}

func makeLogEntries(errors, warnings int) []models.LogEntry {
	entries := make([]models.LogEntry, 0, errors+warnings)
	for i := 0; i < errors; i++ {
		entries = append(entries, models.LogEntry{Severity: "ERROR", Message: "error msg"})
	}
	for i := 0; i < warnings; i++ {
		entries = append(entries, models.LogEntry{Severity: "WARNING", Message: "warn msg"})
	}
	return entries
}

func baseReq() models.GetClusterBottlenecksRequest {
	return models.GetClusterBottlenecksRequest{
		ProjectID:       "proj",
		Location:        "us-central1",
		ClusterName:     "my-cluster",
		LookbackMinutes: 30,
	}
}

func TestAggregateBottlenecks_NoBottlenecks(t *testing.T) {
	report := aggregateBottlenecks(
		baseReq(),
		models.GetMetricsResponse{Points: makePoints(0.1, 0.2, 0.3)},
		models.GetMetricsResponse{Points: makePoints(0.1, 0.2)},
		models.QueryRecentLogsResponse{Entries: makeLogEntries(0, 0)},
	)

	if report.Severity != models.SeverityNone {
		t.Errorf("expected NONE severity, got %q", report.Severity)
	}
	if len(report.Bottlenecks) != 0 {
		t.Errorf("expected no bottlenecks, got %d", len(report.Bottlenecks))
	}
}

func TestAggregateBottlenecks_HighCPU(t *testing.T) {
	report := aggregateBottlenecks(
		baseReq(),
		models.GetMetricsResponse{Points: makePoints(0.5, 0.91, 0.88)}, // peak 0.91 > 0.90 crit
		models.GetMetricsResponse{Points: makePoints(0.1)},
		models.QueryRecentLogsResponse{Entries: makeLogEntries(0, 0)},
	)

	if report.Severity != models.SeverityCritical {
		t.Errorf("expected CRITICAL, got %q", report.Severity)
	}
	if len(report.Bottlenecks) == 0 {
		t.Error("expected at least one bottleneck")
	}
	if report.Bottlenecks[0].MetricName != "cpu_allocatable_utilization" {
		t.Errorf("unexpected metric name: %q", report.Bottlenecks[0].MetricName)
	}
}

func TestAggregateBottlenecks_MediumCPU(t *testing.T) {
	report := aggregateBottlenecks(
		baseReq(),
		models.GetMetricsResponse{Points: makePoints(0.5, 0.80)}, // peak 0.80 > 0.75 warn, < 0.90 crit
		models.GetMetricsResponse{Points: makePoints(0.1)},
		models.QueryRecentLogsResponse{Entries: makeLogEntries(0, 0)},
	)

	if report.Severity != models.SeverityMedium {
		t.Errorf("expected MEDIUM, got %q", report.Severity)
	}
}

func TestAggregateBottlenecks_HighMemory(t *testing.T) {
	report := aggregateBottlenecks(
		baseReq(),
		models.GetMetricsResponse{Points: makePoints(0.1)},
		models.GetMetricsResponse{Points: makePoints(0.96)}, // > 0.95 crit
		models.QueryRecentLogsResponse{Entries: makeLogEntries(0, 0)},
	)

	if report.Severity != models.SeverityCritical {
		t.Errorf("expected CRITICAL, got %q", report.Severity)
	}
}

func TestAggregateBottlenecks_ManyErrors(t *testing.T) {
	report := aggregateBottlenecks(
		baseReq(),
		models.GetMetricsResponse{Points: makePoints(0.1)},
		models.GetMetricsResponse{Points: makePoints(0.1)},
		models.QueryRecentLogsResponse{Entries: makeLogEntries(51, 0)}, // > 50 → HIGH
	)

	if report.Severity != models.SeverityHigh {
		t.Errorf("expected HIGH, got %q", report.Severity)
	}
	if report.LogSummary.ErrorCount != 51 {
		t.Errorf("expected 51 errors, got %d", report.LogSummary.ErrorCount)
	}
}

func TestAggregateBottlenecks_Combined(t *testing.T) {
	report := aggregateBottlenecks(
		baseReq(),
		models.GetMetricsResponse{Points: makePoints(0.95)}, // CRITICAL cpu
		models.GetMetricsResponse{Points: makePoints(0.10)},
		models.QueryRecentLogsResponse{Entries: makeLogEntries(60, 5)}, // > 50 → HIGH
	)

	// CPU CRITICAL beats error HIGH
	if report.Severity != models.SeverityCritical {
		t.Errorf("expected CRITICAL, got %q", report.Severity)
	}
}

func TestAggregateBottlenecks_TopMessages(t *testing.T) {
	entries := []models.LogEntry{
		{Severity: "ERROR", Message: "OOMKilled"},
		{Severity: "ERROR", Message: "OOMKilled"},
		{Severity: "ERROR", Message: "CrashLoopBackOff"},
	}
	report := aggregateBottlenecks(
		baseReq(),
		models.GetMetricsResponse{Points: makePoints(0.1)},
		models.GetMetricsResponse{Points: makePoints(0.1)},
		models.QueryRecentLogsResponse{Entries: entries},
	)

	if len(report.LogSummary.TopMessages) == 0 {
		t.Error("expected top messages")
	}
	if report.LogSummary.TopMessages[0] != "OOMKilled" {
		t.Errorf("expected OOMKilled to be top message, got %q", report.LogSummary.TopMessages[0])
	}
}

func TestAggregateBottlenecks_EmptyMetrics(t *testing.T) {
	report := aggregateBottlenecks(
		baseReq(),
		models.GetMetricsResponse{},
		models.GetMetricsResponse{},
		models.QueryRecentLogsResponse{},
	)

	if report.Severity != models.SeverityNone {
		t.Errorf("empty metrics should produce NONE severity, got %q", report.Severity)
	}
	if report.Summary == "" {
		t.Error("summary should not be empty")
	}
}

func TestPeakValue(t *testing.T) {
	tests := []struct {
		pts      []models.MetricPoint
		expected float64
	}{
		{nil, 0},
		{makePoints(0.1, 0.9, 0.5), 0.9},
		{makePoints(0), 0},
	}
	for _, tt := range tests {
		if got := peakValue(tt.pts); got != tt.expected {
			t.Errorf("peakValue(%v) = %v, want %v", tt.pts, got, tt.expected)
		}
	}
}

func TestMaxSeverity(t *testing.T) {
	if got := maxSeverity(models.SeverityLow, models.SeverityHigh); got != models.SeverityHigh {
		t.Errorf("expected HIGH, got %q", got)
	}
	if got := maxSeverity(models.SeverityCritical, models.SeverityNone); got != models.SeverityCritical {
		t.Errorf("expected CRITICAL, got %q", got)
	}
}
