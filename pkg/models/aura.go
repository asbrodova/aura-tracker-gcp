package models

import "time"

// ResourceKind identifies which GCP resource type an Aura Score covers.
type ResourceKind string

const (
	ResourceKindCloudRun ResourceKind = "cloud_run"
	ResourceKindBigQuery ResourceKind = "bigquery"
	ResourceKindCloudSQL ResourceKind = "cloud_sql"
	ResourceKindGKE      ResourceKind = "gke"
	ResourceKindGCS      ResourceKind = "gcs"
)

// AuraBand is the categorical tier derived from the numeric Aura Score.
type AuraBand string

const (
	AuraBandGreen       AuraBand = "green"       // 80-100
	AuraBandYellow      AuraBand = "yellow"      // 50-79
	AuraBandRed         AuraBand = "red"         // 0-49
	AuraBandUnavailable AuraBand = "unavailable" // no score because expected telemetry was not observed
)

// AuraHealthSignal is one golden-signal measurement contributing to the score.
type AuraHealthSignal struct {
	Name         string  `json:"name"`                   // e.g. "error_rate", "cpu_util", "latency_p99"
	Value        float64 `json:"value"`                  // meaningful only when availability is observed
	Score        int     `json:"score"`                  // 0-100 contribution after threshold mapping
	Label        string  `json:"label"`                  // OK, Warning, Critical, No data, or Unavailable
	Availability string  `json:"availability,omitempty"` // observed, no_data, or error; empty is treated as observed for compatibility
	Message      string  `json:"message,omitempty"`
}

// AuraReport is the complete Aura Score for a single GCP resource.
type AuraReport struct {
	ResourceKind       ResourceKind       `json:"resource_kind"`
	ResourceName       string             `json:"resource_name"`
	Region             string             `json:"region"`
	Score              int                `json:"score"`            // 0-100 composite
	Band               AuraBand           `json:"band"`             // green/yellow/red/unavailable
	Display            string             `json:"display"`          // "🟢 Cloud Run: svc | Aura: 98 (Healthy & Scaled)"
	HealthScore        int                `json:"health_score"`     // weighted health sub-score
	EfficiencyScore    int                `json:"efficiency_score"` // utilization efficiency sub-score
	HealthSignals      []AuraHealthSignal `json:"health_signals"`
	Reasons            []string           `json:"reasons"`                                 // actionable improvement hints for the LLM
	CachedAt           time.Time          `json:"cached_at"`                               // zero if not from cache
	RecommenderNote    string             `json:"recommender_note,omitempty"`              // set when a Recommender quota window is exhausted
	RecommenderRetryAt time.Time          `json:"recommender_retry_at,omitempty,omitzero"` // earliest safe time to refresh missing Recommender signals
	CoverageStatus     string             `json:"coverage_status"`                         // complete, partial, or unavailable
	SignalsObserved    int                `json:"signals_observed"`
	SignalsExpected    int                `json:"signals_expected"`
	Warnings           []string           `json:"warnings,omitempty"`
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

// NodePoolAudit captures autoscaling state for one GKE node pool.
type NodePoolAudit struct {
	Name               string `json:"name"`
	AutoscalingEnabled bool   `json:"autoscaling_enabled"`
	MinNodeCount       int32  `json:"min_node_count,omitempty"`
	MaxNodeCount       int32  `json:"max_node_count,omitempty"`
	InitialNodeCount   int32  `json:"initial_node_count"`
	AtMaxCapacity      bool   `json:"at_max_capacity,omitempty"`
	AutoRepair         bool   `json:"auto_repair"`
	AutoUpgrade        bool   `json:"auto_upgrade"`
}

// GKEVersionDrift describes a cluster's master version relative to its release channel.
type GKEVersionDrift struct {
	Channel        string `json:"channel"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version,omitempty"`
	IsCurrent      bool   `json:"is_current"`
}

// GetGKEAuraScoreRequest is the input for the gcp_gke_get_aura_score tool.
type GetGKEAuraScoreRequest struct {
	ProjectID   string `json:"project_id"`
	Location    string `json:"location"`
	ClusterName string `json:"cluster_name"`
}

// GKEAuraReport embeds AuraReport and adds GKE-specific structured fields.
type GKEAuraReport struct {
	AuraReport
	NodePools    []NodePoolAudit `json:"node_pools,omitempty"`
	VersionDrift GKEVersionDrift `json:"version_drift"`
}

// GCSSecurityPosture is the categorical security assessment for a GCS bucket.
type GCSSecurityPosture string

const (
	GCSPostureCompliant GCSSecurityPosture = "COMPLIANT" // PAP enforced AND UBLA enabled
	GCSPostureAtRisk    GCSSecurityPosture = "AT_RISK"   // one of PAP or UBLA missing
	GCSPostureCritical  GCSSecurityPosture = "CRITICAL"  // both PAP not enforced and UBLA disabled
)

// GCSAuraDetails contains the raw GCS configuration signals for inspection.
type GCSAuraDetails struct {
	UniformBucketLevelAccess bool   `json:"ubla"`
	PublicAccessPrevention   string `json:"pap"` // "enforced" | "inherited"
	VersioningEnabled        bool   `json:"versioning"`
	LifecycleRuleCount       int    `json:"lifecycle_rule_count"`
	StorageClass             string `json:"storage_class"`
}

// GetGCSAuraScoreRequest is the input for the gcp_gcs_get_aura_score tool.
type GetGCSAuraScoreRequest struct {
	ProjectID  string `json:"project_id"`
	BucketName string `json:"bucket_name"`
}

// GCSAuraReport embeds AuraReport and adds GCS-specific structured fields.
type GCSAuraReport struct {
	AuraReport
	SecurityPosture GCSSecurityPosture `json:"security_posture"`
	Details         GCSAuraDetails     `json:"details"`
}

// ProjectAuraSummaryResponse lists all discovered resources with their Aura Scores.
type ProjectAuraSummaryResponse struct {
	ProjectID        string       `json:"project_id"`
	Resources        []AuraReport `json:"resources"` // sorted worst-first
	Summary          string       `json:"summary"`   // pre-formatted emoji block for LLM display
	TotalCount       int          `json:"total_count"`
	CriticalCount    int          `json:"critical_count"`
	WarningCount     int          `json:"warning_count"`
	HealthyCount     int          `json:"healthy_count"`
	UnavailableCount int          `json:"unavailable_count"`
	Truncated        bool         `json:"truncated,omitempty"`
	Warnings         []string     `json:"warnings,omitempty"`
}
