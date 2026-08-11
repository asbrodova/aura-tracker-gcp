package models

type ListClustersRequest struct {
	ProjectID string `json:"project_id"`
	// Location is a GCP region, zone, or "-" for all.
	Location string `json:"location"`
}

type ClusterSummary struct {
	Name           string            `json:"name"`
	Location       string            `json:"location"`
	Status         string            `json:"status"`
	NodeCount      int32             `json:"node_count"`
	K8sVersion     string            `json:"kubernetes_version"`
	ResourceLabels map[string]string `json:"resource_labels,omitempty"`
}

type ListClustersResponse struct {
	Clusters []ClusterSummary `json:"clusters"`
}

type GetClusterDetailsRequest struct {
	ProjectID   string `json:"project_id"`
	Location    string `json:"location"`
	ClusterName string `json:"cluster_name"`
}

type NodePoolSummary struct {
	Name               string            `json:"name"`
	MachineType        string            `json:"machine_type"`
	NodeCount          int32             `json:"node_count"`
	Status             string            `json:"status"`
	Version            string            `json:"version,omitempty"`
	Locations          []string          `json:"locations,omitempty"`
	DiskType           string            `json:"disk_type,omitempty"`
	DiskSizeGB         int32             `json:"disk_size_gb,omitempty"`
	ImageType          string            `json:"image_type,omitempty"`
	ServiceAccount     string            `json:"service_account,omitempty"`
	Labels             map[string]string `json:"labels,omitempty"`
	ResourceLabels     map[string]string `json:"resource_labels,omitempty"`
	Tags               []string          `json:"tags,omitempty"`
	Taints             []string          `json:"taints,omitempty"`
	Preemptible        bool              `json:"preemptible"`
	Spot               bool              `json:"spot"`
	AutoscalingEnabled bool              `json:"autoscaling_enabled"`
	MinNodeCount       int32             `json:"min_node_count,omitempty"`
	MaxNodeCount       int32             `json:"max_node_count,omitempty"`
	AutoUpgrade        bool              `json:"auto_upgrade"`
	AutoRepair         bool              `json:"auto_repair"`
	MaxPodsPerNode     int64             `json:"max_pods_per_node,omitempty"`
}

type ClusterDetails struct {
	ClusterSummary
	NodePools                     []NodePoolSummary `json:"node_pools"`
	Endpoint                      string            `json:"endpoint"`
	CreateTime                    string            `json:"create_time"`
	Description                   string            `json:"description,omitempty"`
	Network                       string            `json:"network,omitempty"`
	Subnetwork                    string            `json:"subnetwork,omitempty"`
	NodeLocations                 []string          `json:"node_locations,omitempty"`
	LoggingService                string            `json:"logging_service,omitempty"`
	MonitoringService             string            `json:"monitoring_service,omitempty"`
	ReleaseChannel                string            `json:"release_channel,omitempty"`
	InitialClusterVersion         string            `json:"initial_cluster_version,omitempty"`
	WorkloadIdentityPool          string            `json:"workload_identity_pool,omitempty"`
	PrivateNodes                  bool              `json:"private_nodes"`
	PrivateEndpoint               bool              `json:"private_endpoint"`
	MasterIPv4CIDR                string            `json:"master_ipv4_cidr,omitempty"`
	MasterAuthorizedNetworks      []string          `json:"master_authorized_networks,omitempty"`
	NetworkPolicyEnabled          bool              `json:"network_policy_enabled"`
	NetworkPolicyProvider         string            `json:"network_policy_provider,omitempty"`
	DataplaneProvider             string            `json:"dataplane_provider,omitempty"`
	BinaryAuthorizationMode       string            `json:"binary_authorization_mode,omitempty"`
	DatabaseEncryptionState       string            `json:"database_encryption_state,omitempty"`
	ShieldedNodesEnabled          bool              `json:"shielded_nodes_enabled"`
	VerticalPodAutoscaling        bool              `json:"vertical_pod_autoscaling"`
	NodeAutoprovisioning          bool              `json:"node_autoprovisioning"`
	AutoscalingProfile            string            `json:"autoscaling_profile,omitempty"`
	AutopilotEnabled              bool              `json:"autopilot_enabled"`
	CostManagementEnabled         bool              `json:"cost_management_enabled"`
	HTTPLoadBalancingDisabled     bool              `json:"http_load_balancing_disabled"`
	HorizontalAutoscalingDisabled bool              `json:"horizontal_pod_autoscaling_disabled"`
	NetworkPolicyAddonDisabled    bool              `json:"network_policy_addon_disabled"`
	DNSCacheEnabled               bool              `json:"dns_cache_enabled"`
}

type GetClusterBottlenecksRequest struct {
	ProjectID       string `json:"project_id"`
	Location        string `json:"location"`
	ClusterName     string `json:"cluster_name"`
	LookbackMinutes int    `json:"lookback_minutes"`
}

type BottleneckSeverity string

const (
	SeverityNone     BottleneckSeverity = "NONE"
	SeverityLow      BottleneckSeverity = "LOW"
	SeverityMedium   BottleneckSeverity = "MEDIUM"
	SeverityHigh     BottleneckSeverity = "HIGH"
	SeverityCritical BottleneckSeverity = "CRITICAL"
)

type ResourceBottleneck struct {
	Resource    string  `json:"resource"`
	MetricName  string  `json:"metric_name"`
	PeakValue   float64 `json:"peak_value"`
	Threshold   float64 `json:"threshold"`
	Description string  `json:"description"`
}

type LogSummary struct {
	ErrorCount   int      `json:"error_count"`
	WarningCount int      `json:"warning_count"`
	TopMessages  []string `json:"top_messages"`
}

type ClusterBottleneckReport struct {
	ProjectID     string               `json:"project_id"`
	ClusterName   string               `json:"cluster_name"`
	Location      string               `json:"location"`
	GeneratedAt   string               `json:"generated_at"`
	Severity      BottleneckSeverity   `json:"severity"`
	Bottlenecks   []ResourceBottleneck `json:"bottlenecks"`
	CPUMetrics    []MetricPoint        `json:"cpu_metrics"`
	MemoryMetrics []MetricPoint        `json:"memory_metrics"`
	LogSummary    LogSummary           `json:"log_summary"`
	Summary       string               `json:"summary"`
}

type ScaleDeploymentRequest struct {
	ProjectID     string `json:"project_id"`
	Location      string `json:"location"`
	ClusterName   string `json:"cluster_name"`
	NodePoolName  string `json:"node_pool_name"`
	NodeCount     int32  `json:"node_count"`
	DryRun        bool   `json:"dry_run"`
	ConfirmPlanID string `json:"confirm_plan_id,omitempty"`
	ExpectedCount *int32 `json:"-"`
}

type ScaleDeploymentResponse struct {
	DryRun         bool   `json:"dry_run"`
	NodePoolName   string `json:"node_pool_name"`
	PreviousCount  int32  `json:"previous_count"`
	RequestedCount int32  `json:"requested_count"`
	NoChangeNeeded bool   `json:"no_change_needed"`
	Description    string `json:"description"`
	PlanID         string `json:"plan_id,omitempty"`
	ExpiresIn      string `json:"expires_in,omitempty"`
	OperationName  string `json:"operation_name,omitempty"`
}
