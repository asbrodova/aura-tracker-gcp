package diagnostics

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

var (
	uuidPattern    = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b`)
	hexPattern     = regexp.MustCompile(`(?i)\b(?:0x)?[0-9a-f]{16,}\b`)
	numberPattern  = regexp.MustCompile(`\b\d+\b`)
	spacePattern   = regexp.MustCompile(`\s+`)
	permissionText = regexp.MustCompile(`(?i)(permission.?denied|access.?denied|unauthori[sz]ed|forbidden|\b401\b|\b403\b|iam\.)`)
	dependencyText = regexp.MustCompile(`(?i)(connection refused|connection reset|dial tcp|deadline exceeded|upstream|dns|database|cloud sql|pubsub|redis|vpc connector)`)
)

type metricWindow struct {
	currentTotal    float64
	baselineTotal   float64
	currentErrors   float64
	baselineErrors  float64
	currentSamples  int
	baselineSamples int
	errorByRevision map[string]float64
	onset           time.Time
}

func analyzeSymptoms(d *serviceData, now time.Time, req models.DiagnoseIncidentRequest) models.IncidentServiceSymptoms {
	metrics := splitRequestMetrics(d.requests, now.Add(-time.Duration(req.LookbackMinutes)*time.Minute))
	currentMinutes := float64(req.LookbackMinutes)
	baselineMinutes := float64(req.BaselineMinutes)
	currentRPM := metrics.currentTotal / currentMinutes
	baselineRPM := metrics.baselineTotal / baselineMinutes
	currentErrorRate := ratio(metrics.currentErrors, metrics.currentTotal)
	baselineErrorRate := ratio(metrics.baselineErrors, metrics.baselineTotal)
	currentLatency, baselineLatency, latencyCurrentSamples, latencyBaselineSamples := splitLatency(d.latency, now.Add(-time.Duration(req.LookbackMinutes)*time.Minute))

	currentStart := now.Add(-time.Duration(req.LookbackMinutes) * time.Minute)
	currentLogs := make([]models.LogEntry, 0, len(d.logs))
	var earliestLog time.Time
	for _, entry := range d.logs {
		ts, ok := parseTime(entry.Timestamp)
		if ok && !ts.Before(currentStart) {
			currentLogs = append(currentLogs, entry)
			if earliestLog.IsZero() || ts.Before(earliestLog) {
				earliestLog = ts
			}
		}
	}
	onset := metrics.onset
	if onset.IsZero() || (!earliestLog.IsZero() && earliestLog.Before(onset)) {
		onset = earliestLog
	}
	status := "healthy"
	if severeSymptoms(currentErrorRate, baselineErrorRate, len(currentLogs), currentLatency, baselineLatency) {
		status = "degraded"
	}
	if currentErrorRate >= 0.10 && metrics.currentTotal >= 20 || len(currentLogs) >= 50 {
		status = "critical"
	}

	return models.IncidentServiceSymptoms{
		ServiceName: d.target.ServiceName,
		Region:      d.target.Region,
		Status:      status,
		Onset:       formatTime(onset),
		RequestCount: models.IncidentMetricObservation{
			Current: currentRPM, Baseline: baselineRPM,
			ChangeFactor: changeFactor(currentRPM, baselineRPM), Unit: "requests/minute",
			Samples: metrics.currentSamples + metrics.baselineSamples,
		},
		ErrorRate: models.IncidentMetricObservation{
			Current: currentErrorRate, Baseline: baselineErrorRate,
			ChangeFactor: changeFactor(currentErrorRate, baselineErrorRate), Unit: "ratio",
			Samples: metrics.currentSamples + metrics.baselineSamples,
		},
		LatencyP99: models.IncidentMetricObservation{
			Current: currentLatency, Baseline: baselineLatency,
			ChangeFactor: changeFactor(currentLatency, baselineLatency), Unit: d.latency.Unit,
			Samples: latencyCurrentSamples + latencyBaselineSamples,
		},
		ErrorLogs:     len(currentLogs),
		ErrorPatterns: errorPatterns(currentLogs, 5),
	}
}

func splitRequestMetrics(response models.GetMetricsResponse, currentStart time.Time) metricWindow {
	window := metricWindow{errorByRevision: map[string]float64{}}
	for _, series := range response.Series {
		class := strings.ToLower(series.MetricLabels["response_code_class"])
		if class == "" {
			class = strings.ToLower(series.MetricLabels["response_code"])
		}
		isError := strings.HasPrefix(class, "5")
		revision := series.ResourceLabels["revision_name"]
		for _, point := range series.Points {
			ts, ok := parseTime(point.Timestamp)
			if !ok {
				continue
			}
			if !ts.Before(currentStart) {
				window.currentTotal += point.Value
				window.currentSamples++
				if isError {
					window.currentErrors += point.Value
					window.errorByRevision[revision] += point.Value
					if point.Value > 0 && (window.onset.IsZero() || ts.Before(window.onset)) {
						window.onset = ts
					}
				}
			} else {
				window.baselineTotal += point.Value
				window.baselineSamples++
				if isError {
					window.baselineErrors += point.Value
				}
			}
		}
	}
	return window
}

func splitLatency(response models.GetMetricsResponse, currentStart time.Time) (float64, float64, int, int) {
	var current, baseline []float64
	for _, series := range response.Series {
		for _, point := range series.Points {
			ts, ok := parseTime(point.Timestamp)
			if !ok {
				continue
			}
			if !ts.Before(currentStart) {
				current = append(current, point.Value)
			} else {
				baseline = append(baseline, point.Value)
			}
		}
	}
	return percentile(current, 1.0), percentile(baseline, 0.50), len(current), len(baseline)
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	copyValues := append([]float64(nil), values...)
	sort.Float64s(copyValues)
	index := int(math.Ceil(float64(len(copyValues))*p)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(copyValues) {
		index = len(copyValues) - 1
	}
	return copyValues[index]
}

func errorPatterns(entries []models.LogEntry, limit int) []models.IncidentErrorPattern {
	type aggregate struct {
		pattern models.IncidentErrorPattern
		first   time.Time
		last    time.Time
	}
	byFingerprint := map[string]*aggregate{}
	for _, entry := range entries {
		normalized := normalizeMessage(entry.Message)
		if normalized == "" {
			continue
		}
		sum := sha256.Sum256([]byte(normalized))
		fingerprint := hex.EncodeToString(sum[:6])
		ts, _ := parseTime(entry.Timestamp)
		revision := entry.ResourceLabels["revision_name"]
		value := byFingerprint[fingerprint]
		if value == nil {
			example := strings.TrimSpace(entry.Message)
			if len(example) > 240 {
				example = example[:240] + "…"
			}
			value = &aggregate{pattern: models.IncidentErrorPattern{Fingerprint: fingerprint, Example: example, Revision: revision}, first: ts, last: ts}
			byFingerprint[fingerprint] = value
		}
		value.pattern.Count++
		if value.first.IsZero() || (!ts.IsZero() && ts.Before(value.first)) {
			value.first = ts
		}
		if ts.After(value.last) {
			value.last = ts
		}
	}
	values := make([]aggregate, 0, len(byFingerprint))
	for _, value := range byFingerprint {
		value.pattern.FirstSeen = formatTime(value.first)
		value.pattern.LastSeen = formatTime(value.last)
		values = append(values, *value)
	}
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].pattern.Count == values[j].pattern.Count {
			return values[i].pattern.Fingerprint < values[j].pattern.Fingerprint
		}
		return values[i].pattern.Count > values[j].pattern.Count
	})
	if len(values) > limit {
		values = values[:limit]
	}
	out := make([]models.IncidentErrorPattern, len(values))
	for i := range values {
		out[i] = values[i].pattern
	}
	return out
}

func normalizeMessage(message string) string {
	value := strings.ToLower(strings.TrimSpace(message))
	value = uuidPattern.ReplaceAllString(value, "<uuid>")
	value = hexPattern.ReplaceAllString(value, "<hex>")
	value = numberPattern.ReplaceAllString(value, "<n>")
	value = spacePattern.ReplaceAllString(value, " ")
	if len(value) > 500 {
		value = value[:500]
	}
	return value
}

func severeSymptoms(errorRate, baselineErrorRate float64, errorLogs int, latency, baselineLatency float64) bool {
	if errorLogs > 0 || errorRate >= 0.05 || errorRate >= math.Max(0.02, baselineErrorRate*3) {
		return true
	}
	return baselineLatency > 0 && latency >= baselineLatency*2
}

func ratio(numerator, denominator float64) float64 {
	if denominator <= 0 {
		return 0
	}
	return numerator / denominator
}

func changeFactor(current, baseline float64) float64 {
	if baseline == 0 {
		return 0
	}
	return current / baseline
}

func parseTime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return parsed, err == nil
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func buildTimeline(d *serviceData, now time.Time, req models.DiagnoseIncidentRequest) []models.IncidentTimelineEvent {
	start := now.Add(-time.Duration(req.LookbackMinutes+req.BaselineMinutes) * time.Minute)
	result := []models.IncidentTimelineEvent{}
	for _, revision := range d.revisions {
		created, ok := parseTime(revision.CreateTime)
		if ok && !created.Before(start) {
			result = append(result, models.IncidentTimelineEvent{Timestamp: formatTime(created), Category: "deployment", ServiceName: d.target.ServiceName, Summary: "Revision " + revision.Name + " created"})
		}
	}
	for _, entry := range d.auditLogs {
		ts, ok := parseTime(entry.Timestamp)
		if !ok || ts.Before(start) {
			continue
		}
		category := "configuration_change"
		if entry.Audit != nil && entry.Audit.Category != "" {
			category = entry.Audit.Category
		}
		result = append(result, models.IncidentTimelineEvent{Timestamp: formatTime(ts), Category: category, ServiceName: d.target.ServiceName, Summary: entry.Message})
	}
	if d.symptoms.Onset != "" {
		result = append(result, models.IncidentTimelineEvent{Timestamp: d.symptoms.Onset, Category: "symptom_onset", ServiceName: d.target.ServiceName, Summary: "Errors or elevated failure metrics first observed"})
	}
	return result
}

func summarize(data []serviceData, causes []models.IncidentRootCause) (string, string) {
	critical, degraded, partial := 0, 0, 0
	for _, d := range data {
		switch d.symptoms.Status {
		case "critical":
			critical++
		case "degraded":
			degraded++
		}
		if len(d.warnings) > 0 {
			partial++
		}
		for _, check := range d.coverage {
			if check.Status == "partial" {
				partial++
			}
		}
	}
	status := "healthy"
	if degraded > 0 {
		status = "degraded"
	}
	if critical > 0 {
		status = "critical"
	}
	if status == "healthy" {
		if partial > 0 {
			return "inconclusive", fmt.Sprintf("No material failure signal was found across %d service(s), but one or more collectors failed; review coverage before treating this as healthy.", len(data))
		}
		return status, fmt.Sprintf("No material production failure signal was found across %d analyzed service(s).", len(data))
	}
	if len(causes) == 0 {
		return status, fmt.Sprintf("%d critical and %d degraded service(s) detected, but the available evidence did not identify a likely root cause.", critical, degraded)
	}
	return status, fmt.Sprintf("%d critical and %d degraded service(s) detected. Leading hypothesis: %s (%s, score %d/100).", critical, degraded, causes[0].Title, causes[0].Likelihood.Band, causes[0].Likelihood.Score)
}
