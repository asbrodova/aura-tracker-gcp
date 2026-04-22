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
