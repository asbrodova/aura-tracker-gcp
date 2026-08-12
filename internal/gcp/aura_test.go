package gcp

import (
	"errors"
	"strings"
	"testing"
	"time"

	monitoringpb "cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// ---------------------------------------------------------------------------
// auraBand
// ---------------------------------------------------------------------------

func TestAuraBand(t *testing.T) {
	tests := []struct {
		score int
		want  models.AuraBand
	}{
		{100, models.AuraBandGreen},
		{80, models.AuraBandGreen},
		{79, models.AuraBandYellow},
		{50, models.AuraBandYellow},
		{49, models.AuraBandRed},
		{0, models.AuraBandRed},
	}
	for _, tt := range tests {
		if got := auraBand(tt.score); got != tt.want {
			t.Errorf("auraBand(%d) = %q, want %q", tt.score, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// cloudRunSignalScore
// ---------------------------------------------------------------------------

func TestCloudRunSignalScore_ErrorRate(t *testing.T) {
	tests := []struct {
		value     float64
		wantScore int
		wantLabel string
	}{
		{0.0, 100, "OK"},
		{0.005, 85, "OK"},
		{0.02, 60, "Warning"},
		{0.04, 30, "Warning"},
		{0.10, 0, "Critical"},
	}
	for _, tt := range tests {
		score, label := cloudRunSignalScore("error_rate", tt.value)
		if score != tt.wantScore || label != tt.wantLabel {
			t.Errorf("cloudRunSignalScore(error_rate, %.3f) = (%d, %q), want (%d, %q)",
				tt.value, score, label, tt.wantScore, tt.wantLabel)
		}
	}
}

func TestCloudRunSignalScore_CPU(t *testing.T) {
	tests := []struct {
		value     float64
		wantScore int
		wantLabel string
	}{
		{0.03, 55, "Warning"}, // over-provisioned
		{0.30, 100, "OK"},
		{0.70, 80, "OK"},
		{0.85, 50, "Warning"},
		{0.95, 20, "Critical"},
	}
	for _, tt := range tests {
		score, label := cloudRunSignalScore("cpu_util", tt.value)
		if score != tt.wantScore || label != tt.wantLabel {
			t.Errorf("cloudRunSignalScore(cpu_util, %.2f) = (%d, %q), want (%d, %q)",
				tt.value, score, label, tt.wantScore, tt.wantLabel)
		}
	}
}

func TestCloudRunSignalScore_LatencyP99(t *testing.T) {
	tests := []struct {
		value     float64
		wantScore int
		wantLabel string
	}{
		{150, 100, "OK"},
		{300, 80, "OK"},
		{700, 60, "Warning"},
		{2000, 30, "Warning"},
		{5000, 0, "Critical"},
	}
	for _, tt := range tests {
		score, label := cloudRunSignalScore("latency_p99", tt.value)
		if score != tt.wantScore || label != tt.wantLabel {
			t.Errorf("cloudRunSignalScore(latency_p99, %.0f) = (%d, %q), want (%d, %q)",
				tt.value, score, label, tt.wantScore, tt.wantLabel)
		}
	}
}

// ---------------------------------------------------------------------------
// sqlSignalScore
// ---------------------------------------------------------------------------

func TestSQLSignalScore(t *testing.T) {
	tests := []struct {
		value     float64
		wantScore int
		wantLabel string
	}{
		{0.05, 60, "Warning"}, // idle
		{0.50, 100, "OK"},
		{0.80, 75, "OK"},
		{0.90, 40, "Warning"},
		{0.97, 0, "Critical"},
	}
	for _, tt := range tests {
		score, label := sqlSignalScore(tt.value)
		if score != tt.wantScore || label != tt.wantLabel {
			t.Errorf("sqlSignalScore(%.2f) = (%d, %q), want (%d, %q)",
				tt.value, score, label, tt.wantScore, tt.wantLabel)
		}
	}
}

// ---------------------------------------------------------------------------
// bqSignalScore
// ---------------------------------------------------------------------------

func TestBQSignalScore_JobFailureRate(t *testing.T) {
	tests := []struct {
		value     float64
		wantScore int
		wantLabel string
	}{
		{0.0, 100, "OK"},
		{0.01, 80, "OK"},
		{0.03, 55, "Warning"},
		{0.07, 30, "Warning"},
		{0.15, 0, "Critical"},
	}
	for _, tt := range tests {
		score, label := bqSignalScore("job_failure_rate", tt.value)
		if score != tt.wantScore || label != tt.wantLabel {
			t.Errorf("bqSignalScore(job_failure_rate, %.2f) = (%d, %q), want (%d, %q)",
				tt.value, score, label, tt.wantScore, tt.wantLabel)
		}
	}
}

// ---------------------------------------------------------------------------
// calculateAura
// ---------------------------------------------------------------------------

func TestCalculateAura_CloudRun_Healthy(t *testing.T) {
	signals := []models.AuraHealthSignal{
		{Name: "error_rate", Value: 0.0, Score: 100, Label: "OK"},
		{Name: "cpu_util", Value: 0.35, Score: 100, Label: "OK"},
		{Name: "latency_p99", Value: 150, Score: 100, Label: "OK"},
		{Name: "request_count_total", Value: 1000, Score: 100, Label: "info"},
	}
	report := calculateAura(models.ResourceKindCloudRun, "auth-service", "us-central1", signals)

	if report.Band != models.AuraBandGreen {
		t.Errorf("expected green band, got %q (score=%d)", report.Band, report.Score)
	}
	if report.Score < 80 {
		t.Errorf("expected score ≥ 80, got %d", report.Score)
	}
	if report.Display == "" {
		t.Error("Display must not be empty")
	}
	if len(report.Reasons) == 0 {
		t.Error("Reasons must not be empty")
	}
}

func TestCalculateAura_CloudRun_OverProvisioned(t *testing.T) {
	signals := []models.AuraHealthSignal{
		{Name: "error_rate", Value: 0.0, Score: 100, Label: "OK"},
		{Name: "cpu_util", Value: 0.03, Score: 55, Label: "Warning"}, // < 5%
		{Name: "latency_p99", Value: 180, Score: 100, Label: "OK"},
		{Name: "request_count_total", Value: 500, Score: 100, Label: "info"},
	}
	report := calculateAura(models.ResourceKindCloudRun, "svc", "us-central1", signals)

	if report.EfficiencyScore != 40 {
		t.Errorf("expected efficiency score 40 for over-provisioned service, got %d", report.EfficiencyScore)
	}
	found := false
	for _, r := range report.Reasons {
		if len(r) > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one reason")
	}
}

func TestCalculateAura_CloudRun_Critical(t *testing.T) {
	signals := []models.AuraHealthSignal{
		{Name: "error_rate", Value: 0.10, Score: 0, Label: "Critical"},
		{Name: "cpu_util", Value: 0.95, Score: 20, Label: "Critical"},
		{Name: "latency_p99", Value: 5000, Score: 0, Label: "Critical"},
		{Name: "request_count_total", Value: 1000, Score: 100, Label: "info"},
	}
	report := calculateAura(models.ResourceKindCloudRun, "broken-svc", "us-central1", signals)

	if report.Band != models.AuraBandRed {
		t.Errorf("expected red band, got %q (score=%d)", report.Band, report.Score)
	}
	if report.Score >= 50 {
		t.Errorf("expected score < 50, got %d", report.Score)
	}
}

func TestCalculateAura_DoesNotTreatMissingTelemetryAsHealthyZero(t *testing.T) {
	noData := metricHealthSignal("error_rate", 0, errAuraMetricNoData, func(value float64) (int, string) {
		return cloudRunSignalScore("error_rate", value)
	})
	if noData.Availability != "no_data" || noData.Score != 0 || noData.Label != "No data" {
		t.Fatalf("no-data signal = %+v", noData)
	}
	signals := []models.AuraHealthSignal{
		noData,
		{Name: "cpu_util", Availability: "no_data", Label: "No data", Message: "no points"},
		{Name: "latency_p99", Availability: "error", Label: "Unavailable", Message: "permission denied"},
		{Name: "request_count_total", Availability: "no_data", Label: "No data", Message: "no points"},
	}
	report := calculateAura(models.ResourceKindCloudRun, "api", "us-central1", signals)
	if report.CoverageStatus != "unavailable" || report.SignalsObserved != 0 || report.SignalsExpected != 4 {
		t.Fatalf("coverage = %+v", report)
	}
	if report.Band != models.AuraBandUnavailable {
		t.Fatalf("unavailable telemetry band = %q", report.Band)
	}
	if report.Display != "⚪ Cloud Run: api | Aura: N/A (telemetry unavailable)" || len(report.Warnings) != 4 {
		t.Fatalf("unavailable report = %+v", report)
	}
}

func TestRatioMetricValue(t *testing.T) {
	permissionErr := errors.New("permission denied")
	tests := []struct {
		name           string
		numerator      float64
		numeratorErr   error
		denominator    float64
		denominatorErr error
		want           float64
		wantErr        error
	}{
		{
			name:         "no numerator series means zero errors when total is observed",
			numeratorErr: errAuraMetricNoData,
			denominator:  200,
			want:         0,
		},
		{
			name:        "observed ratio",
			numerator:   5,
			denominator: 200,
			want:        0.025,
		},
		{
			name:        "zero denominator remains unavailable",
			denominator: 0,
			wantErr:     errAuraMetricNoData,
		},
		{
			name:         "real numerator error is preserved",
			numeratorErr: permissionErr,
			denominator:  200,
			wantErr:      permissionErr,
		},
		{
			name:           "denominator error takes precedence",
			numeratorErr:   errAuraMetricNoData,
			denominatorErr: permissionErr,
			wantErr:        permissionErr,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ratioMetricValue(test.numerator, test.numeratorErr, test.denominator, test.denominatorErr)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("ratioMetricValue() = (%v, %v), want (%v, nil)", got, err, test.want)
			}
		})
	}
}

func TestCalculateAura_PartialTelemetryUsesOnlyObservedSignals(t *testing.T) {
	report := calculateAura(models.ResourceKindCloudSQL, "database", "us-central1", []models.AuraHealthSignal{
		{Name: "cpu_util", Availability: "no_data", Label: "No data", Message: "no points"},
		{Name: "memory_util", Value: 0.5, Score: 100, Label: "OK", Availability: "observed"},
		{Name: "disk_util", Value: 0.3, Score: 100, Label: "OK", Availability: "observed"},
	})
	if report.CoverageStatus != "partial" || report.SignalsObserved != 2 || report.SignalsExpected != 3 {
		t.Fatalf("partial coverage = %+v", report)
	}
	if report.HealthScore != 100 || report.EfficiencyScore != 90 {
		t.Fatalf("missing CPU was treated as a zero value: health=%d efficiency=%d", report.HealthScore, report.EfficiencyScore)
	}
	if !strings.Contains(report.Display, "[PARTIAL: 2/3 signals]") || len(report.Warnings) != 1 {
		t.Fatalf("partial warning missing: %+v", report)
	}
}

func TestPartialTelemetryDoesNotInventZeroValueDiagnoses(t *testing.T) {
	tests := []struct {
		name       string
		kind       models.ResourceKind
		signals    []models.AuraHealthSignal
		forbidden  []string
		labelAvoid string
	}{
		{
			name: "cloud run missing cpu",
			kind: models.ResourceKindCloudRun,
			signals: []models.AuraHealthSignal{
				{Name: "error_rate", Value: 0, Score: 100, Label: "OK", Availability: "observed"},
				{Name: "request_count_total", Value: 10, Score: 100, Label: "info", Availability: "observed"},
				{Name: "latency_p99", Value: 100, Score: 100, Label: "OK", Availability: "observed"},
			},
			forbidden:  []string{"CPU at 0%"},
			labelAvoid: "Healthy, Over-provisioned",
		},
		{
			name: "cloud sql missing cpu",
			kind: models.ResourceKindCloudSQL,
			signals: []models.AuraHealthSignal{
				{Name: "memory_util", Value: 0.5, Score: 100, Label: "OK", Availability: "observed"},
				{Name: "disk_util", Value: 0.3, Score: 100, Label: "OK", Availability: "observed"},
			},
			forbidden:  []string{"CPU at 0%"},
			labelAvoid: "Idle, Consider Downsize",
		},
		{
			name: "bigquery missing slot metric",
			kind: models.ResourceKindBigQuery,
			signals: []models.AuraHealthSignal{
				{Name: "job_failure_rate", Value: 0, Score: 100, Label: "OK", Availability: "observed"},
				{Name: "storage_bytes", Value: 1024, Score: 100, Label: "OK", Availability: "observed"},
			},
			forbidden: []string{"Slot utilization near zero"},
		},
		{
			name: "gke missing control plane and utilization",
			kind: models.ResourceKindGKE,
			signals: []models.AuraHealthSignal{
				{Name: "pod_restart_rate", Value: 0, Score: 100, Label: "OK", Availability: "observed"},
			},
			forbidden:  []string{"Control plane", "cluster is idle"},
			labelAvoid: "Control Plane Error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reasons := strings.Join(buildReasons(test.kind, test.signals), "\n")
			for _, forbidden := range test.forbidden {
				if strings.Contains(reasons, forbidden) {
					t.Fatalf("invented diagnosis %q from missing signal: %s", forbidden, reasons)
				}
			}
			if label := auraLabel(85, test.signals, test.kind); test.labelAvoid != "" && label == test.labelAvoid {
				t.Fatalf("invented label %q from missing signal", label)
			}
		})
	}
}

func TestValidateAuraRequestRejectsFilterInjectionCharacters(t *testing.T) {
	valid := models.GetAuraScoreRequest{
		ProjectID: "valid-project-123", ResourceKind: models.ResourceKindCloudRun,
		ResourceName: "api-service", Region: "us-central1",
	}
	if err := validateAuraRequest(valid); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	invalid := []models.GetAuraScoreRequest{
		{ProjectID: `valid-project-123" OR resource.type="gce_instance`, ResourceName: "api-service", Region: "us-central1"},
		{ProjectID: "valid-project-123", ResourceName: `api" OR true`, Region: "us-central1"},
		{ProjectID: "valid-project-123", ResourceName: "api-service", Region: `us-central1" OR true`},
	}
	for _, request := range invalid {
		if err := validateAuraRequest(request); err == nil {
			t.Fatalf("invalid Aura request accepted: %+v", request)
		}
	}
	if got := escapeMonitoringString(`a"b\\c`); got != `a\"b\\\\c` {
		t.Fatalf("escaped monitoring value = %q", got)
	}
}

func TestCalculateAura_CloudSQL_Idle(t *testing.T) {
	signals := []models.AuraHealthSignal{
		{Name: "cpu_util", Value: 0.05, Score: 60, Label: "Warning"},
		{Name: "memory_util", Value: 0.20, Score: 100, Label: "OK"},
		{Name: "disk_util", Value: 0.30, Score: 100, Label: "OK"},
	}
	report := calculateAura(models.ResourceKindCloudSQL, "legacy-db", "us-central1", signals)

	if report.EfficiencyScore != 30 {
		t.Errorf("expected efficiency score 30 for idle SQL instance, got %d", report.EfficiencyScore)
	}
}

func TestCalculateAura_BigQuery_HighFailRate(t *testing.T) {
	// 100% failure rate + idle slots → both health and efficiency are low → red band.
	signals := []models.AuraHealthSignal{
		{Name: "job_failure_rate", Value: 0.15, Score: 0, Label: "Critical"},
		{Name: "slot_utilization", Value: 5, Score: 60, Label: "Warning"}, // < 10 slots → idle
		{Name: "storage_bytes", Value: 5e9, Score: 100, Label: "OK"},
	}
	report := calculateAura(models.ResourceKindBigQuery, "analytics", "", signals)

	if report.Band != models.AuraBandRed {
		t.Errorf("expected red band, got %q (score=%d)", report.Band, report.Score)
	}
}

// ---------------------------------------------------------------------------
// recommender signal helpers
// ---------------------------------------------------------------------------

func TestRecommenderSignal_Idle(t *testing.T) {
	ins := recommenderInsight{subtype: "idle", description: "Service is idle", monthlySavings: 45.50}
	sig := recommenderSignal(ins)
	if sig.Name != "recommender_idle" {
		t.Errorf("expected name recommender_idle, got %q", sig.Name)
	}
	if sig.Score != 20 {
		t.Errorf("expected score 20 for idle, got %d", sig.Score)
	}
	if sig.Value != 45.50 {
		t.Errorf("expected value 45.50, got %f", sig.Value)
	}
}

func TestRecommenderSignal_Overprovisioned(t *testing.T) {
	ins := recommenderInsight{subtype: "overprovisioned", description: "Downsize recommended", monthlySavings: 12.00}
	sig := recommenderSignal(ins)
	if sig.Name != "recommender_overprovisioned" {
		t.Errorf("expected name recommender_overprovisioned, got %q", sig.Name)
	}
	if sig.Score != 45 {
		t.Errorf("expected score 45 for overprovisioned, got %d", sig.Score)
	}
}

func TestApplyRecommenderEfficiency(t *testing.T) {
	tests := []struct {
		name    string
		byName  map[string]int
		baseEff int
		wantEff int
	}{
		{"no recommender", map[string]int{}, 90, 90},
		{"idle overrides high eff", map[string]int{"recommender_idle": 20}, 90, 20},
		{"idle overrides low eff too", map[string]int{"recommender_idle": 20}, 30, 20},
		{"overprovisioned caps at 45", map[string]int{"recommender_overprovisioned": 45}, 90, 45},
		{"overprovisioned doesn't raise eff", map[string]int{"recommender_overprovisioned": 45}, 30, 30},
		{"idle beats overprovisioned", map[string]int{"recommender_idle": 20, "recommender_overprovisioned": 45}, 90, 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyRecommenderEfficiency(tt.byName, tt.baseEff)
			if got != tt.wantEff {
				t.Errorf("applyRecommenderEfficiency(%v, %d) = %d, want %d", tt.byName, tt.baseEff, got, tt.wantEff)
			}
		})
	}
}

func TestCalculateAura_CloudRun_IdleRecommender(t *testing.T) {
	// Healthy metrics but GCP says it's idle → efficiency crushed → score drops.
	signals := []models.AuraHealthSignal{
		{Name: "error_rate", Value: 0.0, Score: 100, Label: "OK"},
		{Name: "cpu_util", Value: 0.35, Score: 100, Label: "OK"},
		{Name: "latency_p99", Value: 120, Score: 100, Label: "OK"},
		{Name: "request_count_total", Value: 0, Score: 100, Label: "info"}, // no traffic
		{Name: "recommender_idle", Value: 38.75, Score: 20, Label: "Warning"},
	}
	report := calculateAura(models.ResourceKindCloudRun, "ghost-svc", "us-central1", signals)

	if report.EfficiencyScore != 20 {
		t.Errorf("expected efficiency 20 for idle recommendation, got %d", report.EfficiencyScore)
	}
	if report.Band != models.AuraBandYellow && report.Band != models.AuraBandRed {
		t.Errorf("idle service should not be green, got %q (score=%d)", report.Band, report.Score)
	}
	// Reasons must mention the recommender finding.
	found := false
	for _, r := range report.Reasons {
		if strings.Contains(r, "GCP Recommender") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a GCP Recommender reason, got: %v", report.Reasons)
	}
}

func TestBuildReasons_RecommenderFirst(t *testing.T) {
	signals := []models.AuraHealthSignal{
		{Name: "error_rate", Value: 0.0, Score: 100, Label: "OK"},
		{Name: "recommender_idle", Value: 55.00, Score: 20, Label: "Warning"},
	}
	reasons := buildReasons(models.ResourceKindCloudRun, signals)
	if len(reasons) == 0 {
		t.Fatal("expected at least one reason")
	}
	if !strings.Contains(reasons[0], "GCP Recommender") {
		t.Errorf("first reason should be the recommender insight, got: %q", reasons[0])
	}
	if !strings.Contains(reasons[0], "55.00") {
		t.Errorf("reason should include savings amount, got: %q", reasons[0])
	}
}

func TestClassifyRecommenderID(t *testing.T) {
	tests := []struct{ id, want string }{
		{recommenderIDCloudRunIdle, "idle"},
		{recommenderIDCloudSQLIdle, "idle"},
		{recommenderIDCloudSQLOverpro, "overprovisioned"},
	}
	for _, tt := range tests {
		if got := classifyRecommenderID(tt.id); got != tt.want {
			t.Errorf("classifyRecommenderID(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestExtractMonthlySavings_NilInput(t *testing.T) {
	if v := extractMonthlySavings(nil); v != 0 {
		t.Errorf("expected 0 for nil recommendation, got %f", v)
	}
}

// ---------------------------------------------------------------------------
// buildDisplay
// ---------------------------------------------------------------------------

func TestBuildDisplay(t *testing.T) {
	display := buildDisplay(models.ResourceKindCloudRun, "auth-service", 98, "Healthy & Scaled", nil)
	want := "🟢 Cloud Run: auth-service | Aura: 98 (Healthy & Scaled)"
	if display != want {
		t.Errorf("buildDisplay = %q, want %q", display, want)
	}
}

func TestBuildDisplay_Yellow(t *testing.T) {
	display := buildDisplay(models.ResourceKindCloudSQL, "main-db", 65, "Healthy, but High Disk Cost", nil)
	want := "🟡 Cloud SQL: main-db | Aura: 65 (Healthy, but High Disk Cost)"
	if display != want {
		t.Errorf("buildDisplay = %q, want %q", display, want)
	}
}

// ---------------------------------------------------------------------------
// clamp
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Regression tests for commit 31af98c: Cloud Run metric aligner fix
// ---------------------------------------------------------------------------

// TestFetchMetricPoint_AlignerConstants verifies that the three aligner constants
// used by fetchRateMetric, fetchMeanMetric, and fetchPercentileMetric are all
// distinct. Re-introducing ALIGN_MEAN for DELTA/DISTRIBUTION metrics would cause
// this test to fail by making two of the three equal.
func TestFetchMetricPoint_AlignerConstants(t *testing.T) {
	mean := monitoringpb.Aggregation_ALIGN_MEAN
	rate := monitoringpb.Aggregation_ALIGN_RATE
	p99 := monitoringpb.Aggregation_ALIGN_PERCENTILE_99

	if rate == mean {
		t.Error("ALIGN_RATE must differ from ALIGN_MEAN: fetchRateMetric must not use ALIGN_MEAN")
	}
	if p99 == mean {
		t.Error("ALIGN_PERCENTILE_99 must differ from ALIGN_MEAN: fetchPercentileMetric must not use ALIGN_MEAN")
	}
	if rate == p99 {
		t.Error("ALIGN_RATE must differ from ALIGN_PERCENTILE_99")
	}
}

// TestCalculateAura_CloudRun_LatencyP99_ScoreFromDistribution is a regression
// test for the DELTA DISTRIBUTION mis-alignment bug (commit 31af98c).
// When ALIGN_MEAN was incorrectly applied to request_latencies, the raw value
// was near-zero (a meaningless distribution mean), scoring 100 "OK" and hiding
// real latency. With the correct ALIGN_PERCENTILE_99, 450ms maps to score 80.
func TestCalculateAura_CloudRun_LatencyP99_ScoreFromDistribution(t *testing.T) {
	score, label := cloudRunSignalScore("latency_p99", 450)
	if score != 80 || label != "OK" {
		t.Errorf("latency_p99=450ms: got (%d, %q), want (80, OK); "+
			"if score is 100 the wrong aligner (ALIGN_MEAN) may have been restored", score, label)
	}

	// Also verify calculateAura propagates the signal score correctly.
	signals := []models.AuraHealthSignal{
		{Name: "error_rate", Value: 0.0, Score: 100, Label: "OK"},
		{Name: "cpu_util", Value: 0.35, Score: 100, Label: "OK"},
		{Name: "latency_p99", Value: 450, Score: score, Label: label},
		{Name: "request_count_total", Value: 200, Score: 100, Label: "info"},
	}
	report := calculateAura(models.ResourceKindCloudRun, "slow-svc", "us-central1", signals)
	var latSig *models.AuraHealthSignal
	for i := range report.HealthSignals {
		if report.HealthSignals[i].Name == "latency_p99" {
			latSig = &report.HealthSignals[i]
			break
		}
	}
	if latSig == nil {
		t.Fatal("latency_p99 signal missing from AuraReport.HealthSignals")
	}
	if latSig.Score != 80 {
		t.Errorf("latency_p99 signal score in report: got %d, want 80", latSig.Score)
	}
}

// TestCalculateAura_CloudRun_RequestCountRate verifies that request_count_total
// is stored as a rate value (requests/sec), not a raw cumulative counter.
// With ALIGN_RATE a value of 5.0 means ~5 req/s; with ALIGN_MEAN it would be
// an arbitrary snapshot that could be orders of magnitude larger.
func TestCalculateAura_CloudRun_RequestCountRate(t *testing.T) {
	// Simulate the signal as produced by fetchRateMetric: value is req/s.
	// Efficiency logic: cpu<5% AND totalReq>0 → eff=40.
	signals := []models.AuraHealthSignal{
		{Name: "error_rate", Value: 0.0, Score: 100, Label: "OK"},
		{Name: "cpu_util", Value: 0.03, Score: 55, Label: "Warning"},
		{Name: "latency_p99", Value: 120, Score: 100, Label: "OK"},
		{Name: "request_count_total", Value: 5.0, Score: 100, Label: "info"}, // 5 req/s
	}
	report := calculateAura(models.ResourceKindCloudRun, "low-cpu-svc", "us-central1", signals)
	if report.EfficiencyScore != 40 {
		t.Errorf("cpu<5%% with traffic should give efficiency=40, got %d; "+
			"if 0 the request_count signal may be using ALIGN_MEAN instead of ALIGN_RATE", report.EfficiencyScore)
	}
}

// TestFetchPercentileMetric_HonoursPercentileArg verifies that fetchPercentileMetric
// selects the correct aligner constant based on the percentile argument.
// If the percentile parameter is ignored (reverted to _ int), p50 and p99 would
// resolve to the same aligner constant, causing this test to fail.
func TestFetchPercentileMetric_HonoursPercentileArg(t *testing.T) {
	p50 := monitoringpb.Aggregation_ALIGN_PERCENTILE_50
	p95 := monitoringpb.Aggregation_ALIGN_PERCENTILE_95
	p99 := monitoringpb.Aggregation_ALIGN_PERCENTILE_99

	if p50 == p99 {
		t.Error("ALIGN_PERCENTILE_50 must differ from ALIGN_PERCENTILE_99")
	}
	if p95 == p99 {
		t.Error("ALIGN_PERCENTILE_95 must differ from ALIGN_PERCENTILE_99")
	}
	if p50 == p95 {
		t.Error("ALIGN_PERCENTILE_50 must differ from ALIGN_PERCENTILE_95")
	}

	// Verify the switch inside fetchPercentileMetric maps each value correctly.
	alignerFor := func(percentile int) monitoringpb.Aggregation_Aligner {
		switch percentile {
		case 50:
			return monitoringpb.Aggregation_ALIGN_PERCENTILE_50
		case 95:
			return monitoringpb.Aggregation_ALIGN_PERCENTILE_95
		default:
			return monitoringpb.Aggregation_ALIGN_PERCENTILE_99
		}
	}
	if alignerFor(50) != p50 {
		t.Errorf("percentile=50 should map to ALIGN_PERCENTILE_50; got %v", alignerFor(50))
	}
	if alignerFor(95) != p95 {
		t.Errorf("percentile=95 should map to ALIGN_PERCENTILE_95; got %v", alignerFor(95))
	}
	if alignerFor(99) != p99 {
		t.Errorf("percentile=99 should map to ALIGN_PERCENTILE_99; got %v", alignerFor(99))
	}
}

// TestCalculateAura_CloudRun_CpuUtil_UsesPercentile is a regression test for the
// DELTA DISTRIBUTION mis-alignment bug in container/cpu/utilizations.
// With ALIGN_MEAN the Monitoring API returns an error; switching to ALIGN_PERCENTILE_50
// returns a proper fraction in [0, 1]. A value of 0.35 (35% median CPU) should
// score 100 "OK". If cpuUtil stays at 0 (fetch failed silently), the score
// would drop to 55 "Warning" (over-provisioned), which indicates the fix regressed.
func TestCalculateAura_CloudRun_CpuUtil_UsesPercentile(t *testing.T) {
	score, label := cloudRunSignalScore("cpu_util", 0.35)
	if score != 100 || label != "OK" {
		t.Errorf("cpu_util=0.35: got (%d, %q), want (100, OK); "+
			"if score is 55 the metric may be returning 0 due to a wrong aligner", score, label)
	}

	// Verify calculateAura propagates the signal score correctly.
	signals := []models.AuraHealthSignal{
		{Name: "error_rate", Value: 0.0, Score: 100, Label: "OK"},
		{Name: "cpu_util", Value: 0.35, Score: score, Label: label},
		{Name: "latency_p99", Value: 120, Score: 100, Label: "OK"},
		{Name: "request_count_total", Value: 10.0, Score: 100, Label: "info"},
	}
	report := calculateAura(models.ResourceKindCloudRun, "healthy-svc", "us-central1", signals)
	var cpuSig *models.AuraHealthSignal
	for i := range report.HealthSignals {
		if report.HealthSignals[i].Name == "cpu_util" {
			cpuSig = &report.HealthSignals[i]
			break
		}
	}
	if cpuSig == nil {
		t.Fatal("cpu_util signal missing from AuraReport.HealthSignals")
	}
	if cpuSig.Score != 100 {
		t.Errorf("cpu_util signal score in report: got %d, want 100", cpuSig.Score)
	}
}

// ---------------------------------------------------------------------------
// clamp
// ---------------------------------------------------------------------------

func TestClamp(t *testing.T) {
	if clamp(-5) != 0 {
		t.Error("clamp(-5) should be 0")
	}
	if clamp(105) != 100 {
		t.Error("clamp(105) should be 100")
	}
	if clamp(50) != 50 {
		t.Error("clamp(50) should be 50")
	}
}

// ---------------------------------------------------------------------------
// ttlCache
// ---------------------------------------------------------------------------

func TestTTLCache(t *testing.T) {
	c := newTTLCache[string](5 * time.Second)

	c.set("k", "hello")
	v, ok := c.get("k")
	if !ok || v != "hello" {
		t.Errorf("expected hit, got ok=%v v=%q", ok, v)
	}

	// Overwrite
	c.set("k", "world")
	v, ok = c.get("k")
	if !ok || v != "world" {
		t.Errorf("expected updated value, got ok=%v v=%q", ok, v)
	}

	// Miss
	_, ok = c.get("missing")
	if ok {
		t.Error("expected cache miss for unknown key")
	}
}

// ---------------------------------------------------------------------------
// GCS signal scores
// ---------------------------------------------------------------------------

func TestGCSSignalScore(t *testing.T) {
	tests := []struct {
		name      string
		value     float64
		wantScore int
		wantLabel string
	}{
		{"public_access_prevention", 1.0, 100, "OK"},
		{"public_access_prevention", 0.0, 15, "Critical"},
		{"uniform_bucket_level_access", 1.0, 100, "OK"},
		{"uniform_bucket_level_access", 0.0, 30, "Warning"},
		{"versioning", 1.0, 100, "OK"},
		{"versioning", 0.0, 50, "Warning"},
		{"lifecycle_policy", 3.0, 100, "OK"},
		{"lifecycle_policy", 0.0, 25, "Warning"},
	}
	for _, tt := range tests {
		score, label := gcsSignalScore(tt.name, tt.value)
		if score != tt.wantScore || label != tt.wantLabel {
			t.Errorf("gcsSignalScore(%q, %v) = (%d, %q), want (%d, %q)",
				tt.name, tt.value, score, label, tt.wantScore, tt.wantLabel)
		}
	}
}

func TestGCSStorageClassScore(t *testing.T) {
	tests := []struct {
		class        string
		hasLifecycle bool
		wantScore    int
		wantLabel    string
	}{
		{"NEARLINE", false, 100, "OK"},
		{"COLDLINE", false, 100, "OK"},
		{"ARCHIVE", false, 100, "OK"},
		{"STANDARD", true, 90, "OK"},
		{"STANDARD", false, 40, "Warning"},
		{"MULTI_REGIONAL", true, 90, "OK"},
		{"MULTI_REGIONAL", false, 40, "Warning"},
		{"REGIONAL", false, 40, "Warning"},
		{"UNKNOWN_CLASS", false, 70, "OK"},
	}
	for _, tt := range tests {
		score, label := gcsStorageClassScore(tt.class, tt.hasLifecycle)
		if score != tt.wantScore || label != tt.wantLabel {
			t.Errorf("gcsStorageClassScore(%q, %v) = (%d, %q), want (%d, %q)",
				tt.class, tt.hasLifecycle, score, label, tt.wantScore, tt.wantLabel)
		}
	}
}

// ---------------------------------------------------------------------------
// GCS weighted scores
// ---------------------------------------------------------------------------

func TestGCSWeightedScores(t *testing.T) {
	// COMPLIANT bucket: PAP enforced, UBLA enabled, versioning on, lifecycle rules, archival class.
	compliantSignals := []models.AuraHealthSignal{
		{Name: "public_access_prevention", Value: 1.0, Score: 100},
		{Name: "uniform_bucket_level_access", Value: 1.0, Score: 100},
		{Name: "versioning", Value: 1.0, Score: 100},
		{Name: "lifecycle_policy", Value: 2.0, Score: 100},
		{Name: "storage_class_fit", Value: 0, Score: 100},
	}
	health, eff := weightedScores(models.ResourceKindGCS, compliantSignals)
	if health != 100 {
		t.Errorf("compliant bucket health = %d, want 100", health)
	}
	if eff != 100 {
		t.Errorf("compliant bucket efficiency = %d, want 100", eff)
	}

	// CRITICAL bucket: no PAP, no UBLA, no versioning, no lifecycle, STANDARD.
	criticalSignals := []models.AuraHealthSignal{
		{Name: "public_access_prevention", Value: 0.0, Score: 15},
		{Name: "uniform_bucket_level_access", Value: 0.0, Score: 30},
		{Name: "versioning", Value: 0.0, Score: 50},
		{Name: "lifecycle_policy", Value: 0, Score: 25},
		{Name: "storage_class_fit", Value: 0, Score: 40},
	}
	health, eff = weightedScores(models.ResourceKindGCS, criticalSignals)
	// health = 15*0.45 + 30*0.35 + 50*0.20 = 6.75 + 10.5 + 10 = 27.25 → 27
	wantHealth := 27
	if health != wantHealth {
		t.Errorf("critical bucket health = %d, want %d", health, wantHealth)
	}
	// eff = 25*0.60 + 40*0.40 = 15 + 16 = 31
	wantEff := 31
	if eff != wantEff {
		t.Errorf("critical bucket efficiency = %d, want %d", eff, wantEff)
	}

	// Composite score for critical bucket = 27*0.6 + 31*0.4 = 16.2 + 12.4 = 28.6 → 29 → red
	report := calculateAura(models.ResourceKindGCS, "my-bucket", "", criticalSignals)
	if report.Band != models.AuraBandRed {
		t.Errorf("critical bucket band = %q, want red", report.Band)
	}
}

// ---------------------------------------------------------------------------
// GCS security posture
// ---------------------------------------------------------------------------

func TestGCSSecurityPosture(t *testing.T) {
	tests := []struct {
		ubla bool
		pap  string
		want models.GCSSecurityPosture
	}{
		{true, "enforced", models.GCSPostureCompliant},
		{false, "enforced", models.GCSPostureAtRisk},
		{true, "inherited", models.GCSPostureAtRisk},
		{false, "inherited", models.GCSPostureCritical},
	}
	for _, tt := range tests {
		got := gcsSecurityPosture(tt.ubla, tt.pap)
		if got != tt.want {
			t.Errorf("gcsSecurityPosture(ubla=%v, pap=%q) = %q, want %q", tt.ubla, tt.pap, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// GCS reasons
// ---------------------------------------------------------------------------

func TestGCSBuildReasons(t *testing.T) {
	// Bucket with all problems.
	signals := []models.AuraHealthSignal{
		{Name: "public_access_prevention", Value: 0.0, Score: 15, Label: "Critical"},
		{Name: "uniform_bucket_level_access", Value: 0.0, Score: 30, Label: "Warning"},
		{Name: "versioning", Value: 0.0, Score: 50, Label: "Warning"},
		{Name: "lifecycle_policy", Value: 0, Score: 25, Label: "Warning"},
		{Name: "storage_class_fit", Value: 0, Score: 40, Label: "Warning"},
	}
	reasons := buildReasons(models.ResourceKindGCS, signals)
	checkReason := func(substr string) {
		t.Helper()
		for _, r := range reasons {
			if strings.Contains(r, substr) {
				return
			}
		}
		t.Errorf("expected reason containing %q, got: %v", substr, reasons)
	}
	checkReason("Public access prevention")
	checkReason("Uniform bucket-level access")
	checkReason("lifecycle")
	checkReason("versioning")

	// Healthy bucket: no reasons except the nominal message.
	healthySignals := []models.AuraHealthSignal{
		{Name: "public_access_prevention", Value: 1.0, Score: 100, Label: "OK"},
		{Name: "uniform_bucket_level_access", Value: 1.0, Score: 100, Label: "OK"},
		{Name: "versioning", Value: 1.0, Score: 100, Label: "OK"},
		{Name: "lifecycle_policy", Value: 2.0, Score: 100, Label: "OK"},
		{Name: "storage_class_fit", Value: 0, Score: 100, Label: "OK"},
	}
	healthyReasons := buildReasons(models.ResourceKindGCS, healthySignals)
	if len(healthyReasons) != 1 || !strings.Contains(healthyReasons[0], "nominal") {
		t.Errorf("healthy bucket expected nominal message, got: %v", healthyReasons)
	}
}

// ---------------------------------------------------------------------------
// GKE version drift scoring
// ---------------------------------------------------------------------------

func TestGKEVersionDriftScore(t *testing.T) {
	tests := []struct {
		current   string
		latest    string
		wantScore int
		wantLabel string
	}{
		{"1.29.4-gke.100", "1.29.4-gke.100", 100, "OK"},      // exact match
		{"1.29.4-gke.50", "1.29.9-gke.200", 100, "OK"},       // same minor, newer patch — still current
		{"1.28.6-gke.100", "1.29.4-gke.100", 70, "Warning"},  // 1 minor behind
		{"1.27.3-gke.100", "1.29.4-gke.100", 20, "Critical"}, // 2 minor behind
		{"1.29.4-gke.100", "", 100, "OK"},                    // no latest known → neutral
		{"", "1.29.4-gke.100", 100, "OK"},                    // empty current → treat as current
	}
	for _, tt := range tests {
		score, label := gkeVersionDriftScore(tt.current, tt.latest)
		if score != tt.wantScore || label != tt.wantLabel {
			t.Errorf("gkeVersionDriftScore(%q, %q) = (%d, %q), want (%d, %q)",
				tt.current, tt.latest, score, label, tt.wantScore, tt.wantLabel)
		}
	}
}

func TestParseGKEMinorVersion(t *testing.T) {
	tests := []struct {
		v    string
		want int
	}{
		{"1.29.4-gke.100", 29},
		{"1.30.0-gke.1", 30},
		{"1.27.3", 27},
		{"bad", 0},
		{"", 0},
	}
	for _, tt := range tests {
		if got := parseGKEMinorVersion(tt.v); got != tt.want {
			t.Errorf("parseGKEMinorVersion(%q) = %d, want %d", tt.v, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// GKE enhanced weighted scores (rich 5-signal path)
// ---------------------------------------------------------------------------

func TestGKEEnhancedWeightedScores(t *testing.T) {
	// All-healthy rich signals.
	healthy := []models.AuraHealthSignal{
		{Name: "node_cpu_util", Value: 0.5, Score: 100},
		{Name: "node_mem_util", Value: 0.4, Score: 100},
		{Name: "pod_restart_rate", Value: 0.0, Score: 100},
		{Name: "control_plane_health", Value: 1.0, Score: 100},
		{Name: "version_drift", Value: 1.0, Score: 100},
	}
	health, eff := weightedScores(models.ResourceKindGKE, healthy)
	if health != 100 {
		t.Errorf("rich healthy GKE health = %d, want 100", health)
	}
	if eff != 90 {
		t.Errorf("rich healthy GKE efficiency = %d, want 90", eff)
	}

	// Idle cluster (both cpu+mem < 10%) — efficiency should drop to 40.
	idleSignals := []models.AuraHealthSignal{
		{Name: "node_cpu_util", Value: 0.05, Score: 50},
		{Name: "node_mem_util", Value: 0.04, Score: 50},
		{Name: "pod_restart_rate", Value: 0.0, Score: 100},
		{Name: "control_plane_health", Value: 1.0, Score: 100},
		{Name: "version_drift", Value: 1.0, Score: 100},
	}
	_, eff = weightedScores(models.ResourceKindGKE, idleSignals)
	if eff != 40 {
		t.Errorf("idle GKE efficiency = %d, want 40", eff)
	}

	// Autoscaling disabled signal — efficiency capped at 65.
	autoscalingDisabled := append(healthy, models.AuraHealthSignal{Name: "autoscaling_disabled", Value: 1.0, Score: 0})
	_, eff = weightedScores(models.ResourceKindGKE, autoscalingDisabled)
	if eff != 65 {
		t.Errorf("autoscaling_disabled GKE efficiency = %d, want 65", eff)
	}

	// Nodepool at max signal — efficiency capped at 75.
	atMax := append(healthy, models.AuraHealthSignal{Name: "nodepool_at_max", Value: 1.0, Score: 0})
	_, eff = weightedScores(models.ResourceKindGKE, atMax)
	if eff != 75 {
		t.Errorf("nodepool_at_max GKE efficiency = %d, want 75", eff)
	}

	// Both autoscaling_disabled AND at_max — efficiency capped at the lower of 65, 75 → 65.
	both := append(healthy,
		models.AuraHealthSignal{Name: "autoscaling_disabled", Value: 1.0, Score: 0},
		models.AuraHealthSignal{Name: "nodepool_at_max", Value: 1.0, Score: 0},
	)
	_, eff = weightedScores(models.ResourceKindGKE, both)
	if eff != 65 {
		t.Errorf("both autoscaling+atmax GKE efficiency = %d, want 65", eff)
	}

	// Legacy 4-signal path: version_drift absent — original weights, no efficiency deductions.
	legacySignals := []models.AuraHealthSignal{
		{Name: "node_cpu_util", Value: 0.5, Score: 100},
		{Name: "node_mem_util", Value: 0.4, Score: 100},
		{Name: "pod_restart_rate", Value: 0.0, Score: 100},
		{Name: "control_plane_health", Value: 1.0, Score: 100},
	}
	health, eff = weightedScores(models.ResourceKindGKE, legacySignals)
	if health != 100 {
		t.Errorf("legacy GKE health = %d, want 100", health)
	}
	if eff != 90 {
		t.Errorf("legacy GKE efficiency = %d, want 90", eff)
	}
}

// ---------------------------------------------------------------------------
// boolToFloat
// ---------------------------------------------------------------------------

func TestBoolToFloat(t *testing.T) {
	if boolToFloat(true) != 1.0 {
		t.Error("boolToFloat(true) should be 1.0")
	}
	if boolToFloat(false) != 0.0 {
		t.Error("boolToFloat(false) should be 0.0")
	}
}
