package gcp

import (
	"testing"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func TestLatestSubscriptionGaugeValuesUsesNewestBoundedSample(t *testing.T) {
	t.Parallel()
	end := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	start := end.Add(-10 * time.Minute)
	response := models.GetMetricsResponse{Series: []models.MetricSeries{
		{
			ResourceLabels: map[string]string{"subscription_id": "projects/project/subscriptions/orders"},
			Points: []models.MetricPoint{
				{Timestamp: end.Add(-5 * time.Minute).Format(time.RFC3339), Value: 20000},
				{Timestamp: end.Add(-time.Minute).Format(time.RFC3339), Value: 25},
				{Timestamp: end.Add(time.Minute).Format(time.RFC3339), Value: 50000},
			},
		},
		{
			ResourceLabels: map[string]string{"subscription_id": "orders"},
			Points: []models.MetricPoint{
				{Timestamp: end.Add(-time.Minute).Format(time.RFC3339), Value: 30},
			},
		},
	}}

	got := latestSubscriptionGaugeValues(response, start, end)
	observation, ok := got["orders"]
	if !ok {
		t.Fatal("orders subscription was not returned")
	}
	if observation.value != 30 || !observation.timestamp.Equal(end.Add(-time.Minute)) {
		t.Fatalf("observation = %+v", observation)
	}
}
