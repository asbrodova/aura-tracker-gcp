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

// trafficSignal is the Cloud Monitoring metric that independently measures a
// billed resource's traffic.
type trafficSignal struct {
	metricType string
	labels     map[string]string
	unit       string
	signal     string
	summary    string
}

// trafficSignalFor maps a billing traffic anomaly to the operational metric
// that corroborates it. Byte-priced SKUs (egress, network, data transfer) use
// a sent-bytes metric where one exists; other SKUs use request counts.
func trafficSignalFor(anomaly models.CostTrafficAnomaly) (trafficSignal, bool) {
	kind, parts := billedResourceParts(anomaly.Resource)
	bytesPriced := isBytePriced(anomaly)
	switch {
	case kind == "run.googleapis.com" || strings.EqualFold(anomaly.Service, "Cloud Run"):
		serviceName, location, ok := cloudRunResourceParts(anomaly.Resource)
		if !ok && strings.EqualFold(anomaly.Service, "Cloud Run") {
			serviceName, location = resourceLastPart(anomaly.Resource), resourceLocation(anomaly.Resource)
			ok = serviceName != "" && location != ""
		}
		if !ok {
			return trafficSignal{}, false
		}
		return trafficSignal{
			metricType: "run.googleapis.com/request_count", labels: map[string]string{"service_name": serviceName, "location": location},
			unit: "requests", signal: "request_count", summary: "Cloud Run request volume over the same billing comparison windows",
		}, true
	case kind == "cloudfunctions.googleapis.com":
		name, region := parts["functions"], parts["locations"]
		if name == "" || region == "" {
			return trafficSignal{}, false
		}
		return trafficSignal{
			metricType: "cloudfunctions.googleapis.com/function/execution_count", labels: map[string]string{"function_name": name, "region": region},
			unit: "executions", signal: "execution_count", summary: "Cloud Functions executions over the same billing comparison windows",
		}, true
	case kind == "storage.googleapis.com":
		bucket := parts["buckets"]
		if bucket == "" {
			return trafficSignal{}, false
		}
		if bytesPriced {
			return trafficSignal{
				metricType: "storage.googleapis.com/network/sent_bytes_count", labels: map[string]string{"bucket_name": bucket},
				unit: "bytes", signal: "sent_bytes", summary: "Bytes served by the bucket over the same billing comparison windows",
			}, true
		}
		return trafficSignal{
			metricType: "storage.googleapis.com/api/request_count", labels: map[string]string{"bucket_name": bucket},
			unit: "requests", signal: "request_count", summary: "Cloud Storage API requests over the same billing comparison windows",
		}, true
	case kind == "pubsub.googleapis.com":
		if topic := parts["topics"]; topic != "" {
			return trafficSignal{
				metricType: "pubsub.googleapis.com/topic/send_request_count", labels: map[string]string{"topic_id": topic},
				unit: "requests", signal: "publish_requests", summary: "Pub/Sub publish requests over the same billing comparison windows",
			}, true
		}
		if subscription := parts["subscriptions"]; subscription != "" {
			return trafficSignal{
				metricType: "pubsub.googleapis.com/subscription/sent_message_count", labels: map[string]string{"subscription_id": subscription},
				unit: "messages", signal: "delivered_messages", summary: "Pub/Sub messages delivered over the same billing comparison windows",
			}, true
		}
	case kind == "cloudsql.googleapis.com" || kind == "sqladmin.googleapis.com":
		instance, project := parts["instances"], parts["projects"]
		if instance == "" || project == "" || !bytesPriced {
			return trafficSignal{}, false
		}
		return trafficSignal{
			metricType: "cloudsql.googleapis.com/database/network/sent_bytes_count", labels: map[string]string{"database_id": project + ":" + instance},
			unit: "bytes", signal: "sent_bytes", summary: "Bytes sent by the Cloud SQL instance over the same billing comparison windows",
		}, true
	}
	return trafficSignal{}, false
}

