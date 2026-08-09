package models

// Node kind constants — the closed enum across Phase 1 and Phase 2.
// Phase 1 kinds are listed first; Phase 2 additions follow.
const (
	// Phase 1 kinds
	KindCloudRunService      = "cloud_run_service"
	KindCloudRunJob          = "cloud_run_job"
	KindCloudFunctionV1      = "cloud_function_v1"
	KindCloudFunctionV2      = "cloud_function_v2"
	KindEventarcTrigger      = "eventarc_trigger"
	KindSchedulerJob         = "scheduler_job"
	KindWorkflow             = "workflow"
	KindTasksQueue           = "tasks_queue"
	KindPubSubTopic          = "pubsub_topic"
	KindPubSubSubscription   = "pubsub_subscription"
	KindSecret               = "secret"
	KindCloudSQLInstance     = "cloud_sql_instance"
	KindVPCConnector         = "vpc_connector"
	KindExternalEndpoint     = "external_endpoint"
	KindBigQueryDataset      = "bigquery_dataset"
	KindBigQuerySubscription = "bigquery_subscription"

	// Phase 2 kinds — GKE workloads
	KindGKEDeployment    = "gke_deployment"
	KindGKECluster       = "gke_cluster"
	KindGKEStatefulSet   = "gke_statefulset"
	KindGKEDaemonSet     = "gke_daemonset"
	KindGKECronJob       = "gke_cronjob"
	KindGKEJob           = "gke_job"
	KindGKEService       = "gke_service"
	KindGKEIngress       = "gke_ingress"
	KindGKEGateway       = "gke_gateway"
	KindGKENetworkPolicy = "gke_network_policy"

	// Phase 2 kinds — networking
	KindComputeLB     = "compute_lb"
	KindComputeURLMap = "compute_url_map"
	KindComputeNEG    = "compute_neg"
	KindAPIGateway    = "api_gateway"
	KindVPCNetwork    = "vpc_network"
	KindVPCSubnet     = "vpc_subnet"
	KindPSCEndpoint   = "psc_endpoint"

	// Phase 2 kinds — data stores
	KindSpannerInstance     = "spanner_instance"
	KindAlloyDBCluster      = "alloydb_cluster"
	KindFirestoreDatabase   = "firestore_database"
	KindMemorystoreInstance = "memorystore_instance"

	// Phase 2 kinds — supply chain
	KindArtifactRegistryRepo      = "artifact_registry_repo"
	KindArtifactImage             = "artifact_image"
	KindCloudBuildTrigger         = "cloud_build_trigger"
	KindServiceDirectoryNamespace = "service_directory_namespace"
	KindIAMServiceAccount         = "iam_service_account"

	// Phase 2 kinds — observability resources
	KindAlertPolicy = "alert_policy"
	KindUptimeCheck = "uptime_check"
	KindSLO         = "slo"
	KindDashboard   = "dashboard"
)

// Edge type constants — directed relationship types between nodes.
const (
	// Phase 1 edge types
	EdgeTriggers        = "triggers"
	EdgeInvokes         = "invokes"
	EdgePublishesTo     = "publishes_to"
	EdgeSubscribesTo    = "subscribes_to"
	EdgeReadsSecret     = "reads_secret"
	EdgeReadsFrom       = "reads_from"
	EdgeWritesTo        = "writes_to"
	EdgeConnectedViaVPC = "connected_via_vpc"
	EdgeDeadLettersTo   = "dead_letters_to"

	// Phase 2 edge types
	EdgeRoutesTo       = "routes_to"        // LB/ingress/url-map → backend/NEG/service
	EdgeExposes        = "exposes"          // K8s Service → K8s workload via label selector
	EdgeLoadBalancedBy = "load_balanced_by" // workload/NEG → Compute LB
	EdgeMeshCalls      = "mesh_calls"       // caller → callee (Istio telemetry)
	EdgeTraceCalls     = "trace_calls"      // caller → callee (Cloud Trace spans); metadata["protocol"] for http/grpc/tcp
	EdgeDeployedOn     = "deployed_on"      // workload → GKE cluster
	EdgeReadsFromDB    = "reads_from_db"    // workload → Spanner/AlloyDB/Firestore/Memorystore/CloudSQL
	EdgeStoredIn       = "stored_in"        // image → Artifact Registry repo
	EdgeBuiltBy        = "built_by"         // Cloud Run service/workload → Cloud Build trigger
	EdgeInNetwork      = "in_network"       // node → VPC network/subnet
	EdgePSCConnectsTo  = "psc_connects_to"  // PSC endpoint → target service
	EdgeServedBy       = "served_by"        // service → Service Directory namespace
	EdgeBoundToSA      = "bound_to_sa"      // workload → IAM service account
	EdgeGrantedOn      = "granted_on"       // IAM binding: principal → resource
)

