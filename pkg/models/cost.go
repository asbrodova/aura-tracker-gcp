package models

// ExplainCostRequest asks the cost reasoning engine to compare one usage-cost
// window with a historical baseline. Dates are interpreted in Timezone.
type ExplainCostRequest struct {
	ProjectID      string `json:"project_id"`
	Period         string `json:"period,omitempty"`
	Comparison     string `json:"comparison,omitempty"`
	StartDate      string `json:"start_date,omitempty"`
	EndDate        string `json:"end_date,omitempty"`
	Timezone       string `json:"timezone,omitempty"`
	DetailLevel    string `json:"detail_level,omitempty"`
	MaxResults     int    `json:"max_results,omitempty"`
	IncludeIdle    *bool  `json:"include_idle,omitempty"`
	IncludeTraffic *bool  `json:"include_traffic,omitempty"`
}

type CostWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type CostScope struct {
	ProjectID       string     `json:"project_id"`
	Current         CostWindow `json:"current"`
	Baseline        CostWindow `json:"baseline"`
	HistoryStart    string     `json:"history_start"`
	Timezone        string     `json:"timezone"`
	Comparison      string     `json:"comparison"`
	CostBasis       string     `json:"cost_basis"`
	CurrentComplete bool       `json:"current_complete"`
}

type CostMeasure struct {
	GrossCost float64 `json:"gross_cost"`
	Credits   float64 `json:"credits"`
	NetCost   float64 `json:"net_cost"`
	Usage     float64 `json:"usage,omitempty"`
}

type CostComparison struct {
	Currency             string  `json:"currency"`
	CurrentGrossCost     float64 `json:"current_gross_cost"`
	CurrentCredits       float64 `json:"current_credits"`
	CurrentNetCost       float64 `json:"current_net_cost"`
	BaselineGrossCost    float64 `json:"baseline_gross_cost"`
	BaselineCredits      float64 `json:"baseline_credits"`
	BaselineNetCost      float64 `json:"baseline_net_cost"`
	Delta                float64 `json:"delta"`
	PercentChange        float64 `json:"percent_change,omitempty"`
	PercentChangeDefined bool    `json:"percent_change_defined"`
}

type HistoricalCost struct {
	Date      string  `json:"date"`
	GrossCost float64 `json:"gross_cost"`
	Credits   float64 `json:"credits"`
	NetCost   float64 `json:"net_cost"`
}

