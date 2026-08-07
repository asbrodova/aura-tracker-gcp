package diagnostics

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

type causeCandidate struct {
	cause models.IncidentRootCause
	score int
}

func rankRootCauses(data []serviceData, req models.DiagnoseIncidentRequest, now time.Time) []models.IncidentRootCause {
	candidates := []causeCandidate{}
	for i := range data {
		d := &data[i]
		if d.symptoms.Status == "healthy" {
			continue
		}
		for _, detector := range []func(*serviceData, models.DiagnoseIncidentRequest, time.Time) *causeCandidate{
			detectDeploymentRegression,
			detectIAMRegression,
			detectDependencyFailure,
			detectPlatformIncident,
			detectApplicationFailure,
			detectLatencyRegression,
		} {
			if candidate := detector(d, req, now); candidate != nil && candidate.score >= 20 {
				candidate.cause.Likelihood = likelihood(candidate.score)
				candidates = append(candidates, *candidate)
			}
		}
	}
	if len(candidates) == 0 {
		for i := range data {
			if data[i].symptoms.Status != "healthy" {
				candidate := unknownCause(&data[i], req)
				candidate.cause.Likelihood = likelihood(candidate.score)
				candidates = append(candidates, candidate)
			}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			if candidates[i].cause.ServiceName == candidates[j].cause.ServiceName {
				return candidates[i].cause.Code < candidates[j].cause.Code
			}
			return candidates[i].cause.ServiceName < candidates[j].cause.ServiceName
		}
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) > 10 {
		candidates = candidates[:10]
	}
	result := make([]models.IncidentRootCause, len(candidates))
	for i := range candidates {
		result[i] = candidates[i].cause
		if result[i].Evidence == nil {
			result[i].Evidence = []models.IncidentEvidence{}
		}
		if result[i].ContradictingEvidence == nil {
			result[i].ContradictingEvidence = []models.IncidentEvidence{}
		}
		if result[i].SuggestedInvestigation == nil {
			result[i].SuggestedInvestigation = []models.IncidentInvestigation{}
		}
	}
	return result
}