// Evidence constants — how an edge was discovered.
const (
	// Phase 1 evidence
	EvidenceEnvVar              = "env_var"
	EvidenceAnnotation          = "annotation"
	EvidenceIAMBinding          = "iam_binding"
	EvidenceTopicPushEndpoint   = "topic_push_endpoint"
	EvidenceSchedulerTarget     = "scheduler_target"
	EvidenceEventarcDestination = "eventarc_destination"
	EvidenceWorkflowStep        = "workflow_step"
	EvidenceLabel               = "label"

	// Phase 2 evidence
	EvidenceK8sSpec          = "k8s_spec"           // Kubernetes object spec (env vars, annotations, volume mounts)
	EvidenceK8sSelector      = "k8s_selector"       // K8s Service label selector matching pod labels
	EvidenceMeshTelemetry    = "mesh_telemetry"     // Istio/ASM request_count metric
	EvidenceTraceSpan        = "trace_span"         // Cloud Trace span parent/child relationship
	EvidenceComputeBackend   = "compute_backend"    // Compute backend service / forwarding rule config
	EvidenceNEGAnnotation    = "neg_annotation"     // cloud.google.com/neg annotation on K8s Service
	EvidenceBuildTrigger     = "build_trigger"      // Cloud Build trigger substitution / source repo match
	EvidenceVPCFlowLog       = "vpc_flow_log"       // VPC Flow Log IP-to-IP inference
	EvidenceLogBased         = "log_based"          // Structured HTTP log inference fallback
	EvidenceIAMBindingScoped = "iam_binding_scoped" // IAM policy binding on a specific resource
)

// GraphGroup kind constants.
const (
	GroupKindProject   = "project"
	GroupKindRegion    = "region"
	GroupKindNetwork   = "network"
	GroupKindCluster   = "cluster"
	GroupKindNamespace = "namespace"
	GroupKindTag       = "tag"
)

// ObservabilityBlock captures signal coverage for a single compute node.
// All fields are populated by the observability coverage pass in
// gcp_export_architecture_graph and gcp_observability_coverage.
type ObservabilityBlock struct {
	HasMetrics    bool    `json:"has_metrics"`
	HasTraces     bool    `json:"has_traces"`
	HasLogs       bool    `json:"has_logs"`
	HasAlerts     bool    `json:"has_alerts"`
	OtelSidecar   bool    `json:"otel_sidecar,omitempty"`
	CoverageScore float64 `json:"coverage_score"` // 0.0–1.0
}

// GraphNode is a resource node in the architecture graph.
// ID is a stable URN: urn:gcp:<service>:<region|"-">:<project_id>:<kind>/<name>[/<sub>...]
//
// Phase 1 fields (unchanged): ID, Kind, Name, Region, ProjectID, Labels, URL,
// Generation, WrapsFunction, EventarcOwned, ServiceAccount.
// Phase 2 fields are all omitempty and never populated by Phase 1 listers.
type GraphNode struct {
	// Phase 1 fields — never removed or retyped.
	ID             string            `json:"id"`
	Kind           string            `json:"kind"`
	Name           string            `json:"name"`
	Region         string            `json:"region,omitempty"`
	ProjectID      string            `json:"project_id"`
	Labels         map[string]string `json:"labels,omitempty"`
	URL            string            `json:"url,omitempty"`
	Generation     int               `json:"generation,omitempty"`
	WrapsFunction  string            `json:"wraps_function,omitempty"`
	EventarcOwned  bool              `json:"eventarc_owned,omitempty"`
	ServiceAccount string            `json:"service_account,omitempty"`

	// Phase 2 additive fields — all omitempty; zero value means "not applicable".
	Namespace     string              `json:"namespace,omitempty"`     // GKE namespace
	ClusterName   string              `json:"cluster_name,omitempty"`  // parent GKE cluster name
	Image         string              `json:"image,omitempty"`         // primary container image (workload kinds)
	Replicas      int32               `json:"replicas,omitempty"`      // desired replica count
	Attributes    map[string]string   `json:"attributes,omitempty"`    // kind-specific KV bag for extra metadata
	Observability *ObservabilityBlock `json:"observability,omitempty"` // nil unless coverage pass ran
	Network       string              `json:"network,omitempty"`       // VPC network self-link
}

