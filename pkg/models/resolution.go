package models

// ResolutionInput is the combined raw data fed into the resolution-rules engine.
// All fields are optional; absent slices simply skip the corresponding pass.
// This type is populated by the architecture graph fan-out (PR13) and may also
// be used directly in tests.
type ResolutionInput struct {
	// Nodes is the full assembled node set (all resource kinds).
	// The engine uses this for ID lookups when wiring edges.
	Nodes []GraphNode

	// GKE workloads — used by Pass 1 for env-var / secret-ref / SA detection.
	Workloads []GKEWorkloadSummary

	// K8s Services — used by Pass 1 for selector→workload (exposes) and
	// NEG annotation → load_balanced_by edges.
	K8sServices []GKEServiceSummary

	// K8s Ingresses — used by Pass 1 to wire ingress → GCP LB (routes_to).
	Ingresses []GKEIngressSummary

	// Pub/Sub subscriptions — used by Pass 1 for push endpoint → Cloud Run
	// service URL matching (triggers edges).
	Subscriptions []SubscriptionSummary

	// Mesh edges from gcp_gke_get_mesh_topology — used by Pass 3.
	MeshEdges []GKEMeshEdge

	// Trace dependency edges from gcp_trace_list_dependency_edges — used by Pass 4.
	TraceEdges []TraceDependencyEdge

	// IAM bindings keyed by resource URN — used by Pass 2.
	// Map: resource URN → []IAMBinding
	IAMBindingsByResource map[string][]IAMBinding

	// WorkloadSAByNodeID maps GraphNode.ID → service account email for workload nodes.
	// Used by Pass 2 to correlate workloads with IAM bindings.
	WorkloadSAByNodeID map[string]string

	// IncludeExternal controls whether external_endpoint nodes are minted for
	// hostnames that resolve outside GCP-managed domains.
	IncludeExternal bool

	// EnableFlowLogInference enables Pass 5 (VPC Flow Log / log-based inference).
	// This is off by default because it requires logging.privateLogViewer and
	// produces low-confidence (0.45–0.55) edges.
	EnableFlowLogInference bool
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
