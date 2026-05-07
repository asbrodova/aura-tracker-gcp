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

// --- Alert Policies ---

type AlertPolicySummary struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name,omitempty"`
	Enabled     bool     `json:"enabled"`
	Severity    string   `json:"severity,omitempty"`
	Conditions  []string `json:"conditions,omitempty"`
}

type ListAlertPoliciesRequest struct {
	ProjectID string `json:"project_id"`
	Filter    string `json:"filter,omitempty"`
}

type ListAlertPoliciesResponse struct {
	Policies []AlertPolicySummary `json:"policies"`
}

// --- Uptime Checks ---

type UptimeCheckSummary struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	Period      string `json:"period,omitempty"`
	Timeout     string `json:"timeout,omitempty"`
	CheckerType string `json:"checker_type,omitempty"`
}

type ListUptimeChecksRequest struct {
	ProjectID string `json:"project_id"`
}

type ListUptimeChecksResponse struct {
	UptimeChecks []UptimeCheckSummary `json:"uptime_checks"`
}

// --- SLOs ---

type SLOSummary struct {
	Name        string  `json:"name"`
	DisplayName string  `json:"display_name,omitempty"`
	Goal        float64 `json:"goal,omitempty"`
	CalendarPeriod string `json:"calendar_period,omitempty"`
}

type ListSLOsRequest struct {
	ProjectID   string `json:"project_id"`
	ServiceName string `json:"service_name,omitempty"`
}

type ListSLOsResponse struct {
	SLOs []SLOSummary `json:"slos"`
}

// --- Dashboards ---

type DashboardSummary struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	Etag        string `json:"etag,omitempty"`
}

type ListDashboardsRequest struct {
	ProjectID string `json:"project_id"`
}

type ListDashboardsResponse struct {
	Dashboards []DashboardSummary `json:"dashboards"`
}

// TraceDependencyEdge represents a caller→callee relationship inferred from Cloud Trace spans.
type TraceDependencyEdge struct {
	Caller     string `json:"caller"`
	Callee     string `json:"callee"`
	SampleCount int   `json:"sample_count"`
}

// ListTraceDependencyEdgesRequest requests trace-derived service dependency edges.
type ListTraceDependencyEdgesRequest struct {
	ProjectID     string `json:"project_id"`
	LookbackHours int    `json:"lookback_hours,omitempty"` // default 168 (7 days)
}

// ListTraceDependencyEdgesResponse holds the inferred edges.
type ListTraceDependencyEdgesResponse struct {
	Edges []TraceDependencyEdge `json:"edges"`
	// TracesScanned is the number of traces examined.
	TracesScanned int    `json:"traces_scanned"`
	LookbackHours int    `json:"lookback_hours"`
	Backend       string `json:"backend"` // always "trace"
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