type CostEvidence struct {
	Source     string         `json:"source"`
	Signal     string         `json:"signal"`
	Summary    string         `json:"summary"`
	Current    *float64       `json:"current,omitempty"`
	Baseline   *float64       `json:"baseline,omitempty"`
	Unit       string         `json:"unit,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

type CostDriver struct {
	Rank                int            `json:"rank"`
	Category            string         `json:"category"`
	Title               string         `json:"title"`
	Dimension           string         `json:"dimension"`
	Key                 string         `json:"key"`
	CurrentCost         float64        `json:"current_cost"`
	BaselineCost        float64        `json:"baseline_cost"`
	Delta               float64        `json:"delta"`
	ContributionPercent float64        `json:"contribution_percent,omitempty"`
	UsageEffect         float64        `json:"usage_effect,omitempty"`
	RateEffect          float64        `json:"rate_effect,omitempty"`
	Confidence          string         `json:"confidence"`
	Evidence            []CostEvidence `json:"evidence"`
}

type CostContributor struct {
	Rank                int     `json:"rank"`
	Dimension           string  `json:"dimension"`
	Key                 string  `json:"key"`
	Service             string  `json:"service,omitempty"`
	SKU                 string  `json:"sku,omitempty"`
	Resource            string  `json:"resource,omitempty"`
	Location            string  `json:"location,omitempty"`
	CurrentCost         float64 `json:"current_cost"`
	BaselineCost        float64 `json:"baseline_cost"`
	Delta               float64 `json:"delta"`
	PercentChange       float64 `json:"percent_change,omitempty"`
	CurrentSpendPercent float64 `json:"current_spend_percent,omitempty"`
}

type NewCostResource struct {
	Resource       string  `json:"resource"`
	Service        string  `json:"service,omitempty"`
	FirstBilledAt  string  `json:"first_billed_at,omitempty"`
	CreatedAt      string  `json:"created_at,omitempty"`
	Classification string  `json:"classification"`
	CurrentCost    float64 `json:"current_cost"`
	Confidence     string  `json:"confidence"`
}

type IdleCostResource struct {
	Resource          string  `json:"resource"`
	Service           string  `json:"service,omitempty"`
	Subtype           string  `json:"subtype"`
	Description       string  `json:"description,omitempty"`
	Priority          string  `json:"priority,omitempty"`
	CurrentCost       float64 `json:"current_cost,omitempty"`
	MonthlySavingsUSD float64 `json:"monthly_savings_usd,omitempty"`
	Confidence        string  `json:"confidence"`
}

type CostTrafficAnomaly struct {
	Resource           string  `json:"resource,omitempty"`
	Service            string  `json:"service,omitempty"`
	SKU                string  `json:"sku,omitempty"`
	UsageUnit          string  `json:"usage_unit,omitempty"`
	CurrentUsage       float64 `json:"current_usage"`
	BaselineUsage      float64 `json:"baseline_usage"`
	UsageChange        float64 `json:"usage_change"`
	CostDelta          float64 `json:"cost_delta"`
	AnomalyScore       float64 `json:"anomaly_score,omitempty"`
	MonitoringMetric   string  `json:"monitoring_metric,omitempty"`
	MonitoringValue    float64 `json:"monitoring_value,omitempty"`
	MonitoringBaseline float64 `json:"monitoring_baseline,omitempty"`
	MonitoringChange   float64 `json:"monitoring_change,omitempty"`
	Confidence         string  `json:"confidence"`
}

type CostCoverageCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type CostCoverage struct {
	Checks                     []CostCoverageCheck `json:"checks"`
	DataThrough                string              `json:"data_through,omitempty"`
	ExportStart                string              `json:"export_start,omitempty"`
	FreshnessHours             float64             `json:"freshness_hours,omitempty"`
	HistoryDaysAvailable       int                 `json:"history_days_available,omitempty"`
	ResourceAttributionPercent float64             `json:"resource_attribution_percent,omitempty"`
	BytesProcessed             int64               `json:"bytes_processed,omitempty"`
	CacheHit                   bool                `json:"cache_hit,omitempty"`
}

type ExplainCostResponse struct {
	AnalysisID       string               `json:"analysis_id"`
	GeneratedAt      string               `json:"generated_at"`
	Status           string               `json:"status"`
	Summary          string               `json:"summary"`
	ReasoningVersion string               `json:"reasoning_version"`
	Scope            CostScope            `json:"scope"`
	Totals           CostComparison       `json:"totals"`
	History          []HistoricalCost     `json:"history"`
	Drivers          []CostDriver         `json:"drivers"`
	TopSpenders      []CostContributor    `json:"top_spenders"`
	TopIncreases     []CostContributor    `json:"top_increases"`
	NewResources     []NewCostResource    `json:"new_resources"`
	IdleResources    []IdleCostResource   `json:"idle_resources"`
	TrafficAnomalies []CostTrafficAnomaly `json:"traffic_anomalies"`
	Coverage         CostCoverage         `json:"coverage"`
	Warnings         []string             `json:"warnings"`
}

// CollectCostFactsRequest is the normalized, adapter-facing billing query.
type CollectCostFactsRequest struct {
	ProjectID     string `json:"project_id"`
	CurrentStart  string `json:"current_start"`
	CurrentEnd    string `json:"current_end"`
	BaselineStart string `json:"baseline_start"`
	BaselineEnd   string `json:"baseline_end"`
	HistoryStart  string `json:"history_start"`
	Timezone      string `json:"timezone"`
	MaxResults    int    `json:"max_results"`
}

type CostFact struct {
	Dimension string      `json:"dimension"`
	Key       string      `json:"key"`
	Service   string      `json:"service,omitempty"`
	SKU       string      `json:"sku,omitempty"`
	Resource  string      `json:"resource,omitempty"`
	Location  string      `json:"location,omitempty"`
	UsageUnit string      `json:"usage_unit,omitempty"`
	Current   CostMeasure `json:"current"`
	Baseline  CostMeasure `json:"baseline"`
}

type ResourceFirstSeen struct {
	Resource    string  `json:"resource"`
	Service     string  `json:"service,omitempty"`
	FirstSeen   string  `json:"first_seen"`
	CurrentCost float64 `json:"current_cost"`
}

type BillingCostFacts struct {
	Currency                  string              `json:"currency"`
	CurrencyCount             int64               `json:"currency_count,omitempty"`
	DataThrough               string              `json:"data_through,omitempty"`
	ExportStart               string              `json:"export_start,omitempty"`
	DetailedExport            bool                `json:"detailed_export"`
	BytesProcessed            int64               `json:"bytes_processed"`
	ResourceAttributedNetCost float64             `json:"resource_attributed_net_cost,omitempty"`
	TotalNetCostInHistory     float64             `json:"total_net_cost_in_history,omitempty"`
	Daily                     []HistoricalCost    `json:"daily"`
	Facts                     []CostFact          `json:"facts"`
	FirstSeen                 []ResourceFirstSeen `json:"first_seen"`
	Warnings                  []string            `json:"warnings"`
}

type ListCostRecommendationsRequest struct {
	ProjectID string `json:"project_id"`
	Limit     int    `json:"limit,omitempty"`
}

type CostRecommendation struct {
	Resource          string  `json:"resource"`
	RecommenderID     string  `json:"recommender_id"`
	Subtype           string  `json:"subtype"`
	Service           string  `json:"service,omitempty"`
	Description       string  `json:"description,omitempty"`
	Priority          string  `json:"priority,omitempty"`
	MonthlySavingsUSD float64 `json:"monthly_savings_usd,omitempty"`
}

type ListCostRecommendationsResponse struct {
	Available       bool                 `json:"available"`
	Complete        bool                 `json:"complete"`
	Recommendations []CostRecommendation `json:"recommendations"`
	Warnings        []string             `json:"warnings"`
}

type ListCreatedAssetsRequest struct {
	ProjectID string `json:"project_id"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Limit     int    `json:"limit,omitempty"`
}

type CreatedAsset struct {
	Name        string            `json:"name"`
	DisplayName string            `json:"display_name,omitempty"`
	AssetType   string            `json:"asset_type,omitempty"`
	Location    string            `json:"location,omitempty"`
	CreateTime  string            `json:"create_time"`
	Labels      map[string]string `json:"labels,omitempty"`
}

type ListCreatedAssetsResponse struct {
	Assets    []CreatedAsset `json:"assets"`
	Truncated bool           `json:"truncated,omitempty"`
}
