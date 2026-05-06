package models

// GKEEnvVar is a single environment variable on a container.
// SecretRef is non-empty when the value comes from a Secret (secretKeyRef or
// envFrom secretRef), allowing the graph layer to mint reads_secret edges.
type GKEEnvVar struct {
	Name      string `json:"name"`
	Value     string `json:"value,omitempty"`    // literal value; empty for secret/configmap refs
	SecretRef string `json:"secret_ref,omitempty"` // Secret name if sourced from a Secret
}

// GKEResourceRequirements holds CPU and memory requests and limits.
type GKEResourceRequirements struct {
	CPURequest    string `json:"cpu_request,omitempty"`
	CPULimit      string `json:"cpu_limit,omitempty"`
	MemoryRequest string `json:"memory_request,omitempty"`
	MemoryLimit   string `json:"memory_limit,omitempty"`
}

// GKEContainerSummary captures the fields of a container spec relevant to
// workload listing: image, ports, env vars (with secret refs), and resources.
type GKEContainerSummary struct {
	Name      string                  `json:"name"`
	Image     string                  `json:"image"`
	Ports     []int32                 `json:"ports,omitempty"`
	EnvVars   []GKEEnvVar             `json:"env_vars,omitempty"`
	Resources GKEResourceRequirements `json:"resources,omitempty"`
	IsInit    bool                    `json:"is_init,omitempty"` // true for init containers
}

// GKEWorkloadSummary is the list-level view of a GKE workload (Deployment,
// StatefulSet, DaemonSet, CronJob, or Job).
type GKEWorkloadSummary struct {
	Name           string            `json:"name"`
	Namespace      string            `json:"namespace"`
	Kind           string            `json:"kind"` // one of the KindGKE* constants
	Replicas       int32             `json:"replicas,omitempty"`
	ReadyReplicas  int32             `json:"ready_replicas,omitempty"`
	Image          string            `json:"image"` // primary container image (first container)
	ServiceAccount string            `json:"service_account,omitempty"`
	SecretRefs     []string          `json:"secret_refs,omitempty"` // Secret names referenced by any container
	Labels         map[string]string `json:"labels,omitempty"`
	OtelSidecar    bool              `json:"otel_sidecar"` // true if OTel instrumentation detected
	WorkloadAPIFallback bool         `json:"workload_api_fallback,omitempty"` // true if returned via GKE Workloads API fallback
}

// GKEWorkloadDetails extends GKEWorkloadSummary with full container specs,
// probes, volume mounts, and all environment variables.
type GKEWorkloadDetails struct {
	GKEWorkloadSummary
	Containers    []GKEContainerSummary `json:"containers"`
	NodeSelector  map[string]string     `json:"node_selector,omitempty"`
	Tolerations   []string              `json:"tolerations,omitempty"` // key=value strings
	Annotations   map[string]string     `json:"annotations,omitempty"`
}

// ListGKEWorkloadsRequest is the input for gcp_gke_list_workloads.
type ListGKEWorkloadsRequest struct {
	ProjectID   string `json:"project_id"`
	Location    string `json:"location"`
	ClusterName string `json:"cluster_name"`
	Namespace   string `json:"namespace,omitempty"`  // empty = all namespaces
	Kind        string `json:"kind,omitempty"`       // filter by kind; empty = all workload kinds
	PageSize    int    `json:"page_size,omitempty"`  // default 500
}

// ListGKEWorkloadsResponse is the output for gcp_gke_list_workloads.
type ListGKEWorkloadsResponse struct {
	Workloads []GKEWorkloadSummary `json:"workloads"`
	Errors    []ToolError          `json:"errors,omitempty"`
}

// GetGKEWorkloadDetailsRequest is the input for gcp_gke_get_workload_details.
type GetGKEWorkloadDetailsRequest struct {
	ProjectID   string `json:"project_id"`
	Location    string `json:"location"`
	ClusterName string `json:"cluster_name"`
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	Kind        string `json:"kind"` // e.g. "Deployment", "StatefulSet"
}

// --- K8s Services ---