// billedResourceParts splits a billing resource name such as
// //storage.googleapis.com/projects/_/buckets/logs into its service host and
// collection/ID pairs.
func billedResourceParts(resource string) (string, map[string]string) {
	host := ""
	path := resource
	if strings.HasPrefix(resource, "//") {
		trimmed := strings.TrimPrefix(resource, "//")
		if slash := strings.Index(trimmed, "/"); slash > 0 {
			host, path = trimmed[:slash], trimmed[slash+1:]
		}
	}
	segments := strings.Split(strings.Trim(path, "/"), "/")
	parts := make(map[string]string, len(segments)/2)
	for i := 0; i+1 < len(segments); i += 2 {
		parts[segments[i]] = segments[i+1]
	}
	return host, parts
}

func isBytePriced(anomaly models.CostTrafficAnomaly) bool {
	text := strings.ToLower(anomaly.SKU + " " + anomaly.UsageUnit)
	for _, term := range []string{"egress", "network", "data transfer", "internet", "byte"} {
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
		signal, ok := trafficSignalFor(response.TrafficAnomalies[i])
		if !ok {
			continue
		}
		attempted++
		currentMetrics, err := e.source.GetMetrics(ctx, models.GetMetricsRequest{
			ProjectID: projectID, MetricType: signal.metricType, ResourceLabels: signal.labels,
			StartTime: windows.currentStart.Format(time.RFC3339), EndTime: windows.currentEnd.Format(time.RFC3339),
			AlignmentPeriodSeconds: 3600, PerSeriesAligner: "ALIGN_SUM", MaxTimeSeries: 100,
		})
		if err != nil {
			continue
		}
		baselineMetrics, err := e.source.GetMetrics(ctx, models.GetMetricsRequest{
			ProjectID: projectID, MetricType: signal.metricType, ResourceLabels: signal.labels,
			StartTime: windows.baselineStart.Format(time.RFC3339), EndTime: windows.baselineEnd.Format(time.RFC3339),
			AlignmentPeriodSeconds: 3600, PerSeriesAligner: "ALIGN_SUM", MaxTimeSeries: 100,
		})
		if err != nil {
			continue
		}
		// Compare like with like when the baseline has a different length.
		currentValue, baselineValue := metricTotal(currentMetrics), metricTotal(baselineMetrics)*windows.baselineScale()
		metricType := currentMetrics.MetricType
		if metricType == "" {
			metricType = signal.metricType
		}
		anomaly := &response.TrafficAnomalies[i]
		anomaly.MonitoringMetric = metricType
		anomaly.MonitoringUnit = signal.unit
		anomaly.MonitoringValue = currentValue
		anomaly.MonitoringBaseline = baselineValue
		anomaly.MonitoringChange = percentChange(currentValue, baselineValue)
		if (baselineValue == 0 && currentValue > 0) || (baselineValue > 0 && currentValue >= baselineValue*1.5) {
			anomaly.Confidence = "high"
		}
		succeeded++
		updateTrafficDriver(response, *anomaly, signal)
	}
	switch {
	case attempted == 0:
		response.Coverage.Checks = append(response.Coverage.Checks, models.CostCoverageCheck{Name: "traffic_monitoring", Status: "skipped", Message: "No supported resource mapping was available."})
	case succeeded == attempted:
		response.Coverage.Checks = append(response.Coverage.Checks, models.CostCoverageCheck{Name: "traffic_monitoring", Status: "complete"})
	default:
		response.Coverage.Checks = append(response.Coverage.Checks, models.CostCoverageCheck{Name: "traffic_monitoring", Status: "partial", Message: "Some traffic candidates could not be corroborated."})
	}
}

func updateTrafficDriver(response *models.ExplainCostResponse, anomaly models.CostTrafficAnomaly, signal trafficSignal) {
	for i := range response.Drivers {
		if response.Drivers[i].Category != "traffic_spike" || response.Drivers[i].Key != anomaly.Resource {
			continue
		}
		response.Drivers[i].Confidence = anomaly.Confidence
		response.Drivers[i].Evidence = append(response.Drivers[i].Evidence, costEvidence(
			"cloud_monitoring", signal.signal, signal.summary,
			anomaly.MonitoringValue, anomaly.MonitoringBaseline, signal.unit,
		))
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
