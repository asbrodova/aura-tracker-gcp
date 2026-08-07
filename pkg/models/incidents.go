package models

type DiagnoseIncidentRequest struct {
	ProjectID             string `json:"project_id"`
	Environment           string `json:"environment,omitempty"`
	ServiceName           string `json:"service_name,omitempty"`
	Region                string `json:"region,omitempty"`
	LookbackMinutes       int    `json:"lookback_minutes,omitempty"`
	BaselineMinutes       int    `json:"baseline_minutes,omitempty"`
	MaxServices           int    `json:"max_services,omitempty"`
	MaxDependencies       int    `json:"max_dependencies,omitempty"`
	DetailLevel           string `json:"detail_level,omitempty"`
	IncludePlatformHealth bool   `json:"include_platform_health,omitempty"`
}

type IncidentTarget struct {
	ServiceName string            `json:"service_name"`
	Region      string            `json:"region"`
	Labels      map[string]string `json:"labels,omitempty"`
}

type IncidentScope struct {
	ProjectID   string           `json:"project_id"`
	Environment string           `json:"environment,omitempty"`
	Targets     []IncidentTarget `json:"targets"`
	Candidates  []IncidentTarget `json:"candidates,omitempty"`
	Inferred    bool             `json:"inferred"`
	Confidence  string           `json:"confidence"`
}

type IncidentMetricObservation struct {
	Current      float64 `json:"current"`
	Baseline     float64 `json:"baseline"`
	ChangeFactor float64 `json:"change_factor,omitempty"`
	Unit         string  `json:"unit,omitempty"`
	Samples      int     `json:"samples,omitempty"`
}

type IncidentServiceSymptoms struct {
	ServiceName   string                    `json:"service_name"`
	Region        string                    `json:"region"`
	Status        string                    `json:"status"`
	Onset         string                    `json:"onset,omitempty"`
	RequestCount  IncidentMetricObservation `json:"request_count"`
	ErrorRate     IncidentMetricObservation `json:"error_rate"`
	LatencyP99    IncidentMetricObservation `json:"latency_p99"`
	ErrorLogs     int                       `json:"error_logs"`
	ErrorPatterns []IncidentErrorPattern    `json:"error_patterns,omitempty"`
}

type IncidentErrorPattern struct {
	Fingerprint string `json:"fingerprint"`
	Example     string `json:"example"`
	Count       int    `json:"count"`
	FirstSeen   string `json:"first_seen,omitempty"`
	LastSeen    string `json:"last_seen,omitempty"`
	Revision    string `json:"revision,omitempty"`
}

type IncidentDependencyObservation struct {
	ServiceName string   `json:"service_name"`
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	Region      string   `json:"region,omitempty"`
	Status      string   `json:"status"`
	Evidence    string   `json:"evidence,omitempty"`
	Issues      []string `json:"issues,omitempty"`
}

type IncidentLikelihood struct {
	Band   string `json:"band"`
	Score  int    `json:"score"`
	Method string `json:"method"`
}

type IncidentEvidence struct {
	ID         string         `json:"id"`
	Source     string         `json:"source"`
	Resource   string         `json:"resource,omitempty"`
	Signal     string         `json:"signal"`
	ObservedAt string         `json:"observed_at,omitempty"`
	Summary    string         `json:"summary"`
	Value      *float64       `json:"value,omitempty"`
	Baseline   *float64       `json:"baseline,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

type IncidentInvestigation struct {
	Priority       int            `json:"priority"`
	Description    string         `json:"description"`
	Tool           string         `json:"tool,omitempty"`
	Arguments      map[string]any `json:"arguments,omitempty"`
	ExpectedSignal string         `json:"expected_signal,omitempty"`
	ReadOnly       bool           `json:"read_only"`
}

type IncidentRootCause struct {
	Rank                   int                     `json:"rank"`
	Code                   string                  `json:"code"`
	Title                  string                  `json:"title"`
	ServiceName            string                  `json:"service_name,omitempty"`
	Likelihood             IncidentLikelihood      `json:"likelihood"`
	Evidence               []IncidentEvidence      `json:"evidence"`
	ContradictingEvidence  []IncidentEvidence      `json:"contradicting_evidence"`
	SuggestedInvestigation []IncidentInvestigation `json:"suggested_investigation"`
}

type IncidentTimelineEvent struct {
	Timestamp   string `json:"timestamp"`
	Category    string `json:"category"`
	ServiceName string `json:"service_name,omitempty"`
	Summary     string `json:"summary"`
}

type IncidentCoverageCheck struct {
	Name        string `json:"name"`
	ServiceName string `json:"service_name,omitempty"`
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
}

type IncidentCoverage struct {
	Checks   []IncidentCoverageCheck `json:"checks"`
	Complete int                     `json:"complete"`
	Partial  int                     `json:"partial"`
	Skipped  int                     `json:"skipped"`
}

type DiagnoseIncidentResponse struct {
	DiagnosisID        string                          `json:"diagnosis_id"`
	GeneratedAt        string                          `json:"generated_at"`
	Status             string                          `json:"status"`
	Summary            string                          `json:"summary"`
	ScoringVersion     string                          `json:"scoring_version"`
	Scope              IncidentScope                   `json:"scope"`
	Symptoms           []IncidentServiceSymptoms       `json:"symptoms"`
	Dependencies       []IncidentDependencyObservation `json:"dependencies,omitempty"`
	PossibleRootCauses []IncidentRootCause             `json:"possible_root_causes"`
	Timeline           []IncidentTimelineEvent         `json:"timeline"`
	Coverage           IncidentCoverage                `json:"coverage"`
	Warnings           []string                        `json:"warnings"`
}
