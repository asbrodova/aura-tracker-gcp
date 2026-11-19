package costreasoning

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func utcDay(value string) time.Time {
	day, err := time.Parse("2006-01-02", value)
	if err != nil {
		panic(err)
	}
	return day
}

func TestResolveWindowsYearOverYear(t *testing.T) {
	t.Parallel()
	request := models.ExplainCostRequest{ProjectID: "project", Period: "last_7_complete_days", Timezone: "UTC", Comparison: comparisonYearOverYear}
	windows, err := resolveWindows(request, time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC), 90)
	if err != nil {
		t.Fatal(err)
	}
	if !windows.baselineStart.Equal(utcDay("2025-07-31")) || !windows.baselineEnd.Equal(utcDay("2025-08-07")) {
		t.Fatalf("baseline = %v .. %v, want 2025-07-31 .. 2025-08-07", windows.baselineStart, windows.baselineEnd)
	}
	// The daily history must not stretch back a whole year to the baseline.
	if !windows.historyStart.Equal(utcDay("2026-05-09")) {
		t.Fatalf("history start = %v, want 90 days before the current end", windows.historyStart)
	}
	if windows.baselineScale() != 1 {
		t.Fatalf("equal-length year-over-year baseline scaled by %v", windows.baselineScale())
	}
}

func TestResolveWindowsPreviousPeriodHistoryStillCoversBaseline(t *testing.T) {
	t.Parallel()
	request := models.ExplainCostRequest{ProjectID: "project", Period: "last_30_complete_days", Timezone: "UTC", Comparison: comparisonPreviousPeriod}
	windows, err := resolveWindows(request, time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC), 14)
	if err != nil {
		t.Fatal(err)
	}
	if !windows.historyStart.Equal(windows.baselineStart) {
		t.Fatalf("history start = %v, want the baseline start %v", windows.historyStart, windows.baselineStart)
	}
}

func TestResolveWindowsCustomBaselineOfDifferentLength(t *testing.T) {
	t.Parallel()
	request := models.ExplainCostRequest{
		ProjectID: "project", Period: "custom", StartDate: "2026-07-01", EndDate: "2026-07-31",
		Timezone: "UTC", Comparison: comparisonCustom, BaselineStartDate: "2026-02-01", BaselineEndDate: "2026-02-28",
	}
	windows, err := resolveWindows(request, time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC), 90)
	if err != nil {
		t.Fatal(err)
	}
	if windows.currentDays != 31 || windows.baselineDays != 28 {
		t.Fatalf("days = %d / %d, want 31 / 28", windows.currentDays, windows.baselineDays)
	}
	if got := windows.baselineScale(); math.Abs(got-31.0/28.0) > 1e-12 {
		t.Fatalf("scale = %v", got)
	}
	if !windows.baselineEnd.Equal(utcDay("2026-03-01")) {
		t.Fatalf("baseline end = %v, want the day after the inclusive end date", windows.baselineEnd)
	}
}

func TestResolveWindowsRejectsInvalidCustomBaselines(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	base := models.ExplainCostRequest{ProjectID: "project", Period: "custom", StartDate: "2026-07-01", EndDate: "2026-07-31", Timezone: "UTC", Comparison: comparisonCustom}
	for name, dates := range map[string][2]string{
		"missing end":          {"2026-06-01", ""},
		"reversed":             {"2026-06-10", "2026-06-01"},
		"overlaps current":     {"2026-06-20", "2026-07-05"},
		"after current":        {"2026-08-01", "2026-08-03"},
		"longer than 366 days": {"2024-01-01", "2025-06-30"},
		"not a date":           {"June", "2026-06-30"},
	} {
		request := base
		request.BaselineStartDate, request.BaselineEndDate = dates[0], dates[1]
		if _, err := resolveWindows(request, now, 90); err == nil {
			t.Errorf("%s: custom baseline %v was accepted", name, dates)
		}
	}
}

func TestNormalizeRequestValidatesComparison(t *testing.T) {
	t.Parallel()
	for name, request := range map[string]models.ExplainCostRequest{
		"unknown comparison":            {ProjectID: "project", Comparison: "last_quarter"},
		"baseline dates without custom": {ProjectID: "project", Comparison: comparisonYearOverYear, BaselineStartDate: "2026-01-01"},
	} {
		if err := normalizeRequest(&request, "UTC"); err == nil {
			t.Errorf("%s: request was accepted", name)
		}
	}
	request := models.ExplainCostRequest{ProjectID: "project", Comparison: " Year_Over_Year "}
	if err := normalizeRequest(&request, "UTC"); err != nil || request.Comparison != comparisonYearOverYear {
		t.Fatalf("normalized comparison = %q, err = %v", request.Comparison, err)
	}
}

