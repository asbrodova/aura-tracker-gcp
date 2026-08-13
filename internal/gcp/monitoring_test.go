package gcp

import (
	"context"
	"strings"
	"testing"
	"time"

	monitoringpb "cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	distributionpb "google.golang.org/genproto/googleapis/api/distribution"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func TestBuildMetricFilterPreservesLabelsDeterministically(t *testing.T) {
	filter, err := buildMetricFilter(models.GetMetricsRequest{
		MetricType: "run.googleapis.com/request_count",
		ResourceLabels: map[string]string{
			"service_name": "payments-api",
			"location":     "us-central1",
		},
		MetricLabels: map[string]string{"response_code_class": "5xx"},
	})
	if err != nil {
		t.Fatalf("buildMetricFilter: %v", err)
	}
	want := `metric.type = "run.googleapis.com/request_count" AND resource.labels.location = "us-central1" AND resource.labels.service_name = "payments-api" AND metric.labels.response_code_class = "5xx"`
	if filter != want {
		t.Fatalf("filter mismatch\n got: %s\nwant: %s", filter, want)
	}
}

func TestBuildMetricFilterRejectsInvalidKeys(t *testing.T) {
	_, err := buildMetricFilter(models.GetMetricsRequest{
		MetricType:     "custom.googleapis.com/example",
		ResourceLabels: map[string]string{"bad.key": "value"},
	})
	if err == nil {
		t.Fatal("expected invalid label key error")
	}
}

func TestMetricPointFromProtoDistribution(t *testing.T) {
	end := time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC)
	point := metricPointFromProto(&monitoringpb.Point{
		Interval: &monitoringpb.TimeInterval{EndTime: timestamppb.New(end)},
		Value: &monitoringpb.TypedValue{Value: &monitoringpb.TypedValue_DistributionValue{
			DistributionValue: &distributionpb.Distribution{
				Count: 12,
				Mean:  345.5,
				Range: &distributionpb.Distribution_Range{Min: 10, Max: 900},
			},
		}},
	})
	if point.Timestamp != "2026-08-07T10:00:00Z" || point.ValueType != "DISTRIBUTION" {
		t.Fatalf("unexpected point metadata: %+v", point)
	}
	if point.Value != 345.5 || point.Distribution == nil || point.Distribution.Count != 12 || point.Distribution.Max != 900 {
		t.Fatalf("unexpected distribution summary: %+v", point)
	}
}

func TestMetricResponsePointsUsesEverySeries(t *testing.T) {
	resp := models.GetMetricsResponse{
		Points: []models.MetricPoint{{Value: 99}},
		Series: []models.MetricSeries{
			{Points: []models.MetricPoint{{Value: 1}, {Value: 2}}},
			{Points: []models.MetricPoint{{Value: 3}}},
		},
	}
	points := metricResponsePoints(resp)
	if len(points) != 3 || points[2].Value != 3 {
		t.Fatalf("expected all labeled-series points, got %+v", points)
	}
}

func TestValidateGroupByField(t *testing.T) {
	for _, field := range []string{"resource.type", "resource.labels.revision_name", "metric.labels.response_code_class"} {
		if err := validateGroupByField(field); err != nil {
			t.Errorf("valid field %q rejected: %v", field, err)
		}
	}
	if err := validateGroupByField("resource.labels.bad-key"); err == nil {
		t.Fatal("expected invalid group-by field error")
	}
}

func TestMetricIntervalRejectsExplicitRangeBeyondLimit(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	_, _, err := metricInterval(models.GetMetricsRequest{
		StartTime: now.Add(-25 * time.Hour).Format(time.RFC3339),
		EndTime:   now.Format(time.RFC3339),
	}, now)
	if err == nil || !strings.Contains(err.Error(), "must not exceed 1440 minutes") {
		t.Fatalf("metricInterval error = %v", err)
	}
}

func TestGetMetricsRejectsNegativeBoundsBeforeCallingClient(t *testing.T) {
	adapter := &gcpAdapter{callTimeout: time.Second}
	tests := []models.GetMetricsRequest{
		{MetricType: "custom.googleapis.com/test", LookbackMinutes: -1},
		{MetricType: "custom.googleapis.com/test", AlignmentPeriodSeconds: -1},
		{MetricType: "custom.googleapis.com/test", MaxTimeSeries: -1},
	}
	for _, req := range tests {
		if _, err := adapter.GetMetrics(context.Background(), req); err == nil {
			t.Fatalf("GetMetrics(%+v) unexpectedly succeeded", req)
		}
	}
}
