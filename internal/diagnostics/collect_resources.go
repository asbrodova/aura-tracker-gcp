package diagnostics

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/internal/safego"
	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// collectTarget gathers evidence for one target. Cloud Run services and Gen 2
// functions (which run on Cloud Run) share the Cloud Run collectors.
func (e *Engine) collectTarget(ctx context.Context, req models.DiagnoseIncidentRequest, target models.IncidentTarget, now time.Time) serviceData {
	switch {
	case isGKEWorkload(target):
		return e.collectGKEWorkload(ctx, req, target, now)
	case isGen1Function(target):
		return e.collectFunctionGen1(ctx, req, target, now)
	default:
		return e.collectService(ctx, req, target, now)
	}
}

type namedCollector struct {
	name string
	fn   func(context.Context) (string, error)
}

// runCollectors runs failure-isolated collectors in parallel and records one
// coverage check per collector.
func runCollectors(ctx context.Context, d *serviceData, collectors []namedCollector) {
	type result struct {
		name, message string
		err           error
	}
	results := make([]result, len(collectors))
	g, groupCtx := safego.WithContext(ctx)
	for i, collector := range collectors {
		g.Go(func() error {
			message, err := collector.fn(groupCtx)
			results[i] = result{name: collector.name, message: message, err: err}
			return nil
		})
	}
	_ = g.Wait()
	for _, r := range results {
		status, message := "complete", r.message
		if r.err != nil {
			status, message = "partial", r.err.Error()
			d.warnings = append(d.warnings, fmt.Sprintf("%s/%s: %v", d.target.ServiceName, r.name, r.err))
		}
		d.coverage = append(d.coverage, models.IncidentCoverageCheck{Name: r.name, ServiceName: d.target.ServiceName, Status: status, Message: message})
	}
}

func (e *Engine) platformHealthCollector(req models.DiagnoseIncidentRequest, d *serviceData, mu *sync.Mutex) namedCollector {
	return namedCollector{"platform_health", func(ctx context.Context) (string, error) {
		value, err := e.source.QueryRecentLogs(ctx, models.QueryRecentLogsRequest{
			ProjectID:  req.ProjectID,
			Filter:     fmt.Sprintf(`logName="projects/%s/logs/servicehealth.googleapis.com%%2Factivity" AND resource.type="servicehealth.googleapis.com/Event" AND jsonPayload.category="INCIDENT" AND jsonPayload.relevance!="NOT_IMPACTED" AND jsonPayload.state="ACTIVE"`, req.ProjectID),
			MaxEntries: 50, LookbackMinutes: req.LookbackMinutes + req.BaselineMinutes,
		})
		if err == nil {
			mu.Lock()
			d.platformLogs = value.Entries
			mu.Unlock()
		}
		return "No platform-health log entries is not proof that the platform was healthy.", err
	}}
}

func finishCollection(req models.DiagnoseIncidentRequest, d *serviceData, dependencyMessage string) {
	d.coverage = append(d.coverage, models.IncidentCoverageCheck{Name: "dependency_health", ServiceName: d.target.ServiceName, Status: "skipped", Message: dependencyMessage})
	if !req.IncludePlatformHealth {
		d.coverage = append(d.coverage, models.IncidentCoverageCheck{Name: "platform_health", ServiceName: d.target.ServiceName, Status: "skipped", Message: "include_platform_health is false"})
	}
	sort.SliceStable(d.coverage, func(i, j int) bool { return d.coverage[i].Name < d.coverage[j].Name })
}