// GraphEdge is a directed relationship between two GraphNodes.
type GraphEdge struct {
	Source     string            `json:"source"`
	Target     string            `json:"target"`
	Type       string            `json:"type"`
	Evidence   string            `json:"evidence"`
	Confidence float64           `json:"confidence"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// GraphGroup is a swimlane container.
// Phase 1 kinds: project, region, network.
// Phase 2 adds: cluster, namespace, tag.
type GraphGroup struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Label   string   `json:"label"`
	Members []string `json:"members"`
}

// ServerlessGraph is the graph returned by gcp_export_serverless_graph (Phase 1)
// and gcp_export_architecture_graph (Phase 2). Errors is non-empty on partial failure.
//
// Phase 2 adds top-level metadata fields (all omitempty) for reproducibility.
type ServerlessGraph struct {
	Nodes  []GraphNode  `json:"nodes"`
	Edges  []GraphEdge  `json:"edges"`
	Groups []GraphGroup `json:"groups"`
	Errors []ToolError  `json:"errors,omitempty"`

	// Phase 2 metadata — populated by gcp_export_architecture_graph, not by
	// gcp_export_serverless_graph (which leaves these zero/nil).
	GeneratedAt    string   `json:"generated_at,omitempty"`    // RFC3339 timestamp
	ProjectID      string   `json:"project_id,omitempty"`      // project this graph covers
	RegionsScanned []string `json:"regions_scanned,omitempty"` // GCP regions included
	LookbackHours  int      `json:"lookback_hours,omitempty"`  // trace/log lookback window
	ToolVersion    string   `json:"tool_version,omitempty"`    // server version for reproducibility
	Truncated      bool     `json:"truncated,omitempty"`       // true when a view cap omitted nodes
}

// ExportServerlessGraphRequest is the input for gcp_export_serverless_graph.
type ExportServerlessGraphRequest struct {
	ProjectID         string `json:"project_id"`
	Region            string `json:"region,omitempty"`
	MaxNodes          int    `json:"max_nodes,omitempty"`
	IncludeReferences bool   `json:"include_references,omitempty"`
}

// ExportArchitectureGraphRequest is the input for gcp_export_architecture_graph.
// It produces the full project-wide graph fusing Phase 1 + Phase 2 resources.
type ExportArchitectureGraphRequest struct {
	ProjectID       string   `json:"project_id"`
	Regions         []string `json:"regions,omitempty"`          // empty = all regions
	IncludeExternal bool     `json:"include_external,omitempty"` // include external_endpoint nodes
	MaxDepth        int      `json:"max_depth,omitempty"`        // BFS depth cap; 0 = unlimited
	MaxNodes        int      `json:"max_nodes,omitempty"`        // hard node cap; 0 = unlimited
	// EnableFlowLogInference is retained for source compatibility only. The
	// adapter rejects true because flow-log evidence is not collected yet.
	EnableFlowLogInference bool `json:"-"`
	LookbackHours          int  `json:"lookback_hours,omitempty"` // trace/log lookback; default 168
}

// PartialResult wraps a list of results alongside any non-fatal errors that
// occurred during a multi-region fan-out. Callers should treat a non-empty
// Errors slice as a partial-success signal, not a hard failure.
type PartialResult[T any] struct {
	Results []T         `json:"results"`
	Errors  []ToolError `json:"errors,omitempty"`
}
