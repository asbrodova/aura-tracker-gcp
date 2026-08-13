package diagnostics

import (
	"context"
	"testing"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/internal/testutil"
	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func TestDependencyMetricCurrentUsesNewestPointInsideExplicitWindow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	var captured models.GetMetricsRequest
	fake := &testutil.FakeGCPService{
		GetMetricsFunc: func(_ context.Context, req models.GetMetricsRequest) (models.GetMetricsResponse, error) {
			captured = req
			return models.GetMetricsResponse{Series: []models.MetricSeries{{Points: []models.MetricPoint{
				{Timestamp: now.Add(-5 * time.Minute).Format(time.RFC3339), Value: 20000},
				{Timestamp: now.Add(-time.Minute).Format(time.RFC3339), Value: 25},
				{Timestamp: now.Add(time.Minute).Format(time.RFC3339), Value: 50000},
			}}}}, nil
		},
	}
	engine := New(fake, nil, WithClock(func() time.Time { return now }))

	got, err := engine.dependencyMetricCurrent(context.Background(), models.DiagnoseIncidentRequest{
		ProjectID: "project", LookbackMinutes: 10,
	}, "pubsub.googleapis.com/subscription/num_undelivered_messages", "subscription_id", "orders")
	if err != nil {
		t.Fatal(err)
	}
	if got != 25 {
		t.Fatalf("current metric = %v, want 25", got)
	}
	if captured.StartTime != now.Add(-10*time.Minute).Format(time.RFC3339) || captured.EndTime != now.Format(time.RFC3339) {
		t.Fatalf("metric interval = [%q, %q]", captured.StartTime, captured.EndTime)
	}
	if captured.PerSeriesAligner != "ALIGN_MAX" || captured.CrossSeriesReducer != "REDUCE_MAX" {
		t.Fatalf("metric aggregation = %s/%s", captured.PerSeriesAligner, captured.CrossSeriesReducer)
	}
}
