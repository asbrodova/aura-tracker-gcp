package costreasoning

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

type fakeSource struct {
	facts      models.BillingCostFacts
	factsErr   error
	recs       models.ListCostRecommendationsResponse
	recsErr    error
	assets     models.ListCreatedAssetsResponse
	assetsErr  error
	metrics    models.GetMetricsResponse
	metricsErr error
	metricsFn  func(models.GetMetricsRequest) (models.GetMetricsResponse, error)
	factCalls  int
	recsCalls  int
}

func (f *fakeSource) CollectCostFacts(_ context.Context, _ models.CollectCostFactsRequest) (models.BillingCostFacts, error) {
	f.factCalls++
	return f.facts, f.factsErr
}
func (f *fakeSource) ListCostRecommendations(_ context.Context, _ models.ListCostRecommendationsRequest) (models.ListCostRecommendationsResponse, error) {
	f.recsCalls++
	return f.recs, f.recsErr
}
func (f *fakeSource) ListCreatedAssets(_ context.Context, _ models.ListCreatedAssetsRequest) (models.ListCreatedAssetsResponse, error) {
	return f.assets, f.assetsErr
}

func (f *fakeSource) GetMetrics(_ context.Context, request models.GetMetricsRequest) (models.GetMetricsResponse, error) {
	if f.metricsFn != nil {
		return f.metricsFn(request)
	}
	return f.metrics, f.metricsErr
}

func TestExplainFindsNewResourceIdleRecommendationAndTraffic(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	resource := "//run.googleapis.com/projects/prod-project/locations/us-central1/services/checkout"
	source := &fakeSource{
		facts: models.BillingCostFacts{
			Currency: "USD", DataThrough: "2026-08-07T00:00:00Z", ExportStart: "2026-05-09T00:00:00Z",
			BytesProcessed: 1234, ResourceAttributedNetCost: 100, TotalNetCostInHistory: 120,
			Facts: []models.CostFact{
				{Dimension: "total", Key: "total", Current: models.CostMeasure{GrossCost: 125, Credits: -5, NetCost: 120}, Baseline: models.CostMeasure{GrossCost: 42, Credits: -2, NetCost: 40}},
				{Dimension: "service", Key: "Cloud Run", Service: "Cloud Run", Current: models.CostMeasure{NetCost: 120}, Baseline: models.CostMeasure{NetCost: 40}},
				{Dimension: "resource", Key: resource, Resource: resource, Service: "Cloud Run", Location: "us-central1", Current: models.CostMeasure{NetCost: 100}, Baseline: models.CostMeasure{NetCost: 20}},
				{Dimension: "resource_sku", Key: resource + " / Requests [requests]", Resource: resource, Service: "Cloud Run", SKU: "Requests", UsageUnit: "requests", Current: models.CostMeasure{NetCost: 100, Usage: 1000}, Baseline: models.CostMeasure{NetCost: 20, Usage: 100}},
			},
			FirstSeen: []models.ResourceFirstSeen{{Resource: resource, Service: "Cloud Run", FirstSeen: "2026-08-02T00:00:00Z", CurrentCost: 100}},
		},
		assets: models.ListCreatedAssetsResponse{Assets: []models.CreatedAsset{{Name: resource, CreateTime: "2026-08-01T12:00:00Z"}}},
		recs:   models.ListCostRecommendationsResponse{Available: true, Complete: true, Recommendations: []models.CostRecommendation{{Resource: resource, Service: "Cloud Run", Subtype: "idle", MonthlySavingsUSD: 30}}},
		metricsFn: func(request models.GetMetricsRequest) (models.GetMetricsResponse, error) {
			value := 100.0
			if request.StartTime == "2026-07-31T00:00:00Z" {
				value = 500
			}
			return models.GetMetricsResponse{MetricType: "run.googleapis.com/request_count", Series: []models.MetricSeries{{Points: []models.MetricPoint{{Value: value}}}}}, nil
		},
	}
	response, err := New(source, nil, Config{}, WithClock(func() time.Time { return now })).Explain(context.Background(), models.ExplainCostRequest{ProjectID: "prod-project"})
	if err != nil {
		t.Fatalf("Explain() error = %v", err)
	}
	if response.Totals.Delta != 80 || response.Totals.CurrentNetCost != 120 {
		t.Fatalf("unexpected totals: %+v", response.Totals)
	}
	if len(response.NewResources) != 1 || response.NewResources[0].Classification != "confirmed_new" || response.NewResources[0].Confidence != "high" {
		t.Fatalf("new resources = %+v", response.NewResources)
	}
	if len(response.IdleResources) != 1 || response.IdleResources[0].CurrentCost != 100 {
		t.Fatalf("idle resources = %+v", response.IdleResources)
	}
	if len(response.TrafficAnomalies) != 1 || response.TrafficAnomalies[0].MonitoringValue != 500 ||
		response.TrafficAnomalies[0].MonitoringBaseline != 100 || response.TrafficAnomalies[0].Confidence != "high" {
		t.Fatalf("traffic anomalies = %+v", response.TrafficAnomalies)
	}
	if len(response.Drivers) == 0 || response.Drivers[0].Category != "confirmed_new" {
		t.Fatalf("drivers = %+v", response.Drivers)
	}
	if response.Coverage.BytesProcessed != 1234 || response.Status != "complete" {
		t.Fatalf("coverage/status = %+v / %q", response.Coverage, response.Status)
	}
}