// collectFunctionGen1 reads a Gen 1 function's executions, execution-time
// p99, error logs, deployment time and audit changes. Executions with a
// status other than "ok" count as errors.
func (e *Engine) collectFunctionGen1(ctx context.Context, req models.DiagnoseIncidentRequest, target models.IncidentTarget, now time.Time) serviceData {
	d := serviceData{target: target, coverage: []models.IncidentCoverageCheck{}, warnings: []string{}}
	totalLookback := req.LookbackMinutes + req.BaselineMinutes
	labels := map[string]string{"function_name": target.FunctionName, "region": target.Region}
	var mu sync.Mutex
	collectors := []namedCollector{
		{"function_details", func(ctx context.Context) (string, error) {
			details, err := e.source.GetFunctionDetails(ctx, models.GetFunctionDetailsRequest{
				ProjectID: req.ProjectID, Region: target.Region, FunctionName: target.FunctionName, Generation: 1,
			})
			if err != nil {
				return "", err
			}
			if details.LastModified != "" {
				mu.Lock()
				// The latest deployment is the function's only "revision".
				d.revisions = []models.RevisionSummary{{
					Name: target.FunctionName + " deployment", ServiceName: target.FunctionName, Region: target.Region,
					CreateTime: details.LastModified, Ready: strings.EqualFold(details.Status, "ACTIVE"),
				}}
				mu.Unlock()
			}
			return "", nil
		}},
		{"error_logs", func(ctx context.Context) (string, error) {
			value, err := e.source.QueryRecentLogs(ctx, models.QueryRecentLogsRequest{
				ProjectID: req.ProjectID, ResourceType: "cloud_function", ResourceLabels: labels,
				MinSeverity: "ERROR", MaxEntries: 200, LookbackMinutes: totalLookback,
			})
			if err != nil {
				return "", err
			}
			mu.Lock()
			d.logs = value.Entries
			mu.Unlock()
			if value.Truncated {
				return "Log results reached the 200-entry cap.", nil
			}
			return "", nil
		}},
		{"audit_changes", func(ctx context.Context) (string, error) {
			filter := fmt.Sprintf(`(logName:"cloudaudit.googleapis.com%%2Factivity") AND (protoPayload.resourceName:"/functions/%s" OR protoPayload.serviceName="iam.googleapis.com" OR protoPayload.serviceName="secretmanager.googleapis.com")`, target.FunctionName)
			value, err := e.source.QueryRecentLogs(ctx, models.QueryRecentLogsRequest{ProjectID: req.ProjectID, Filter: filter, MaxEntries: 100, LookbackMinutes: totalLookback})
			if err != nil {
				return "", err
			}
			mu.Lock()
			d.auditLogs = value.Entries
			mu.Unlock()
			return "", nil
		}},
		{"execution_and_error_rate", func(ctx context.Context) (string, error) {
			value, err := e.source.GetMetrics(ctx, models.GetMetricsRequest{
				ProjectID: req.ProjectID, MetricType: "cloudfunctions.googleapis.com/function/execution_count",
				ResourceLabels: labels, LookbackMinutes: totalLookback, AlignmentPeriodSeconds: 60,
				PerSeriesAligner: "ALIGN_SUM", CrossSeriesReducer: "REDUCE_SUM",
				GroupByFields: []string{"metric.labels.status"}, MaxTimeSeries: 20,
			})
			if err != nil {
				return "", err
			}
			mu.Lock()
			d.requests = executionsAsRequests(value)
			mu.Unlock()
			return "", nil
		}},
		{"latency_p99", func(ctx context.Context) (string, error) {
			value, err := e.source.GetMetrics(ctx, models.GetMetricsRequest{
				ProjectID: req.ProjectID, MetricType: "cloudfunctions.googleapis.com/function/execution_times",
				ResourceLabels: labels, LookbackMinutes: totalLookback, AlignmentPeriodSeconds: 60,
				PerSeriesAligner: "ALIGN_PERCENTILE_99", CrossSeriesReducer: "REDUCE_PERCENTILE_99", MaxTimeSeries: 5,
			})
			if err == nil {
				mu.Lock()
				d.latency = value
				mu.Unlock()
			}
			return "", err
		}},
	}
	if req.IncludePlatformHealth {
		collectors = append(collectors, e.platformHealthCollector(req, &d, &mu))
	}
	runCollectors(ctx, &d, collectors)
	finishCollection(req, &d, "dependency topology is available for Cloud Run services and Gen 2 functions only")
	return d
}

