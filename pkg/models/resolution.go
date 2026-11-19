package models

// ResolutionInput is the combined raw data fed into the resolution-rules engine.
// All fields are optional; absent slices simply skip the corresponding pass.
// This type is populated by the architecture graph fan-out (PR13) and may also
// be used directly in tests.
type ResolutionInput struct {
	// Nodes is the full assembled node set (all resource kinds).
	// The engine uses this for ID lookups when wiring edges.
	Nodes []GraphNode

	// GKE workloads — used by Pass 1 for Kubernetes Service selectors.
	Workloads []GKEWorkloadSummary

	// K8s Services — used by Pass 1 for selector→workload (exposes) and
	// NEG annotation → load_balanced_by edges.
	K8sServices []GKEServiceSummary

	// K8s Ingresses — used by Pass 1 to wire ingress → GCP LB (routes_to).
	Ingresses []GKEIngressSummary

	// Pub/Sub subscriptions — used by Pass 1 for push endpoint → Cloud Run
	// service URL matching (triggers edges).
	Subscriptions []SubscriptionSummary

	// Event-driven configuration used by Pass 1 for deterministic trigger,
	// topic, subscription, and dead-letter relationships.
	Triggers      []TriggerSummary
	SchedulerJobs []SchedulerJobSummary
	Topics        []TopicSummary

	// Mesh edges from gcp_gke_get_mesh_topology — used by Pass 3.
	MeshEdges []GKEMeshEdge

	// Trace dependency edges from gcp_trace_list_dependency_edges — used by Pass 4.
	TraceEdges []TraceDependencyEdge

	// Aggregated VPC Flow Log connections between GKE endpoints — used by
	// Pass 5 (opt-in, low confidence).
	FlowEdges []FlowLogEdge

	// IAM bindings keyed by resource URN — used by Pass 2.
	// Map: resource URN → []IAMBinding
	IAMBindingsByResource map[string][]IAMBinding

	// WorkloadSAByNodeID maps GraphNode.ID → service account email for workload nodes.
	// Used by Pass 2 to correlate workloads with IAM bindings.
	WorkloadSAByNodeID map[string]string

	// IncludeExternal controls whether external_endpoint nodes are minted for
	// hostnames that resolve outside GCP-managed domains.
	IncludeExternal bool
}

// ResolutionOutput is the result of the resolution engine.
type ResolutionOutput struct {
	// Edges are the inferred directed relationships, merged and deduplicated.
	Edges []GraphEdge

	// MintedNodes contains external_endpoint nodes created by the engine that
	// are not present in ResolutionInput.Nodes. Callers should append these to
	// their node list before emitting the final graph.
	MintedNodes []GraphNode
}

// FlowEndpoint is one side of a VPC Flow Log connection, as GKE annotates it.
// Workload is the pod's controller (for example a Deployment); Service is set
// when the destination was reached through a Kubernetes Service.
type FlowEndpoint struct {
	Cluster   string `json:"cluster,omitempty"`
	Location  string `json:"location,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Workload  string `json:"workload,omitempty"`
	Service   string `json:"service,omitempty"`
}

// FlowLogEdge aggregates the VPC Flow Log records between two endpoints on
// one destination port and protocol.
type FlowLogEdge struct {
	Source      FlowEndpoint `json:"source"`
	Destination FlowEndpoint `json:"destination"`
	Port        int          `json:"port,omitempty"`
	Protocol    int          `json:"protocol,omitempty"` // IANA number: 6 TCP, 17 UDP
	Records     int          `json:"records"`
	Bytes       int64        `json:"bytes,omitempty"`
}