func TestExplainRetainsBillingEvidenceWhenOptionalSourcesFail(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{
		facts: models.BillingCostFacts{Currency: "USD", Facts: []models.CostFact{
			{Dimension: "total", Key: "total", Current: models.CostMeasure{NetCost: 20}, Baseline: models.CostMeasure{NetCost: 10}},
			{Dimension: "service", Key: "Compute Engine", Service: "Compute Engine", Current: models.CostMeasure{NetCost: 20}, Baseline: models.CostMeasure{NetCost: 10}},
		}},
		assetsErr: errors.New("permission denied"), recsErr: errors.New("quota exhausted"),
	}
	response, err := New(source, nil, Config{}, WithClock(func() time.Time { return now })).Explain(context.Background(), models.ExplainCostRequest{ProjectID: "prod-project"})
	if err != nil {
		t.Fatalf("Explain() error = %v", err)
	}
	if response.Status != "partial" || response.Totals.Delta != 10 || len(response.Drivers) == 0 {
		t.Fatalf("billing evidence was not retained: %+v", response)
	}
}

func TestExplainQuotaDegradationIsPartialAndCachedOnlyUntilRetry(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	retryAt := now.Add(2 * time.Minute)
	source := &fakeSource{
		facts: models.BillingCostFacts{Currency: "USD", Facts: []models.CostFact{
			{Dimension: "total", Key: "total", Current: models.CostMeasure{NetCost: 20}, Baseline: models.CostMeasure{NetCost: 10}},
		}},
		recsErr: &ports.RecommenderQuotaExhaustedError{Op: "cost recommendations", RetryAt: retryAt},
	}
	engine := New(source, nil, Config{}, WithClock(func() time.Time { return now }))
	request := models.ExplainCostRequest{ProjectID: "prod-project", IncludeTraffic: boolPointer(false)}

	response, err := engine.Explain(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "partial" {
		t.Fatalf("status = %q, want partial", response.Status)
	}
	wantRetry := retryAt.Format(time.RFC3339)
	foundCoverage, foundWarning := false, false
	for _, check := range response.Coverage.Checks {
		if check.Name == "recommender" && check.Status == "partial" && strings.Contains(check.Message, wantRetry) {
			foundCoverage = true
		}
	}
	for _, warning := range response.Warnings {
		if strings.Contains(warning, wantRetry) {
			foundWarning = true
		}
	}
	if !foundCoverage || !foundWarning {
		t.Fatalf("quota retry timestamp missing from coverage or warnings: coverage=%+v warnings=%+v", response.Coverage.Checks, response.Warnings)
	}
	engine.cacheMu.RLock()
	for _, entry := range engine.cache {
		if !entry.expiresAt.Equal(retryAt) {
			engine.cacheMu.RUnlock()
			t.Fatalf("cache expiry = %s, want %s", entry.expiresAt, retryAt)
		}
	}
	engine.cacheMu.RUnlock()

	now = retryAt.Add(-time.Second)
	cached, err := engine.Explain(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !cached.Coverage.CacheHit || source.factCalls != 1 || source.recsCalls != 1 {
		t.Fatalf("before retry: cache_hit=%v fact_calls=%d recommender_calls=%d", cached.Coverage.CacheHit, source.factCalls, source.recsCalls)
	}

	now = retryAt
	if _, err := engine.Explain(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if source.factCalls != 2 || source.recsCalls != 2 {
		t.Fatalf("at retry: fact_calls=%d recommender_calls=%d, want 2/2", source.factCalls, source.recsCalls)
	}
}

func TestExplainCacheAvoidsRepeatedBillingQuery(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{facts: models.BillingCostFacts{Facts: []models.CostFact{{Dimension: "total", Key: "total"}}}}
	engine := New(source, nil, Config{}, WithClock(func() time.Time { return now }))
	request := models.ExplainCostRequest{ProjectID: "prod-project"}
	if _, err := engine.Explain(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	response, err := engine.Explain(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if source.factCalls != 1 || !response.Coverage.CacheHit {
		t.Fatalf("factCalls=%d cacheHit=%v", source.factCalls, response.Coverage.CacheHit)
	}
}

func TestExplainCacheIsIsolatedByProject(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{facts: models.BillingCostFacts{Facts: []models.CostFact{{Dimension: "total", Key: "total"}}}}
	engine := New(source, nil, Config{}, WithClock(func() time.Time { return now }))
	for _, projectID := range []string{"dev-project-123", "preprod-project-123"} {
		if _, err := engine.Explain(context.Background(), models.ExplainCostRequest{ProjectID: projectID}); err != nil {
			t.Fatal(err)
		}
	}
	if source.factCalls != 2 {
		t.Fatalf("billing fact calls = %d, want 2 distinct project cache entries", source.factCalls)
	}
}

func TestResponseCacheIsBoundedAndPurgesExpiredEntries(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	engine := New(nil, nil, Config{})
	for i := 0; i <= maxCacheEntries; i++ {
		engine.setCached(fmt.Sprintf("key-%03d", i), models.ExplainCostResponse{}, now.Add(time.Duration(i)*time.Nanosecond))
	}
	engine.cacheMu.RLock()
	size := len(engine.cache)
	engine.cacheMu.RUnlock()
	if size != maxCacheEntries {
		t.Fatalf("cache size = %d, want %d", size, maxCacheEntries)
	}

	engine.cacheMu.Lock()
	engine.cache["expired"] = cacheEntry{expiresAt: now.Add(-time.Second)}
	engine.cacheMu.Unlock()
	if _, ok := engine.getCached("expired", now); ok {
		t.Fatal("expired cache entry was returned")
	}
}

func TestExplainReportsNoDataAndDisabledRecommenderAsSkipped(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{
		facts:  models.BillingCostFacts{Facts: []models.CostFact{}, Daily: []models.HistoricalCost{}, FirstSeen: []models.ResourceFirstSeen{}},
		recs:   models.ListCostRecommendationsResponse{Available: false, Warnings: []string{"disabled"}},
		assets: models.ListCreatedAssetsResponse{Assets: []models.CreatedAsset{}},
	}
	response, err := New(source, nil, Config{}, WithClock(func() time.Time { return now })).Explain(context.Background(), models.ExplainCostRequest{ProjectID: "prod-project", IncludeTraffic: boolPointer(false)})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "no_data" {
		t.Fatalf("status = %q, want no_data", response.Status)
	}
	foundSkipped := false
	for _, check := range response.Coverage.Checks {
		if check.Name == "recommender" && check.Status == "skipped" {
			foundSkipped = true
		}
	}
	if !foundSkipped {
		t.Fatalf("coverage = %+v", response.Coverage.Checks)
	}
}

func TestExplainMarksTruncatedAssetEvidencePartial(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{
		facts: models.BillingCostFacts{DataThrough: "2026-08-06T00:00:00Z", Facts: []models.CostFact{
			{Dimension: "total", Key: "total", Current: models.CostMeasure{NetCost: 20}, Baseline: models.CostMeasure{NetCost: 10}},
		}, FirstSeen: []models.ResourceFirstSeen{{Resource: "new-resource", FirstSeen: "2026-08-02T00:00:00Z", CurrentCost: 1}}},
		assets: models.ListCreatedAssetsResponse{Truncated: true},
		recs:   models.ListCostRecommendationsResponse{Available: true, Complete: true},
	}
	response, err := New(source, nil, Config{}, WithClock(func() time.Time { return now })).Explain(context.Background(), models.ExplainCostRequest{ProjectID: "prod-project", IncludeTraffic: boolPointer(false)})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "partial" {
		t.Fatalf("status = %q, want partial", response.Status)
	}
}

func TestExplainMarksIncompleteRecommenderEvidencePartial(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{
		facts: models.BillingCostFacts{Facts: []models.CostFact{
			{Dimension: "total", Key: "total", Current: models.CostMeasure{NetCost: 20}, Baseline: models.CostMeasure{NetCost: 10}},
		}},
		assets: models.ListCreatedAssetsResponse{},
		recs: models.ListCostRecommendationsResponse{
			Available: true, Complete: false,
			Recommendations: []models.CostRecommendation{{Resource: "//compute.googleapis.com/projects/prod-project/zones/us-central1-a/instances/idle-vm", Subtype: "idle"}},
			Warnings:        []string{"one recommender type was unavailable"},
		},
	}
	response, err := New(source, nil, Config{}, WithClock(func() time.Time { return now })).Explain(context.Background(), models.ExplainCostRequest{ProjectID: "prod-project", IncludeTraffic: boolPointer(false)})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "partial" || len(response.IdleResources) != 1 {
		t.Fatalf("status/idle = %q / %+v", response.Status, response.IdleResources)
	}
	foundPartial := false
	for _, check := range response.Coverage.Checks {
		if check.Name == "recommender" && check.Status == "partial" {
			foundPartial = true
		}
	}
	if !foundPartial {
		t.Fatalf("coverage = %+v", response.Coverage.Checks)
	}
}

func TestExplainMarksMixedCurrencyTotalsPartial(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{
		facts: models.BillingCostFacts{Currency: "USD", CurrencyCount: 2, Facts: []models.CostFact{
			{Dimension: "total", Key: "total", Current: models.CostMeasure{NetCost: 20}, Baseline: models.CostMeasure{NetCost: 10}},
		}},
		assets: models.ListCreatedAssetsResponse{},
		recs:   models.ListCostRecommendationsResponse{Available: false},
	}
	response, err := New(source, nil, Config{}, WithClock(func() time.Time { return now })).Explain(context.Background(), models.ExplainCostRequest{ProjectID: "prod-project", IncludeTraffic: boolPointer(false)})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "partial" || response.Totals.Currency != "MULTIPLE" {
		t.Fatalf("status/totals = %q / %+v", response.Status, response.Totals)
	}
}

func boolPointer(value bool) *bool { return &value }

func TestResolveWindowsUsesCompleteCalendarDaysAcrossDST(t *testing.T) {
	t.Parallel()
	request := models.ExplainCostRequest{ProjectID: "project", Period: "last_7_complete_days", Timezone: "America/Los_Angeles", Comparison: "previous_period"}
	windows, err := resolveWindows(request, time.Date(2026, 3, 12, 12, 0, 0, 0, time.UTC), 90)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := time.LoadLocation(request.Timezone)
	if got := windows.currentEnd.In(location).Format("2006-01-02 15:04"); got != "2026-03-12 00:00" {
		t.Fatalf("current end = %s", got)
	}
	if got := windows.currentStart.In(location).Format("2006-01-02 15:04"); got != "2026-03-05 00:00" {
		t.Fatalf("current start = %s", got)
	}
	if days := calendarDays(windows.currentStart.In(location), windows.currentEnd.In(location)); days != 7 {
		t.Fatalf("calendar days = %d", days)
	}
}

func TestResolveWindowsRejectsUnboundedCustomPeriod(t *testing.T) {
	t.Parallel()
	request := models.ExplainCostRequest{
		ProjectID: "project", Period: "custom", StartDate: "2024-01-01", EndDate: "2025-12-31",
		Timezone: "UTC", Comparison: "previous_period",
	}
	if _, err := resolveWindows(request, time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC), 90); err == nil {
		t.Fatal("resolveWindows() accepted a custom period longer than 366 days")
	}
}

func TestUsageAndRateEffectsReconcileFactDelta(t *testing.T) {
	t.Parallel()
	driver := models.CostDriver{Delta: 38}
	fact := models.CostFact{SKU: "CPU", UsageUnit: "hours", Current: models.CostMeasure{NetCost: 60, Usage: 20}, Baseline: models.CostMeasure{NetCost: 22, Usage: 11}}
	classifyUsageDriver(&driver, fact, models.CostTrafficAnomaly{}, "USD")
	if diff := mathAbs((driver.UsageEffect + driver.RateEffect) - driver.Delta); diff > 1e-9 {
		t.Fatalf("effects do not reconcile: usage=%v rate=%v delta=%v", driver.UsageEffect, driver.RateEffect, driver.Delta)
	}
}

func TestDriverRollupPreservesProjectDelta(t *testing.T) {
	t.Parallel()
	facts := models.BillingCostFacts{Currency: "USD", Facts: []models.CostFact{
		{Dimension: "total", Key: "total", Current: models.CostMeasure{NetCost: 60}},
		{Dimension: "resource", Key: "first", Resource: "first", Current: models.CostMeasure{NetCost: 30}},
		{Dimension: "resource", Key: "second", Resource: "second", Current: models.CostMeasure{NetCost: 20}},
		{Dimension: "resource", Key: "third", Resource: "third", Current: models.CostMeasure{NetCost: 10}},
	}}
	drivers := buildDrivers(facts, nil, nil, 2)
	if len(drivers) != 2 || drivers[1].Key != "other measured changes" {
		t.Fatalf("drivers = %+v", drivers)
	}
	got := 0.0
	for _, driver := range drivers {
		got += driver.Delta
	}
	if mathAbs(got-60) > 1e-9 {
		t.Fatalf("driver delta = %v, want 60", got)
	}
}

func TestCompleteDailyHistoryFillsObservedCoverageGaps(t *testing.T) {
	t.Parallel()
	location, _ := time.LoadLocation("America/Los_Angeles")
	windows := analysisWindows{
		historyStart: time.Date(2026, 3, 6, 0, 0, 0, 0, location).UTC(),
		currentEnd:   time.Date(2026, 3, 10, 0, 0, 0, 0, location).UTC(),
	}
	daily := completeDailyHistory(
		[]models.HistoricalCost{{Date: "2026-03-07", NetCost: 2}, {Date: "2026-03-09", NetCost: 4}},
		"2026-03-07T08:00:00Z", windows, "America/Los_Angeles",
	)
	if len(daily) != 3 || daily[0].Date != "2026-03-07" || daily[1].Date != "2026-03-08" || daily[1].NetCost != 0 || daily[2].NetCost != 4 {
		t.Fatalf("daily history = %+v", daily)
	}
}

func TestSummaryDetailCapsContributorsPerDimension(t *testing.T) {
	t.Parallel()
	response := models.ExplainCostResponse{}
	for _, dimension := range []string{"service", "sku", "resource"} {
		for rank := 1; rank <= 4; rank++ {
			response.TopSpenders = append(response.TopSpenders, models.CostContributor{Dimension: dimension, Rank: rank})
			response.TopIncreases = append(response.TopIncreases, models.CostContributor{Dimension: dimension, Rank: rank})
		}
	}
	applyDetailLevel(&response, "summary")
	if len(response.TopSpenders) != 9 || len(response.TopIncreases) != 9 {
		t.Fatalf("contributor counts = %d / %d, want 9 / 9", len(response.TopSpenders), len(response.TopIncreases))
	}
}

func TestBuildSummaryDoesNotReportZeroPercentFromZeroBaseline(t *testing.T) {
	t.Parallel()
	response := models.ExplainCostResponse{
		Status: "complete",
		Totals: models.CostComparison{Currency: "USD", CurrentNetCost: 10, Delta: 10, PercentChangeDefined: false},
	}
	summary := buildSummary(&response)
	if !strings.Contains(summary, "percentage change is undefined") || strings.Contains(summary, "0.0%") {
		t.Fatalf("summary = %q", summary)
	}
}

func mathAbs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
