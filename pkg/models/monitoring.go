package models

type MetricPoint struct {
	Timestamp string  `json:"timestamp"`
	Value     float64 `json:"value"`
}

type GetMetricsRequest struct {
	ProjectID              string            `json:"project_id"`
	MetricType             string            `json:"metric_type"`
	ResourceLabels         map[string]string `json:"resource_labels,omitempty"`
	LookbackMinutes        int               `json:"lookback_minutes"`
	AlignmentPeriodSeconds int               `json:"alignment_period_seconds"`
}

type GetMetricsResponse struct {
	MetricType string        `json:"metric_type"`
	Points     []MetricPoint `json:"points"`
	Unit       string        `json:"unit"`
}

// MetricDescriptorSummary is a lightweight view of a Cloud Monitoring metric descriptor.
type MetricDescriptorSummary struct {
	Type        string `json:"type"`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`
	MetricKind  string `json:"metric_kind,omitempty"`
	ValueType   string `json:"value_type,omitempty"`
	Unit        string `json:"unit,omitempty"`
}

// ListMetricDescriptorsRequest lists metric descriptors.
type ListMetricDescriptorsRequest struct {
	ProjectID string `json:"project_id"`
	Filter    string `json:"filter,omitempty"` // optional prefix filter, e.g. "metric.type = starts_with(\"custom.\")"
}

// ListMetricDescriptorsResponse holds the list result.
type ListMetricDescriptorsResponse struct {
	Descriptors []MetricDescriptorSummary `json:"descriptors"`
}

// TraceService represents a service discovered via Cloud Trace.
type TraceService struct {
	Name string `json:"name"`
}

// ListTraceServicesRequest lists services that have sent traces.
type ListTraceServicesRequest struct {
	ProjectID string `json:"project_id"`
}

// ListTraceServicesResponse holds the list result.
type ListTraceServicesResponse struct {
	Services []TraceService `json:"services"`
	Backend  string         `json:"backend"` // "trace" or "monitoring"
}
