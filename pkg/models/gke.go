package models

type ListClustersRequest struct {
	ProjectID string `json:"project_id"`
	// Location is a GCP region, zone, or "-" for all.
	Location string `json:"location"`
}

type ClusterSummary struct {
	Name       string `json:"name"`
	Location   string `json:"location"`
	Status     string `json:"status"`
	NodeCount  int32  `json:"node_count"`
	K8sVersion string `json:"kubernetes_version"`
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
	Name        string `json:"name"`
	MachineType string `json:"machine_type"`
	NodeCount   int32  `json:"node_count"`
	Status      string `json:"status"`
}

type ClusterDetails struct {
	ClusterSummary
	NodePools      []NodePoolSummary `json:"node_pools"`
	Endpoint       string            `json:"endpoint"`
	CreateTime     string            `json:"create_time"`
	ResourceLabels map[string]string `json:"resource_labels,omitempty"`
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
	ProjectID      string `json:"project_id"`
	Location       string `json:"location"`
	ClusterName    string `json:"cluster_name"`
	NodePoolName   string `json:"node_pool_name"`
	NodeCount      int32  `json:"node_count"`
	DryRun         bool   `json:"dry_run"`
}

type ScaleDeploymentResponse struct {
	DryRun           bool   `json:"dry_run"`
	NodePoolName     string `json:"node_pool_name"`
	PreviousCount    int32  `json:"previous_count"`
	RequestedCount   int32  `json:"requested_count"`
	NoChangeNeeded   bool   `json:"no_change_needed"`
	Description      string `json:"description"`
}