// executionsAsRequests labels Gen 1 execution counts like Cloud Run request
// counts so the shared analysis applies: status "ok" is a success, any other
// status (error, timeout, crash, ...) a failure.
func executionsAsRequests(response models.GetMetricsResponse) models.GetMetricsResponse {
	out := response
	out.Series = make([]models.MetricSeries, len(response.Series))
	for i, series := range response.Series {
		labels := make(map[string]string, len(series.MetricLabels)+1)
		for key, value := range series.MetricLabels {
			labels[key] = value
		}
		if strings.EqualFold(labels["status"], "ok") {
			labels["response_code_class"] = "2xx"
		} else {
			labels["response_code_class"] = "5xx"
		}
		series.MetricLabels = labels
		out.Series[i] = series
	}
	return out
}

var eventReasonPattern = regexp.MustCompile(`"reason"\s*:\s*"([A-Za-z]+)"`)

// collectGKEWorkload reads the workload's replica availability, container
// restarts, Kubernetes warning events, container error logs and rollouts
// (Kubernetes audit entries for the workload).
func (e *Engine) collectGKEWorkload(ctx context.Context, req models.DiagnoseIncidentRequest, target models.IncidentTarget, now time.Time) serviceData {
	d := serviceData{target: target, coverage: []models.IncidentCoverageCheck{}, warnings: []string{}}
	totalLookback := req.LookbackMinutes + req.BaselineMinutes
	currentStart := now.Add(-time.Duration(req.LookbackMinutes) * time.Minute)
	podScope := fmt.Sprintf(`resource.labels.cluster_name="%s" AND resource.labels.location="%s" AND resource.labels.namespace_name="%s" AND resource.labels.pod_name:"%s-"`,
		escapeFilter(target.ClusterName), escapeFilter(target.Region), escapeFilter(target.Namespace), escapeFilter(target.ServiceName))
	state := &models.IncidentWorkloadState{Kind: target.WorkloadKind, WarningEvents: map[string]int{}}
	var mu sync.Mutex
	collectors := []namedCollector{
		{"workload_details", func(ctx context.Context) (string, error) {
			details, err := e.source.GetGKEWorkloadDetails(ctx, models.GetGKEWorkloadDetailsRequest{
				ProjectID: req.ProjectID, Location: target.Region, ClusterName: target.ClusterName,
				Namespace: target.Namespace, Name: target.ServiceName, Kind: target.WorkloadKind,
			})
			if err != nil {
				return "", err
			}
			mu.Lock()
			state.DesiredReplicas, state.ReadyReplicas = details.Replicas, details.ReadyReplicas
			d.workloadKnown = true
			mu.Unlock()
			return "", nil
		}},
		{"error_logs", func(ctx context.Context) (string, error) {
			value, err := e.source.QueryRecentLogs(ctx, models.QueryRecentLogsRequest{
				ProjectID: req.ProjectID, Filter: `resource.type="k8s_container" AND ` + podScope,
				MinSeverity: "ERROR", MaxEntries: 200, LookbackMinutes: totalLookback,
			})
			if err != nil {
				return "", err
			}
			mu.Lock()
			d.logs = value.Entries
			mu.Unlock()
			if value.Truncated {
				return "Log results reached the 200-entry cap.", nil
			}
			return "", nil
		}},
		{"kubernetes_events", func(ctx context.Context) (string, error) {
			value, err := e.source.QueryRecentLogs(ctx, models.QueryRecentLogsRequest{
				ProjectID:   req.ProjectID,
				Filter:      fmt.Sprintf(`logName="projects/%s/logs/events" AND resource.type="k8s_pod" AND %s`, req.ProjectID, podScope),
				MinSeverity: "WARNING", MaxEntries: 200, LookbackMinutes: req.LookbackMinutes,
			})
			if err != nil {
				return "", err
			}
			mu.Lock()
			for _, entry := range value.Entries {
				state.WarningEvents[eventReason(entry.Message)]++
			}
			mu.Unlock()
			return "", nil
		}},
		{"container_restarts", func(ctx context.Context) (string, error) {
			value, err := e.source.GetMetrics(ctx, models.GetMetricsRequest{
				ProjectID: req.ProjectID, MetricType: "kubernetes.io/container/restart_count",
				ResourceLabels:  map[string]string{"cluster_name": target.ClusterName, "location": target.Region, "namespace_name": target.Namespace},
				LookbackMinutes: totalLookback, AlignmentPeriodSeconds: 60, PerSeriesAligner: "ALIGN_DELTA",
				CrossSeriesReducer: "REDUCE_SUM", GroupByFields: []string{"resource.labels.pod_name"}, MaxTimeSeries: 200,
			})
			if err != nil {
				return "", err
			}
			current, baseline := splitRestarts(value, target.ServiceName, currentStart)
			mu.Lock()
			state.Restarts, state.BaselineRestarts = current, baseline
			mu.Unlock()
			if value.Truncated {
				return "Restart metrics reached the time-series cap; some pods may be missing.", nil
			}
			return "", nil
		}},
		{"rollout_changes", func(ctx context.Context) (string, error) {
			filter := fmt.Sprintf(`logName:"cloudaudit.googleapis.com%%2Factivity" AND protoPayload.serviceName="k8s.io" AND resource.labels.cluster_name="%s" AND protoPayload.resourceName:"namespaces/%s/%ss/%s" AND (protoPayload.methodName:"update" OR protoPayload.methodName:"patch" OR protoPayload.methodName:"create")`,
				escapeFilter(target.ClusterName), escapeFilter(target.Namespace), strings.ToLower(target.WorkloadKind), escapeFilter(target.ServiceName))
			value, err := e.source.QueryRecentLogs(ctx, models.QueryRecentLogsRequest{ProjectID: req.ProjectID, Filter: filter, MaxEntries: 50, LookbackMinutes: totalLookback})
			if err != nil {
				return "", err
			}
			mu.Lock()
			for _, entry := range value.Entries {
				if entry.Audit != nil && !entry.Audit.Succeeded {
					continue
				}
				d.revisions = append(d.revisions, models.RevisionSummary{
					Name: target.ServiceName + " rollout at " + entry.Timestamp, ServiceName: target.ServiceName,
					Region: target.Region, CreateTime: entry.Timestamp, Ready: true,
				})
			}
			mu.Unlock()
			return "Rollouts are read from Kubernetes Admin Activity audit logs.", nil
		}},
	}
	if req.IncludePlatformHealth {
		collectors = append(collectors, e.platformHealthCollector(req, &d, &mu))
	}
	runCollectors(ctx, &d, collectors)
	d.workload = state
	finishCollection(req, &d, "dependency topology is available for Cloud Run services and Gen 2 functions only")
	return d
}