func detectDeploymentRegression(d *serviceData, req models.DiagnoseIncidentRequest, now time.Time) *causeCandidate {
	revisions := sortedRevisions(d.revisions)
	if len(revisions) == 0 {
		return nil
	}
	latest := revisions[0]
	created, createdOK := parseTime(latest.CreateTime)
	currentStart := now.Add(-time.Duration(req.LookbackMinutes) * time.Minute)
	totalStart := currentStart.Add(-time.Duration(req.BaselineMinutes) * time.Minute)
	if !createdOK || created.Before(totalStart) {
		return nil
	}
	candidate := baseCandidate("deployment_regression", "Recent Cloud Run revision regression", d)
	if !created.Before(currentStart) {
		candidate.addEvidence(30, evidence("revision-recent", "cloud_run_revision", latest.Name, "recent_deployment", latest.CreateTime,
			fmt.Sprintf("Revision %s was created inside the incident window.", latest.Name), nil, nil,
			map[string]any{"ready": latest.Ready, "config_fingerprint": latest.ConfigFingerprint}))
	} else {
		candidate.addEvidence(15, evidence("revision-baseline", "cloud_run_revision", latest.Name, "recent_deployment", latest.CreateTime,
			fmt.Sprintf("Revision %s was created during the comparison window.", latest.Name), nil, nil, nil))
	}
	if onset, ok := parseTime(d.symptoms.Onset); ok {
		delta := onset.Sub(created)
		if delta >= -5*time.Minute && delta <= 30*time.Minute {
			candidate.addEvidence(25, evidence("deploy-onset", "correlation", latest.Name, "temporal_proximity", d.symptoms.Onset,
				fmt.Sprintf("Symptom onset was %s after the revision was created.", delta.Round(time.Minute)), nil, nil, nil))
		}
	}
	metrics := splitRequestMetrics(d.requests, currentStart)
	if metrics.currentErrors > 0 {
		share := ratio(metrics.errorByRevision[latest.Name], metrics.currentErrors)
		if share >= 0.60 {
			candidate.addEvidence(25, evidence("revision-errors", "cloud_monitoring", latest.Name, "error_concentration", d.symptoms.Onset,
				fmt.Sprintf("%.0f%% of current 5xx responses are attributed to the latest revision.", share*100), floatPointer(share), nil, nil))
		}
	}
	if len(revisions) > 1 && latest.ConfigFingerprint != "" && latest.ConfigFingerprint != revisions[1].ConfigFingerprint {
		candidate.addEvidence(15, evidence("config-change", "cloud_run_revision", latest.Name, "configuration_changed", latest.CreateTime,
			"The latest revision configuration fingerprint differs from its predecessor.", nil, nil, nil))
	}
	if trafficPercent(d.details.Traffic, latest.Name) > 0 || d.details.LatestRevision == latest.Name {
		candidate.addEvidence(10, evidence("live-traffic", "cloud_run_service", d.target.ServiceName, "serving_traffic", "",
			"The latest revision is configured to serve traffic.", nil, nil, map[string]any{"traffic_percent": trafficPercent(d.details.Traffic, latest.Name)}))
	}
	for _, entry := range d.auditLogs {
		if entry.Audit != nil && (entry.Audit.Category == "deployment" || entry.Audit.Category == "deployment_change" || entry.Audit.Category == "traffic_change") {
			candidate.addEvidence(10, logEvidence("deployment-audit", "cloud_audit_logs", entry, "deployment_change"))
			break
		}
	}
	if len(revisions) > 1 && metrics.currentErrors > 0 {
		previousShare := ratio(metrics.errorByRevision[revisions[1].Name], metrics.currentErrors)
		latestShare := ratio(metrics.errorByRevision[latest.Name], metrics.currentErrors)
		if previousShare >= latestShare && previousShare > 0.30 {
			candidate.addContradiction(25, evidence("older-revision-errors", "cloud_monitoring", revisions[1].Name, "errors_not_isolated", d.symptoms.Onset,
				"An older revision has at least as much current error volume as the latest revision.", floatPointer(previousShare), nil, nil))
		}
	}
	candidate.cause.SuggestedInvestigation = []models.IncidentInvestigation{
		investigation(1, "Compare the latest revision with its predecessor and inspect its first errors.", "gcp_logging_query_recent", map[string]any{
			"project_id": req.ProjectID, "resource_type": "cloud_run_revision", "resource_labels": map[string]string{"service_name": d.target.ServiceName, "revision_name": latest.Name}, "min_severity": "ERROR", "lookback_minutes": req.LookbackMinutes, "max_entries": 100,
		}, "Errors should cluster on the new revision if the deployment caused the incident."),
		investigation(2, "Verify the service traffic split before considering a rollback.", "gcp_cloudrun_get_service_details", map[string]any{
			"project_id": req.ProjectID, "region": d.target.Region, "service_name": d.target.ServiceName,
		}, "The suspected revision should be receiving production traffic."),
	}
	return candidate.finish()
}

func detectIAMRegression(d *serviceData, req models.DiagnoseIncidentRequest, _ time.Time) *causeCandidate {
	candidate := baseCandidate("iam_regression", "IAM or runtime identity regression", d)
	for _, entry := range d.logs {
		if permissionText.MatchString(entry.Message) {
			candidate.addEvidence(35, logEvidence("permission-error", "cloud_logging", entry, "permission_failure"))
			break
		}
	}
	revisions := sortedRevisions(d.revisions)
	if len(revisions) > 1 && revisions[0].ServiceAccount != revisions[1].ServiceAccount {
		candidate.addEvidence(25, evidence("identity-change", "cloud_run_revision", revisions[0].Name, "runtime_identity_changed", revisions[0].CreateTime,
			"The runtime service account changed between the two newest revisions.", nil, nil, nil))
	}
	if candidate.score == 0 {
		return nil
	}
	for _, entry := range d.auditLogs {
		if entry.Audit == nil {
			continue
		}
		if entry.Audit.Category == "iam" || entry.Audit.Category == "iam_change" || strings.Contains(strings.ToLower(entry.Audit.MethodName), "setiampolicy") {
			candidate.addEvidence(30, logEvidence("iam-change", "cloud_audit_logs", entry, "iam_change"))
			break
		}
	}
	candidate.cause.SuggestedInvestigation = []models.IncidentInvestigation{
		investigation(1, "Inspect denied permissions and the principal or resource named in the error.", "gcp_logging_query_recent", map[string]any{
			"project_id": req.ProjectID, "resource_type": "cloud_run_revision", "resource_labels": map[string]string{"service_name": d.target.ServiceName}, "min_severity": "ERROR", "lookback_minutes": req.LookbackMinutes, "max_entries": 100,
		}, "Permission-denied entries should identify the missing permission and affected resource."),
		investigation(2, "Compare recent IAM policy changes with symptom onset.", "gcp_logging_query_recent", map[string]any{
			"project_id": req.ProjectID, "filter": `logName:"cloudaudit.googleapis.com%2Factivity" AND protoPayload.methodName:"SetIamPolicy"`, "lookback_minutes": req.LookbackMinutes + req.BaselineMinutes, "max_entries": 100,
		}, "A relevant binding removal or service-account change should precede the first failures."),
	}
	return candidate.finish()
}

