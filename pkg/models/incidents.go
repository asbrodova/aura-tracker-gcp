package models

type DiagnoseIncidentRequest struct {
	ProjectID   string `json:"project_id"`
	Environment string `json:"environment,omitempty"`
	// ResourceType is cloud_run_service (default), cloud_function, or
	// gke_workload. ServiceName names the service, function, or workload.
	ResourceType string `json:"resource_type,omitempty"`
	ServiceName  string `json:"service_name,omitempty"`
	Region       string `json:"region,omitempty"`
	// ClusterName and Namespace locate a gke_workload; Region is the
	// cluster location. WorkloadKind defaults to Deployment.
	ClusterName           string `json:"cluster_name,omitempty"`
	Namespace             string `json:"namespace,omitempty"`
	WorkloadKind          string `json:"workload_kind,omitempty"`
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
	// ResourceType is empty for a Cloud Run service. A Gen 2 Cloud Function
	// is diagnosed through its Cloud Run service (ServiceName) and keeps its
	// own name in FunctionName.
	ResourceType string `json:"resource_type,omitempty"`
	FunctionName string `json:"function_name,omitempty"`
	Generation   int    `json:"generation,omitempty"`
	ClusterName  string `json:"cluster_name,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
	WorkloadKind string `json:"workload_kind,omitempty"`
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
	// Workload is set for GKE workloads: replica availability, container
	// restarts and Kubernetes warning events in the incident window.
	Workload *IncidentWorkloadState `json:"workload,omitempty"`
}

type IncidentWorkloadState struct {
	Kind             string         `json:"kind"`
	DesiredReplicas  int32          `json:"desired_replicas"`
	ReadyReplicas    int32          `json:"ready_replicas"`
	Restarts         float64        `json:"restarts"`
	BaselineRestarts float64        `json:"baseline_restarts"`
	WarningEvents    map[string]int `json:"warning_events,omitempty"` // Kubernetes event reason → count
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