func TestExplainScalesACustomBaselineToTheCurrentLength(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{facts: models.BillingCostFacts{Currency: "USD", Facts: []models.CostFact{
		{Dimension: "total", Key: "total", Current: models.CostMeasure{GrossCost: 31, NetCost: 31}, Baseline: models.CostMeasure{GrossCost: 28, NetCost: 28}},
		{Dimension: "service", Key: "Cloud Run", Service: "Cloud Run", Current: models.CostMeasure{NetCost: 31}, Baseline: models.CostMeasure{NetCost: 28}},
	}}}
	response, err := New(source, nil, Config{}, WithClock(func() time.Time { return now })).Explain(context.Background(), models.ExplainCostRequest{
		ProjectID: "prod-project", Period: "custom", StartDate: "2026-07-01", EndDate: "2026-07-31",
		Comparison: comparisonCustom, BaselineStartDate: "2026-02-01", BaselineEndDate: "2026-02-28",
	})
	if err != nil {
		t.Fatal(err)
	}
	// 28 over 28 days is the same daily rate as 31 over 31 days.
	if math.Abs(response.Totals.BaselineNetCost-31) > 1e-9 || math.Abs(response.Totals.Delta) > 1e-9 {
		t.Fatalf("totals = %+v, want the baseline scaled to 31", response.Totals)
	}
	if response.Status != "no_material_change" {
		t.Fatalf("status = %q", response.Status)
	}
	if response.Scope.Comparison != comparisonCustom || response.Scope.CurrentDays != 31 || response.Scope.BaselineDays != 28 || response.Scope.BaselineScale == 0 {
		t.Fatalf("scope = %+v", response.Scope)
	}
	if !strings.Contains(strings.Join(response.Warnings, " "), "scaled by") {
		t.Fatalf("warnings do not explain the scaling: %v", response.Warnings)
	}
	if !strings.Contains(response.Summary, "the custom baseline") {
		t.Fatalf("summary = %q", response.Summary)
	}
	request := source.factsReqs[0]
	if request.BaselineStart != "2026-02-01T00:00:00Z" || request.BaselineEnd != "2026-03-01T00:00:00Z" {
		t.Fatalf("collector baseline = %s .. %s", request.BaselineStart, request.BaselineEnd)
	}
}

func TestExplainYearOverYearFlagsAnEmptyBaseline(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{facts: models.BillingCostFacts{Currency: "USD", DataThrough: "2026-08-07T00:00:00Z", Facts: []models.CostFact{
		{Dimension: "total", Key: "total", Current: models.CostMeasure{GrossCost: 50, NetCost: 50}},
	}}}
	engine := New(source, nil, Config{}, WithClock(func() time.Time { return now }))
	response, err := engine.Explain(context.Background(), models.ExplainCostRequest{ProjectID: "prod-project", Comparison: comparisonYearOverYear})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "partial" {
		t.Fatalf("status = %q, want partial for a baseline the export does not cover", response.Status)
	}
	found := false
	for _, check := range response.Coverage.Checks {
		if check.Name == "baseline_window" && check.Status == "partial" {
			found = true
		}
	}
	if !found {
		t.Fatalf("coverage checks = %+v", response.Coverage.Checks)
	}
	if !strings.Contains(response.Summary, "the same period last year") {
		t.Fatalf("summary = %q", response.Summary)
	}

	// A different comparison over the same current window is a different analysis.
	if _, err := engine.Explain(context.Background(), models.ExplainCostRequest{ProjectID: "prod-project"}); err != nil {
		t.Fatal(err)
	}
	if source.factCalls != 2 {
		t.Fatalf("fact collections = %d, want 2 (comparisons must not share a cache entry)", source.factCalls)
	}
}