func detectDependencyFailure(d *serviceData, req models.DiagnoseIncidentRequest, _ time.Time) *causeCandidate {
	bad := []models.IncidentDependencyObservation{}
	for _, dependency := range d.dependencies {
		status := strings.ToLower(dependency.Status)
		if len(dependency.Issues) > 0 || status == "unhealthy" || status == "failed" || status == "error" || status == "degraded" {
			bad = append(bad, dependency)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	candidate := baseCandidate("dependency_failure", "Downstream dependency failure", d)
	for i, dependency := range bad {
		weight := 55
		if i > 0 {
			weight = 10
		}
		candidate.addEvidence(weight, evidence("dependency-"+fmt.Sprint(i+1), "dependency_health", dependency.Name, "unhealthy_dependency", "",
			fmt.Sprintf("%s %s is %s: %s", dependency.Kind, dependency.Name, dependency.Status, strings.Join(dependency.Issues, "; ")), nil, nil,
			map[string]any{"topology_evidence": dependency.Evidence}))
	}
	for _, entry := range d.logs {
		if dependencyText.MatchString(entry.Message) {
			candidate.addEvidence(20, logEvidence("dependency-error", "cloud_logging", entry, "dependency_error"))
			break
		}
	}
	candidate.cause.SuggestedInvestigation = []models.IncidentInvestigation{
		investigation(1, "Validate the unhealthy dependency directly and correlate its state with application errors.", "gcp_get_service_topology", map[string]any{
			"project": req.ProjectID, "region": d.target.Region, "service_name": d.target.ServiceName, "depth": 1,
		}, "The failing resource should be on the service's active dependency path."),
		investigation(2, "Search for connection, timeout, and upstream errors around symptom onset.", "gcp_logging_query_recent", map[string]any{
			"project_id": req.ProjectID, "resource_type": "cloud_run_revision", "resource_labels": map[string]string{"service_name": d.target.ServiceName}, "min_severity": "ERROR", "lookback_minutes": req.LookbackMinutes, "max_entries": 100,
		}, "Application errors should name or implicate the unhealthy dependency."),
	}
	return candidate.finish()
}

func detectPlatformIncident(d *serviceData, req models.DiagnoseIncidentRequest, now time.Time) *causeCandidate {
	currentStart := now.Add(-time.Duration(req.LookbackMinutes) * time.Minute)
	var relevant *models.LogEntry
	for i := range d.platformLogs {
		timestamp, ok := parseTime(d.platformLogs[i].Timestamp)
		if !ok || timestamp.Before(currentStart) {
			continue
		}
		message := strings.ToLower(d.platformLogs[i].Message)
		locationRelevant := strings.Contains(message, strings.ToLower(d.target.Region)) || strings.Contains(message, "global")
		if locationRelevant && platformProductRelevant(message, d) {
			relevant = &d.platformLogs[i]
			break
		}
	}
	if relevant == nil {
		return nil
	}
	candidate := baseCandidate("platform_incident", "Google Cloud platform health event", d)
	candidate.addEvidence(75, logEvidence("platform-health", "service_health_logs", *relevant, "platform_event"))
	candidate.cause.SuggestedInvestigation = []models.IncidentInvestigation{
		investigation(1, "Confirm the event's product, region, and active time in Personalized Service Health.", "gcp_logging_query_recent", map[string]any{
			"project_id": req.ProjectID, "filter": `logName:"servicehealth.googleapis.com"`, "lookback_minutes": req.LookbackMinutes + req.BaselineMinutes, "max_entries": 50,
		}, "The event should overlap the affected product, region, and symptom window."),
	}
	return candidate.finish()
}

func platformProductRelevant(message string, d *serviceData) bool {
	if strings.Contains(message, "cloud run") || strings.Contains(message, "cloudrun") || strings.Contains(message, strings.ToLower(d.target.ServiceName)) {
		return true
	}
	for _, dependency := range d.dependencies {
		switch dependency.Kind {
		case "cloudsql_instance":
			if strings.Contains(message, "cloud sql") || strings.Contains(message, "cloudsql") {
				return true
			}
		case "pubsub_topic", "pubsub_subscription":
			if strings.Contains(message, "pub/sub") || strings.Contains(message, "pubsub") {
				return true
			}
		case "vpc_connector":
			if strings.Contains(message, "vpc") {
				return true
			}
		}
	}
	return false
}

func detectApplicationFailure(d *serviceData, req models.DiagnoseIncidentRequest, _ time.Time) *causeCandidate {
	candidate := baseCandidate("application_failure", "Application exception or request-handler failure", d)
	if d.symptoms.ErrorRate.Current >= 0.02 && (d.symptoms.ErrorRate.Current >= 0.05 || d.symptoms.ErrorRate.Current >= d.symptoms.ErrorRate.Baseline*3) {
		candidate.addEvidence(30, evidence("error-rate", "cloud_monitoring", d.target.ServiceName, "elevated_5xx_rate", d.symptoms.Onset,
			fmt.Sprintf("Current 5xx rate is %.2f%% versus %.2f%% baseline.", d.symptoms.ErrorRate.Current*100, d.symptoms.ErrorRate.Baseline*100),
			floatPointer(d.symptoms.ErrorRate.Current), floatPointer(d.symptoms.ErrorRate.Baseline), nil))
	}
	if d.symptoms.ErrorLogs > 0 {
		candidate.addEvidence(25, evidence("error-logs", "cloud_logging", d.target.ServiceName, "error_entries", d.symptoms.Onset,
			fmt.Sprintf("%d ERROR-or-higher entries occurred in the incident window.", d.symptoms.ErrorLogs), floatPointer(float64(d.symptoms.ErrorLogs)), nil, nil))
	}
	if len(d.symptoms.ErrorPatterns) > 0 {
		pattern := d.symptoms.ErrorPatterns[0]
		candidate.addEvidence(20, evidence("dominant-error", "cloud_logging", pattern.Revision, "repeated_error_pattern", pattern.FirstSeen,
			fmt.Sprintf("Error fingerprint %s repeated %d times: %s", pattern.Fingerprint, pattern.Count, pattern.Example), floatPointer(float64(pattern.Count)), nil, nil))
	}
	if candidate.score == 0 {
		return nil
	}
	candidate.cause.SuggestedInvestigation = []models.IncidentInvestigation{
		investigation(1, "Inspect the dominant error fingerprint with trace and revision context.", "gcp_logging_query_recent", map[string]any{
			"project_id": req.ProjectID, "resource_type": "cloud_run_revision", "resource_labels": map[string]string{"service_name": d.target.ServiceName}, "min_severity": "ERROR", "lookback_minutes": req.LookbackMinutes, "max_entries": 100,
		}, "Repeated stack traces or request failures should identify the failing code path."),
	}
	return candidate.finish()
}

func detectLatencyRegression(d *serviceData, req models.DiagnoseIncidentRequest, _ time.Time) *causeCandidate {
	current, baseline := d.symptoms.LatencyP99.Current, d.symptoms.LatencyP99.Baseline
	if baseline <= 0 || current < baseline*2 {
		return nil
	}
	candidate := baseCandidate("latency_regression", "Cloud Run request latency regression", d)
	candidate.addEvidence(50, evidence("p99-latency", "cloud_monitoring", d.target.ServiceName, "latency_increase", d.symptoms.Onset,
		fmt.Sprintf("Current p99 latency is %.2f %s versus %.2f baseline (%.1fx).", current, d.symptoms.LatencyP99.Unit, baseline, current/baseline),
		floatPointer(current), floatPointer(baseline), nil))
	if d.symptoms.ErrorRate.Current > d.symptoms.ErrorRate.Baseline {
		candidate.addEvidence(15, evidence("latency-errors", "cloud_monitoring", d.target.ServiceName, "errors_coincident", d.symptoms.Onset,
			"The error rate also increased during the latency regression.", floatPointer(d.symptoms.ErrorRate.Current), floatPointer(d.symptoms.ErrorRate.Baseline), nil))
	}
	candidate.cause.SuggestedInvestigation = []models.IncidentInvestigation{
		investigation(1, "Break down p99 latency by revision and compare it with the baseline window.", "gcp_monitoring_get_metrics", map[string]any{
			"project_id": req.ProjectID, "metric_type": "run.googleapis.com/request_latencies", "resource_labels": map[string]string{"service_name": d.target.ServiceName, "location": d.target.Region}, "lookback_minutes": req.LookbackMinutes + req.BaselineMinutes, "alignment_period_seconds": 60, "per_series_aligner": "ALIGN_PERCENTILE_99", "cross_series_reducer": "REDUCE_PERCENTILE_99", "group_by_fields": []string{"resource.labels.revision_name"},
		}, "A specific revision or the full service should show the elevated tail latency."),
	}
	return candidate.finish()
}

func unknownCause(d *serviceData, req models.DiagnoseIncidentRequest) causeCandidate {
	candidate := baseCandidate("unclassified_incident", "Unclassified production failure", d)
	candidate.addEvidence(25, evidence("degraded-service", "correlation", d.target.ServiceName, "service_degraded", d.symptoms.Onset,
		"The service has material failure symptoms, but collected change and dependency evidence is inconclusive.", nil, nil, nil))
	candidate.cause.SuggestedInvestigation = []models.IncidentInvestigation{
		investigation(1, "Inspect errors at symptom onset and expand the lookback if needed.", "gcp_logging_query_recent", map[string]any{
			"project_id": req.ProjectID, "resource_type": "cloud_run_revision", "resource_labels": map[string]string{"service_name": d.target.ServiceName}, "min_severity": "ERROR", "lookback_minutes": req.LookbackMinutes, "max_entries": 200,
		}, "The earliest repeated error should provide a more specific hypothesis."),
	}
	finished := candidate.finish()
	if finished == nil {
		return *candidate
	}
	return *finished
}

func baseCandidate(code, title string, d *serviceData) *causeCandidate {
	return &causeCandidate{cause: models.IncidentRootCause{
		Code: code, Title: title, ServiceName: d.target.ServiceName,
		Evidence: []models.IncidentEvidence{}, ContradictingEvidence: []models.IncidentEvidence{}, SuggestedInvestigation: []models.IncidentInvestigation{},
	}}
}

func (c *causeCandidate) addEvidence(weight int, item models.IncidentEvidence) {
	if item.Attributes == nil {
		item.Attributes = map[string]any{}
	}
	item.Attributes["score_weight"] = weight
	c.score += weight
	c.cause.Evidence = append(c.cause.Evidence, item)
}

func (c *causeCandidate) addContradiction(weight int, item models.IncidentEvidence) {
	if item.Attributes == nil {
		item.Attributes = map[string]any{}
	}
	item.Attributes["score_penalty"] = weight
	c.score -= weight
	c.cause.ContradictingEvidence = append(c.cause.ContradictingEvidence, item)
}

func (c *causeCandidate) finish() *causeCandidate {
	if c.score < 0 {
		c.score = 0
	}
	if c.score > 100 {
		c.score = 100
	}
	if c.score < 20 {
		return nil
	}
	return c
}

func likelihood(score int) models.IncidentLikelihood {
	band := "low"
	if score >= 75 {
		band = "high"
	} else if score >= 45 {
		band = "medium"
	}
	return models.IncidentLikelihood{Band: band, Score: score, Method: "heuristic-evidence-score"}
}

func evidence(id, source, resource, signal, observedAt, summary string, value, baseline *float64, attributes map[string]any) models.IncidentEvidence {
	return models.IncidentEvidence{ID: id, Source: source, Resource: resource, Signal: signal, ObservedAt: observedAt, Summary: summary, Value: value, Baseline: baseline, Attributes: attributes}
}

func logEvidence(id, source string, entry models.LogEntry, signal string) models.IncidentEvidence {
	attributes := map[string]any{"severity": entry.Severity}
	if entry.Audit != nil {
		attributes["method_name"] = entry.Audit.MethodName
		attributes["category"] = entry.Audit.Category
		attributes["changed_fields"] = entry.Audit.ChangedFields
	}
	return evidence(id, source, entry.ResourceLabels["service_name"], signal, entry.Timestamp, entry.Message, nil, nil, attributes)
}

func investigation(priority int, description, tool string, arguments map[string]any, expected string) models.IncidentInvestigation {
	return models.IncidentInvestigation{Priority: priority, Description: description, Tool: tool, Arguments: arguments, ExpectedSignal: expected, ReadOnly: true}
}

func floatPointer(value float64) *float64 { return &value }

func sortedRevisions(input []models.RevisionSummary) []models.RevisionSummary {
	result := append([]models.RevisionSummary(nil), input...)
	sort.SliceStable(result, func(i, j int) bool {
		left, _ := parseTime(result[i].CreateTime)
		right, _ := parseTime(result[j].CreateTime)
		return left.After(right)
	})
	return result
}

func trafficPercent(traffic []models.TrafficTarget, revision string) int32 {
	var result int32
	for _, target := range traffic {
		if target.Revision == revision {
			result += target.Percent
		}
	}
	return result
}
