package costreasoning

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

var trafficTerms = []string{"request", "operation", "network", "egress", "data transfer", "internet", "byte", "packet"}

func findTrafficAnomalies(facts []models.CostFact, totalDelta float64, maxResults int) []models.CostTrafficAnomaly {
	materiality := math.Max(0.01, math.Abs(totalDelta)*0.01)
	result := make([]models.CostTrafficAnomaly, 0)
	for _, fact := range facts {
		if fact.Dimension != "resource_sku" || !isTrafficFact(fact) {
			continue
		}
		delta := fact.Current.NetCost - fact.Baseline.NetCost
		if delta < materiality || fact.Current.Usage <= 0 {
			continue
		}
		if fact.Baseline.Usage > 0 && fact.Current.Usage < fact.Baseline.Usage*1.5 {
			continue
		}
		change := percentChange(fact.Current.Usage, fact.Baseline.Usage)
		score := 10.0
		if fact.Baseline.Usage > 0 {
			score = fact.Current.Usage / fact.Baseline.Usage
		}
		result = append(result, models.CostTrafficAnomaly{
			Resource: fact.Resource, Service: fact.Service, SKU: fact.SKU, UsageUnit: fact.UsageUnit,
			CurrentUsage: fact.Current.Usage, BaselineUsage: fact.Baseline.Usage, UsageChange: change,
			CostDelta: delta, AnomalyScore: score, Confidence: "medium",
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].CostDelta == result[j].CostDelta {
			return result[i].Resource < result[j].Resource
		}
		return result[i].CostDelta > result[j].CostDelta
	})
	if len(result) > maxResults {
		result = result[:maxResults]
	}
	return result
}

func isTrafficFact(fact models.CostFact) bool {
	text := strings.ToLower(fact.SKU + " " + fact.UsageUnit)
	for _, term := range trafficTerms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func (e *Engine) enrichTrafficWithMonitoring(ctx context.Context, projectID string, windows analysisWindows, response *models.ExplainCostResponse) {
	if len(response.TrafficAnomalies) == 0 {
		response.Coverage.Checks = append(response.Coverage.Checks, models.CostCoverageCheck{Name: "traffic_monitoring", Status: "complete", Message: "No material traffic candidates required corroboration."})
		return
	}
	attempted, succeeded := 0, 0
	limit := min(5, len(response.TrafficAnomalies))
	for i := 0; i < limit; i++ {
		serviceName, location, ok := cloudRunResourceParts(response.TrafficAnomalies[i].Resource)
		if !ok && strings.EqualFold(response.TrafficAnomalies[i].Service, "Cloud Run") {
			serviceName = resourceLastPart(response.TrafficAnomalies[i].Resource)
			location = resourceLocation(response.TrafficAnomalies[i].Resource)
			ok = serviceName != "" && location != ""
		}
		if !ok {
			continue
		}
		attempted++
		currentMetrics, err := e.source.GetMetrics(ctx, models.GetMetricsRequest{
			ProjectID: projectID, MetricType: "run.googleapis.com/request_count",
			ResourceLabels: map[string]string{"service_name": serviceName, "location": location},
			StartTime:      windows.currentStart.Format(time.RFC3339), EndTime: windows.currentEnd.Format(time.RFC3339),
			AlignmentPeriodSeconds: 3600, PerSeriesAligner: "ALIGN_SUM", MaxTimeSeries: 100,
		})
		if err != nil {
			continue
		}
		baselineMetrics, err := e.source.GetMetrics(ctx, models.GetMetricsRequest{
			ProjectID: projectID, MetricType: "run.googleapis.com/request_count",
			ResourceLabels: map[string]string{"service_name": serviceName, "location": location},
			StartTime:      windows.baselineStart.Format(time.RFC3339), EndTime: windows.baselineEnd.Format(time.RFC3339),
			AlignmentPeriodSeconds: 3600, PerSeriesAligner: "ALIGN_SUM", MaxTimeSeries: 100,
		})
		if err != nil {
			continue
		}
		currentValue, baselineValue := metricTotal(currentMetrics), metricTotal(baselineMetrics)
		metricType := currentMetrics.MetricType
		if metricType == "" {
			metricType = "run.googleapis.com/request_count"
		}
		response.TrafficAnomalies[i].MonitoringMetric = metricType
		response.TrafficAnomalies[i].MonitoringValue = currentValue
		response.TrafficAnomalies[i].MonitoringBaseline = baselineValue
		response.TrafficAnomalies[i].MonitoringChange = percentChange(currentValue, baselineValue)
		if (baselineValue == 0 && currentValue > 0) || (baselineValue > 0 && currentValue >= baselineValue*1.5) {
			response.TrafficAnomalies[i].Confidence = "high"
		}
		succeeded++
	}
	updateTrafficDrivers(response)
	switch {
	case attempted == 0:
		response.Coverage.Checks = append(response.Coverage.Checks, models.CostCoverageCheck{Name: "traffic_monitoring", Status: "skipped", Message: "No supported resource mapping was available."})
	case succeeded == attempted:
		response.Coverage.Checks = append(response.Coverage.Checks, models.CostCoverageCheck{Name: "traffic_monitoring", Status: "complete"})
	default:
		response.Coverage.Checks = append(response.Coverage.Checks, models.CostCoverageCheck{Name: "traffic_monitoring", Status: "partial", Message: "Some traffic candidates could not be corroborated."})
	}
}

func updateTrafficDrivers(response *models.ExplainCostResponse) {
	for _, anomaly := range response.TrafficAnomalies {
		for i := range response.Drivers {
			if response.Drivers[i].Category != "traffic_spike" || response.Drivers[i].Key != anomaly.Resource || anomaly.MonitoringMetric == "" {
				continue
			}
			response.Drivers[i].Confidence = anomaly.Confidence
			response.Drivers[i].Evidence = append(response.Drivers[i].Evidence, costEvidence(
				"cloud_monitoring", "request_count", "Operational request volume over the same billing comparison windows",
				anomaly.MonitoringValue, anomaly.MonitoringBaseline, "requests",
			))
		}
	}
}

func metricTotal(response models.GetMetricsResponse) float64 {
	total := 0.0
	for _, series := range response.Series {
		for _, point := range series.Points {
			total += point.Value
		}
	}
	if len(response.Series) == 0 {
		for _, point := range response.Points {
			total += point.Value
		}
	}
	return total
}

func cloudRunResourceParts(resource string) (string, string, bool) {
	parts := strings.Split(strings.Trim(resource, "/"), "/")
	service, location := "", ""
	for i := 0; i+1 < len(parts); i++ {
		switch parts[i] {
		case "locations":
			location = parts[i+1]
		case "services":
			service = parts[i+1]
		}
	}
	return service, location, service != "" && location != ""
}

func resourceLastPart(resource string) string {
	parts := strings.Split(strings.Trim(resource, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func resourceLocation(resource string) string {
	_, location, _ := cloudRunResourceParts(resource)
	return location
}
