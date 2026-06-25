package models

// GetObservabilityCoverageRequest is the input for gcp_observability_coverage.
type GetObservabilityCoverageRequest struct {
	ProjectID string   `json:"project_id"`
	Regions   []string `json:"regions,omitempty"` // empty = all regions
}

// NodeCoverage is the per-node observability signal breakdown.
type NodeCoverage struct {
	NodeURN  string             `json:"node_urn"`
	NodeKind string             `json:"node_kind"`
	NodeName string             `json:"node_name"`
	Coverage ObservabilityBlock `json:"coverage"`
}

// CoverageSummary aggregates signal presence across all compute nodes.
type CoverageSummary struct {
	TotalNodes        int     `json:"total_nodes"`
	WithMetrics       int     `json:"with_metrics"`
	WithTraces        int     `json:"with_traces"`
	WithLogs          int     `json:"with_logs"`
	WithAlerts        int     `json:"with_alerts"`
	WithOtelSidecar   int     `json:"with_otel_sidecar"`
	MeanCoverageScore float64 `json:"mean_coverage_score"`
	// GapNodes lists node URNs with CoverageScore < 0.5, sorted worst-first.
	GapNodes []string `json:"gap_nodes"`
}

// ObservabilityCoverageResponse is the output for gcp_observability_coverage.
type ObservabilityCoverageResponse struct {
	ProjectID       string          `json:"project_id"`
	GeneratedAt     string          `json:"generated_at"`
	Nodes           []NodeCoverage  `json:"nodes"`
	Summary         CoverageSummary `json:"summary"`
	Recommendations []string        `json:"recommendations"`
}
