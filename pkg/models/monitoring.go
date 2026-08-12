package models

type MetricPoint struct {
	Timestamp    string                     `json:"timestamp"`
	Value        float64                    `json:"value"`
	ValueType    string                     `json:"value_type,omitempty"`
	TextValue    string                     `json:"text_value,omitempty"`
	Distribution *MetricDistributionSummary `json:"distribution,omitempty"`
}

// MetricDistributionSummary is a compact representation of a Monitoring
// distribution point. Request a percentile aligner when an exact P50/P95/P99
// scalar is needed; otherwise this summary preserves the raw count and range.
type MetricDistributionSummary struct {
	Count int64   `json:"count"`
	Mean  float64 `json:"mean"`
	Min   float64 `json:"min,omitempty"`
	Max   float64 `json:"max,omitempty"`
}

// MetricSeries preserves the labels that identify a Monitoring time series.
type MetricSeries struct {
	MetricLabels   map[string]string `json:"metric_labels,omitempty"`
	ResourceType   string            `json:"resource_type,omitempty"`
	ResourceLabels map[string]string `json:"resource_labels,omitempty"`
	MetricKind     string            `json:"metric_kind,omitempty"`
	ValueType      string            `json:"value_type,omitempty"`
	Unit           string            `json:"unit,omitempty"`
	Points         []MetricPoint     `json:"points"`
}

type GetMetricsRequest struct {
	ProjectID              string            `json:"project_id"`
	MetricType             string            `json:"metric_type"`
	StartTime              string            `json:"start_time,omitempty"`
	EndTime                string            `json:"end_time,omitempty"`
	ResourceLabels         map[string]string `json:"resource_labels,omitempty"`
	MetricLabels           map[string]string `json:"metric_labels,omitempty"`
	LookbackMinutes        int               `json:"lookback_minutes"`
	AlignmentPeriodSeconds int               `json:"alignment_period_seconds"`
	PerSeriesAligner       string            `json:"per_series_aligner,omitempty"`
	CrossSeriesReducer     string            `json:"cross_series_reducer,omitempty"`
	GroupByFields          []string          `json:"group_by_fields,omitempty"`
	MaxTimeSeries          int               `json:"max_time_series,omitempty"`
}

type GetMetricsResponse struct {
	MetricType string `json:"metric_type"`
	// Points is retained for compatibility and contains the first returned
	// series. New callers should use Series so labels are not lost.
	Points             []MetricPoint  `json:"points"`
	Series             []MetricSeries `json:"series"`
	Unit               string         `json:"unit"`
	PerSeriesAligner   string         `json:"per_series_aligner"`
	CrossSeriesReducer string         `json:"cross_series_reducer"`
	NoData             bool           `json:"no_data"`
	Truncated          bool           `json:"truncated"`
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
	PageSize  int    `json:"page_size,omitempty"`
	PageToken string `json:"page_token,omitempty"`
}

// ListMetricDescriptorsResponse holds the list result.
type ListMetricDescriptorsResponse struct {
	Descriptors   []MetricDescriptorSummary `json:"descriptors"`
	NextPageToken string                    `json:"next_page_token,omitempty"`
	Truncated     bool                      `json:"truncated,omitempty"`
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
	PageSize  int    `json:"page_size,omitempty"`
	PageToken string `json:"page_token,omitempty"`
}

type ListAlertPoliciesResponse struct {
	Policies      []AlertPolicySummary `json:"policies"`
	NextPageToken string               `json:"next_page_token,omitempty"`
	Truncated     bool                 `json:"truncated,omitempty"`
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
	Name           string  `json:"name"`
	DisplayName    string  `json:"display_name,omitempty"`
	Goal           float64 `json:"goal,omitempty"`
	CalendarPeriod string  `json:"calendar_period,omitempty"`
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
	Caller      string `json:"caller"`
	Callee      string `json:"callee"`
	SampleCount int    `json:"sample_count"`
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
	PageSize  int    `json:"page_size,omitempty"`
	PageToken string `json:"page_token,omitempty"`
}

// ListTraceServicesResponse holds the list result.
type ListTraceServicesResponse struct {
	Services      []TraceService `json:"services"`
	Backend       string         `json:"backend"` // "trace" or "monitoring"
	NextPageToken string         `json:"next_page_token,omitempty"`
	Truncated     bool           `json:"truncated,omitempty"`
}