// GKEServicePort is a single port exposed by a K8s Service.
type GKEServicePort struct {
	Name       string `json:"name,omitempty"`
	Port       int32  `json:"port"`
	TargetPort string `json:"target_port,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
	NodePort   int32  `json:"node_port,omitempty"`
}

// GKEServiceSummary is the list-level view of a Kubernetes Service.
// NEGAnnotation is non-empty when the cloud.google.com/neg annotation is
// present, indicating this service has a linked Network Endpoint Group.
type GKEServiceSummary struct {
	Name          string            `json:"name"`
	Namespace     string            `json:"namespace"`
	Type          string            `json:"type"` // ClusterIP, NodePort, LoadBalancer, ExternalName
	ClusterIP     string            `json:"cluster_ip,omitempty"`
	ExternalIPs   []string          `json:"external_ips,omitempty"`
	Ports         []GKEServicePort  `json:"ports,omitempty"`
	Selector      map[string]string `json:"selector,omitempty"`
	NEGAnnotation string            `json:"neg_annotation,omitempty"` // raw value of cloud.google.com/neg
	Labels        map[string]string `json:"labels,omitempty"`
}

// ListGKEServicesRequest is the input for gcp_gke_list_services.
type ListGKEServicesRequest struct {
	ProjectID   string `json:"project_id"`
	Location    string `json:"location"`
	ClusterName string `json:"cluster_name"`
	Namespace   string `json:"namespace,omitempty"`
}

// ListGKEServicesResponse is the output for gcp_gke_list_services.
type ListGKEServicesResponse struct {
	Services []GKEServiceSummary `json:"services"`
	Errors   []ToolError         `json:"errors,omitempty"`
}

// --- Ingresses ---

// GKEIngressPath is a single path rule within a host rule.
type GKEIngressPath struct {
	Path        string `json:"path,omitempty"`
	PathType    string `json:"path_type,omitempty"`
	BackendName string `json:"backend_name,omitempty"` // K8s Service or Gateway backend name
	BackendPort int32  `json:"backend_port,omitempty"`
}

// GKEIngressRule maps a hostname to its path rules.
type GKEIngressRule struct {
	Host  string           `json:"host,omitempty"`
	Paths []GKEIngressPath `json:"paths,omitempty"`
}

// GKEIngressSummary covers both classic Ingress resources and Gateway API
// HTTPRoute resources. GCPLBName is non-empty when GKE has linked this
// ingress to a Compute load balancer (from annotations or status).
type GKEIngressSummary struct {
	Name       string           `json:"name"`
	Namespace  string           `json:"namespace"`
	Kind       string           `json:"kind"` // "Ingress" or "HTTPRoute"
	Hosts      []string         `json:"hosts,omitempty"`
	TLSEnabled bool             `json:"tls_enabled,omitempty"`
	Rules      []GKEIngressRule `json:"rules,omitempty"`
	GCPLBName  string           `json:"gcp_lb_name,omitempty"` // linked Compute LB name
	Labels     map[string]string `json:"labels,omitempty"`
}

// ListGKEIngressesRequest is the input for gcp_gke_list_ingresses.
type ListGKEIngressesRequest struct {
	ProjectID   string `json:"project_id"`
	Location    string `json:"location"`
	ClusterName string `json:"cluster_name"`
	Namespace   string `json:"namespace,omitempty"`
}

// ListGKEIngressesResponse is the output for gcp_gke_list_ingresses.
type ListGKEIngressesResponse struct {
	Ingresses []GKEIngressSummary `json:"ingresses"`
	Errors    []ToolError         `json:"errors,omitempty"`
}

// --- NetworkPolicies ---

// GKENetworkPolicySummary is the list-level view of a Kubernetes NetworkPolicy.
// IngressRuleCount / EgressRuleCount indicate rule presence without expanding
// the full selector trees (which are verbose and rarely needed at list level).
type GKENetworkPolicySummary struct {
	Name             string            `json:"name"`
	Namespace        string            `json:"namespace"`
	PodSelector      map[string]string `json:"pod_selector,omitempty"`
	IngressRuleCount int               `json:"ingress_rule_count"`
	EgressRuleCount  int               `json:"egress_rule_count"`
	PolicyTypes      []string          `json:"policy_types,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
}

// ListGKENetworkPoliciesRequest is the input for gcp_gke_list_network_policies.
type ListGKENetworkPoliciesRequest struct {
	ProjectID   string `json:"project_id"`
	Location    string `json:"location"`
	ClusterName string `json:"cluster_name"`
	Namespace   string `json:"namespace,omitempty"`
}

// ListGKENetworkPoliciesResponse is the output for gcp_gke_list_network_policies.
type ListGKENetworkPoliciesResponse struct {
	Policies []GKENetworkPolicySummary `json:"policies"`
	Errors   []ToolError               `json:"errors,omitempty"`
}

// --- Mesh topology ---

// GKEMeshEdge is a single caller→callee edge from service mesh telemetry.
type GKEMeshEdge struct {
	Caller            string  `json:"caller"`             // workload name
	CallerNamespace   string  `json:"caller_namespace"`
	Callee            string  `json:"callee"`             // workload name
	CalleeNamespace   string  `json:"callee_namespace"`
	RequestsPerMinute float64 `json:"requests_per_minute"`
	P99LatencyMs      float64 `json:"p99_latency_ms"`
	ErrorRate         float64 `json:"error_rate"` // 0.0–1.0
}

// GKEMeshTopologyResponse is the output for gcp_gke_get_mesh_topology.
// Backend describes the data source: "istio_metrics", "kube_endpoints", or "log_based".
type GKEMeshTopologyResponse struct {
	ClusterName string        `json:"cluster_name"`
	Location    string        `json:"location"`
	Edges       []GKEMeshEdge `json:"edges"`
	Backend     string        `json:"backend"`
	Warnings    []string      `json:"warnings,omitempty"`
}

// GetGKEMeshTopologyRequest is the input for gcp_gke_get_mesh_topology.
type GetGKEMeshTopologyRequest struct {
	ProjectID    string `json:"project_id"`
	Location     string `json:"location"`
	ClusterName  string `json:"cluster_name"`
	LookbackHours int   `json:"lookback_hours,omitempty"` // default 24
}
