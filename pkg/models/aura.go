package models

import "time"

// ResourceKind identifies which GCP resource type an Aura Score covers.
type ResourceKind string

const (
	ResourceKindCloudRun ResourceKind = "cloud_run"
	ResourceKindBigQuery ResourceKind = "bigquery"
	ResourceKindCloudSQL ResourceKind = "cloud_sql"
	ResourceKindGKE      ResourceKind = "gke"
)

// AuraBand is the categorical tier derived from the numeric Aura Score.
type AuraBand string

const (
	AuraBandGreen  AuraBand = "green"  // 80-100
	AuraBandYellow AuraBand = "yellow" // 50-79
	AuraBandRed    AuraBand = "red"    // 0-49
)

// AuraHealthSignal is one golden-signal measurement contributing to the score.
type AuraHealthSignal struct {
	Name  string  `json:"name"`  // e.g. "error_rate", "cpu_util", "latency_p99"
	Value float64 `json:"value"` // raw metric value (ratio 0-1, ms, bytes, etc.)
	Score int     `json:"score"` // 0-100 contribution after threshold mapping
	Label string  `json:"label"` // "OK", "Warning", "Critical"
}

// AuraReport is the complete Aura Score for a single GCP resource.
type AuraReport struct {
	ResourceKind    ResourceKind       `json:"resource_kind"`
	ResourceName    string             `json:"resource_name"`
	Region          string             `json:"region"`
	Score           int                `json:"score"`            // 0-100 composite
	Band            AuraBand           `json:"band"`             // green/yellow/red
	Display         string             `json:"display"`          // "🟢 Cloud Run: svc | Aura: 98 (Healthy & Scaled)"
	HealthScore     int                `json:"health_score"`     // weighted health sub-score
	EfficiencyScore int                `json:"efficiency_score"` // utilization efficiency sub-score
	HealthSignals   []AuraHealthSignal `json:"health_signals"`
	Reasons         []string           `json:"reasons"`   // actionable improvement hints for the LLM
	CachedAt        time.Time          `json:"cached_at"` // zero if not from cache
}

// GetAuraScoreRequest is the input for the gcp_get_aura_score tool.
type GetAuraScoreRequest struct {
	ProjectID    string       `json:"project_id"`
	ResourceKind ResourceKind `json:"resource_kind"`
	ResourceName string       `json:"resource_name"`
	Region       string       `json:"region"`
}

// ProjectAuraSummaryRequest is the input for the gcp_project_aura_summary tool.
type ProjectAuraSummaryRequest struct {
	ProjectID string `json:"project_id"`
	Region    string `json:"region,omitempty"` // empty = all regions
}

// ProjectAuraSummaryResponse lists all discovered resources with their Aura Scores.
type ProjectAuraSummaryResponse struct {
	ProjectID     string       `json:"project_id"`
	Resources     []AuraReport `json:"resources"`      // sorted worst-first
	Summary       string       `json:"summary"`        // pre-formatted emoji block for LLM display
	TotalCount    int          `json:"total_count"`
	CriticalCount int          `json:"critical_count"`
	WarningCount  int          `json:"warning_count"`
	HealthyCount  int          `json:"healthy_count"`
}
