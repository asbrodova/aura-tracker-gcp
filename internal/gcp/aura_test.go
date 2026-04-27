package gcp

import (
	"strings"
	"testing"
	"time"

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
		{0.03, 55, "Warning"},  // over-provisioned
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
		{0.05, 60, "Warning"},  // idle
		{0.50, 100, "OK"},
		{0.80, 75, "OK"},
		{0.90, 40, "Warning"},
		{0.97, 0, "Critical"},
	}
	for _, tt := range tests {
		score, label := sqlSignalScore("cpu_util", tt.value)
		if score != tt.wantScore || label != tt.wantLabel {
			t.Errorf("sqlSignalScore(cpu_util, %.2f) = (%d, %q), want (%d, %q)",
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
		name      string
		byName    map[string]int
		baseEff   int
		wantEff   int
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
	display := buildDisplay(models.ResourceKindCloudRun, "auth-service", 98, "Healthy & Scaled")
	want := "🟢 Cloud Run: auth-service | Aura: 98 (Healthy & Scaled)"
	if display != want {
		t.Errorf("buildDisplay = %q, want %q", display, want)
	}
}

func TestBuildDisplay_Yellow(t *testing.T) {
	display := buildDisplay(models.ResourceKindCloudSQL, "main-db", 65, "Healthy, but High Disk Cost")
	want := "🟡 Cloud SQL: main-db | Aura: 65 (Healthy, but High Disk Cost)"
	if display != want {
		t.Errorf("buildDisplay = %q, want %q", display, want)
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