func TestTrafficSignalForSupportedResources(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		anomaly models.CostTrafficAnomaly
		metric  string
		label   string
		value   string
	}{
		"cloud run": {models.CostTrafficAnomaly{Resource: "//run.googleapis.com/projects/p/locations/us-central1/services/api", SKU: "Requests"},
			"run.googleapis.com/request_count", "service_name", "api"},
		"function": {models.CostTrafficAnomaly{Resource: "//cloudfunctions.googleapis.com/projects/p/locations/europe-west1/functions/resize", SKU: "Invocations"},
			"cloudfunctions.googleapis.com/function/execution_count", "region", "europe-west1"},
		"bucket egress": {models.CostTrafficAnomaly{Resource: "//storage.googleapis.com/projects/_/buckets/media", SKU: "Network Internet Egress", UsageUnit: "gibibyte"},
			"storage.googleapis.com/network/sent_bytes_count", "bucket_name", "media"},
		"bucket operations": {models.CostTrafficAnomaly{Resource: "//storage.googleapis.com/projects/_/buckets/media", SKU: "Class A Operations", UsageUnit: "requests"},
			"storage.googleapis.com/api/request_count", "bucket_name", "media"},
		"topic": {models.CostTrafficAnomaly{Resource: "//pubsub.googleapis.com/projects/p/topics/orders", SKU: "Message Delivery Basic"},
			"pubsub.googleapis.com/topic/send_request_count", "topic_id", "orders"},
		"subscription": {models.CostTrafficAnomaly{Resource: "//pubsub.googleapis.com/projects/p/subscriptions/orders-worker", SKU: "Message Delivery Basic"},
			"pubsub.googleapis.com/subscription/sent_message_count", "subscription_id", "orders-worker"},
		"cloud sql egress": {models.CostTrafficAnomaly{Resource: "//cloudsql.googleapis.com/projects/p/instances/db", SKU: "Network Internet Egress"},
			"cloudsql.googleapis.com/database/network/sent_bytes_count", "database_id", "p:db"},
	} {
		signal, ok := trafficSignalFor(tc.anomaly)
		if !ok || signal.metricType != tc.metric || signal.labels[tc.label] != tc.value {
			t.Errorf("%s: signal = %+v ok=%t, want %s with %s=%s", name, signal, ok, tc.metric, tc.label, tc.value)
		}
	}
	for name, anomaly := range map[string]models.CostTrafficAnomaly{
		"compute instance":     {Resource: "//compute.googleapis.com/projects/p/zones/a/instances/vm", SKU: "Network Egress"},
		"cloud sql operations": {Resource: "//cloudsql.googleapis.com/projects/p/instances/db", SKU: "Requests"},
		"bare name":            {Resource: "media", SKU: "Requests"},
	} {
		if signal, ok := trafficSignalFor(anomaly); ok {
			t.Errorf("%s: unexpected signal %+v", name, signal)
		}
	}
}

func TestExplainCorroboratesBucketEgressWithMonitoring(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	bucket := "//storage.googleapis.com/projects/_/buckets/media"
	sku := "Network Internet Egress"
	source := &fakeSource{
		facts: models.BillingCostFacts{Currency: "USD", Facts: []models.CostFact{
			{Dimension: "total", Key: "total", Current: models.CostMeasure{GrossCost: 120, NetCost: 120}, Baseline: models.CostMeasure{GrossCost: 20, NetCost: 20}},
			{Dimension: "service", Key: "Cloud Storage", Service: "Cloud Storage", Current: models.CostMeasure{NetCost: 120}, Baseline: models.CostMeasure{NetCost: 20}},
			{Dimension: "resource", Key: bucket, Resource: bucket, Service: "Cloud Storage", Current: models.CostMeasure{NetCost: 120}, Baseline: models.CostMeasure{NetCost: 20}},
			{Dimension: "resource_sku", Key: bucket + " / " + sku + " [gibibyte]", Resource: bucket, Service: "Cloud Storage", SKU: sku, UsageUnit: "gibibyte",
				Current: models.CostMeasure{NetCost: 120, Usage: 1000}, Baseline: models.CostMeasure{NetCost: 20, Usage: 100}},
		}},
		metricsFn: func(request models.GetMetricsRequest) (models.GetMetricsResponse, error) {
			if request.MetricType != "storage.googleapis.com/network/sent_bytes_count" || request.ResourceLabels["bucket_name"] != "media" {
				t.Errorf("unexpected metrics request %+v", request)
			}
			value := 1e9
			if request.StartTime == "2026-07-31T00:00:00Z" {
				value = 9e9
			}
			return models.GetMetricsResponse{MetricType: request.MetricType, Series: []models.MetricSeries{{Points: []models.MetricPoint{{Value: value}}}}}, nil
		},
	}
	response, err := New(source, nil, Config{}, WithClock(func() time.Time { return now })).Explain(context.Background(), models.ExplainCostRequest{ProjectID: "prod-project"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.TrafficAnomalies) != 1 {
		t.Fatalf("traffic anomalies = %+v", response.TrafficAnomalies)
	}
	anomaly := response.TrafficAnomalies[0]
	if anomaly.MonitoringUnit != "bytes" || anomaly.MonitoringValue != 9e9 || anomaly.MonitoringBaseline != 1e9 || anomaly.Confidence != "high" {
		t.Fatalf("anomaly = %+v", anomaly)
	}
	for _, driver := range response.Drivers {
		if driver.Category != "traffic_spike" {
			continue
		}
		for _, evidence := range driver.Evidence {
			if evidence.Source == "cloud_monitoring" && evidence.Unit == "bytes" && evidence.Signal == "sent_bytes" {
				return
			}
		}
		t.Fatalf("traffic driver lacks the bytes evidence: %+v", driver.Evidence)
	}
	t.Fatalf("no traffic_spike driver in %+v", response.Drivers)
}