// splitRestarts sums restart deltas for the workload's pods (named
// "<workload>-...") into the incident and baseline windows.
func splitRestarts(response models.GetMetricsResponse, workload string, currentStart time.Time) (float64, float64) {
	prefix := workload + "-"
	current, baseline := 0.0, 0.0
	for _, series := range response.Series {
		if !strings.HasPrefix(series.ResourceLabels["pod_name"], prefix) {
			continue
		}
		for _, point := range series.Points {
			ts, ok := parseTime(point.Timestamp)
			if !ok {
				continue
			}
			if ts.Before(currentStart) {
				baseline += point.Value
			} else {
				current += point.Value
			}
		}
	}
	return current, baseline
}

// eventReason extracts a Kubernetes event reason from a rendered event entry,
// falling back to common message wording.
func eventReason(message string) string {
	if match := eventReasonPattern.FindStringSubmatch(message); match != nil {
		return match[1]
	}
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "back-off restarting failed container"):
		return "BackOff"
	case strings.Contains(lower, "oom"):
		return "OOMKilling"
	case strings.Contains(lower, "probe failed"):
		return "Unhealthy"
	case strings.Contains(lower, "nodes are available") || strings.Contains(lower, "insufficient"):
		return "FailedScheduling"
	case strings.Contains(lower, "pull") && strings.Contains(lower, "image"):
		return "ImagePullBackOff"
	case strings.Contains(lower, "evict"):
		return "Evicted"
	default:
		return "Other"
	}
}

func escapeFilter(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}
