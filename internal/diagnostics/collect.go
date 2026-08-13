package diagnostics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

type serviceData struct {
	target       models.IncidentTarget
	details      models.ServiceDetails
	revisions    []models.RevisionSummary
	logs         []models.LogEntry
	auditLogs    []models.LogEntry
	platformLogs []models.LogEntry
	requests     models.GetMetricsResponse
	latency      models.GetMetricsResponse
	topology     models.ServiceTopologyReport
	topologyOK   bool
	dependencies []models.IncidentDependencyObservation
	coverage     []models.IncidentCoverageCheck
	warnings     []string
	symptoms     models.IncidentServiceSymptoms
}

type indexedServiceData struct {
	index int
	data  serviceData
}

type collectorResult struct {
	name    string
	status  string
	message string
	err     error
}

func (e *Engine) collectService(ctx context.Context, req models.DiagnoseIncidentRequest, target models.IncidentTarget, now time.Time) serviceData {
	d := serviceData{target: target, coverage: []models.IncidentCoverageCheck{}, warnings: []string{}}
	totalLookback := req.LookbackMinutes + req.BaselineMinutes
	results := make(chan collectorResult, 8)
	var mu sync.Mutex
	g, groupCtx := errgroup.WithContext(ctx)

	run := func(name string, fn func(context.Context) (string, error)) {
		g.Go(func() error {
			message, err := fn(groupCtx)
			status := "complete"
			if err != nil {
				status = "partial"
			}
			results <- collectorResult{name: name, status: status, message: message, err: err}
			return nil // collectors are deliberately failure-isolated
		})
	}

	run("service_details", func(ctx context.Context) (string, error) {
		value, err := e.source.GetServiceDetails(ctx, models.GetServiceDetailsRequest{
			ProjectID: req.ProjectID, Region: target.Region, ServiceName: target.ServiceName,
		})
		if err == nil {
			mu.Lock()
			d.details = value
			mu.Unlock()
		}
		return "", err
	})
	run("revision_history", func(ctx context.Context) (string, error) {
		value, err := e.source.ListRevisions(ctx, models.ListRevisionsRequest{
			ProjectID: req.ProjectID, Region: target.Region, ServiceName: target.ServiceName, Limit: 25,
		})
		if err == nil {
			mu.Lock()
			d.revisions = value.Revisions
			mu.Unlock()
		}
		return "", err
	})
	run("error_logs", func(ctx context.Context) (string, error) {
		value, err := e.source.QueryRecentLogs(ctx, models.QueryRecentLogsRequest{
			ProjectID: req.ProjectID, ResourceType: "cloud_run_revision",
			ResourceLabels: map[string]string{"service_name": target.ServiceName, "location": target.Region},
			MinSeverity:    "ERROR", MaxEntries: 200, LookbackMinutes: totalLookback,
		})
		if err == nil {
			mu.Lock()
			d.logs = value.Entries
			mu.Unlock()
			if value.Truncated {
				return "Log results reached the 200-entry cap.", nil
			}
		}
		return "", err
	})
	run("audit_changes", func(ctx context.Context) (string, error) {
		filter := fmt.Sprintf(`(logName:"cloudaudit.googleapis.com%%2Factivity") AND (resource.labels.service_name="%s" OR protoPayload.resourceName:"/services/%s" OR protoPayload.serviceName="iam.googleapis.com" OR protoPayload.serviceName="secretmanager.googleapis.com")`, target.ServiceName, target.ServiceName)
		value, err := e.source.QueryRecentLogs(ctx, models.QueryRecentLogsRequest{
			ProjectID: req.ProjectID, Filter: filter, MaxEntries: 100, LookbackMinutes: totalLookback,
		})
		if err == nil {
			mu.Lock()
			d.auditLogs = value.Entries
			mu.Unlock()
			if value.Truncated {
				return "Audit results reached the 100-entry cap.", nil
			}
		}
		return "", err
	})
	run("request_and_error_rate", func(ctx context.Context) (string, error) {
		value, err := e.source.GetMetrics(ctx, models.GetMetricsRequest{
			ProjectID: req.ProjectID, MetricType: "run.googleapis.com/request_count",
			ResourceLabels:  map[string]string{"service_name": target.ServiceName, "location": target.Region},
			LookbackMinutes: totalLookback, AlignmentPeriodSeconds: 60,
			PerSeriesAligner: "ALIGN_SUM", CrossSeriesReducer: "REDUCE_SUM",
			GroupByFields: []string{"resource.labels.revision_name", "metric.labels.response_code_class"}, MaxTimeSeries: 100,
		})
		if err == nil {
			mu.Lock()
			d.requests = value
			mu.Unlock()
			if value.Truncated {
				return "Request metric results reached the time-series cap.", nil
			}
		}
		return "", err
	})
	run("latency_p99", func(ctx context.Context) (string, error) {
		value, err := e.source.GetMetrics(ctx, models.GetMetricsRequest{
			ProjectID: req.ProjectID, MetricType: "run.googleapis.com/request_latencies",
			ResourceLabels:  map[string]string{"service_name": target.ServiceName, "location": target.Region},
			LookbackMinutes: totalLookback, AlignmentPeriodSeconds: 60,
			PerSeriesAligner: "ALIGN_PERCENTILE_99", CrossSeriesReducer: "REDUCE_PERCENTILE_99",
			GroupByFields: []string{"resource.labels.revision_name"}, MaxTimeSeries: 50,
		})
		if err == nil {
			mu.Lock()
			d.latency = value
			mu.Unlock()
		}
		return "", err
	})
	if req.MaxDependencies > 0 {
		run("dependency_topology", func(ctx context.Context) (string, error) {
			value, err := e.source.GetServiceTopology(ctx, models.GetServiceTopologyRequest{
				Project: req.ProjectID, Region: target.Region, ServiceName: target.ServiceName, Depth: 1,
			})
			if err == nil {
				mu.Lock()
				d.topology = value
				d.topologyOK = true
				mu.Unlock()
			}
			return "", err
		})
	}
	if req.IncludePlatformHealth {
		run("platform_health", func(ctx context.Context) (string, error) {
			value, err := e.source.QueryRecentLogs(ctx, models.QueryRecentLogsRequest{
				ProjectID:  req.ProjectID,
				Filter:     fmt.Sprintf(`logName="projects/%s/logs/servicehealth.googleapis.com%%2Factivity" AND resource.type="servicehealth.googleapis.com/Event" AND jsonPayload.category="INCIDENT" AND jsonPayload.relevance!="NOT_IMPACTED" AND jsonPayload.state="ACTIVE"`, req.ProjectID),
				MaxEntries: 50, LookbackMinutes: totalLookback,
			})
			if err == nil {
				mu.Lock()
				d.platformLogs = value.Entries
				mu.Unlock()
			}
			return "No platform-health log entries is not proof that the platform was healthy.", err
		})
	}

	_ = g.Wait()
	close(results)
	for result := range results {
		message := result.message
		if result.err != nil {
			message = result.err.Error()
			d.warnings = append(d.warnings, fmt.Sprintf("%s/%s: %v", target.ServiceName, result.name, result.err))
		}
		d.coverage = append(d.coverage, models.IncidentCoverageCheck{
			Name: result.name, ServiceName: target.ServiceName, Status: result.status, Message: message,
		})
	}
	if d.topologyOK {
		e.collectDependencyHealth(ctx, req, &d)
	} else {
		d.coverage = append(d.coverage, models.IncidentCoverageCheck{Name: "dependency_health", ServiceName: target.ServiceName, Status: "skipped", Message: "dependency topology was unavailable"})
	}
	if !req.IncludePlatformHealth {
		d.coverage = append(d.coverage, models.IncidentCoverageCheck{Name: "platform_health", ServiceName: target.ServiceName, Status: "skipped", Message: "include_platform_health is false"})
	}
	sort.SliceStable(d.coverage, func(i, j int) bool { return d.coverage[i].Name < d.coverage[j].Name })
	return d
}

func (e *Engine) collectDependencyHealth(ctx context.Context, req models.DiagnoseIncidentRequest, d *serviceData) {
	nodes := make([]models.TopologyNode, 0, len(d.topology.Nodes))
	for _, node := range d.topology.Nodes {
		if node.ID == d.topology.RootService || (node.Kind == "cloudrun_service" && node.Name == d.target.ServiceName) {
			continue
		}
		nodes = append(nodes, node)
	}
	if len(nodes) > req.MaxDependencies {
		nodes = nodes[:req.MaxDependencies]
		d.warnings = append(d.warnings, fmt.Sprintf("%s dependency checks reached max_dependencies", d.target.ServiceName))
	}
	if len(nodes) == 0 {
		d.coverage = append(d.coverage, models.IncidentCoverageCheck{Name: "dependency_health", ServiceName: d.target.ServiceName, Status: "complete", Message: "No downstream dependencies were discovered."})
		return
	}

	var sqlByName map[string]models.SQLInstanceSummary
	var vpcByName map[string]models.VPCConnectorSummary
	needSQL, needVPC := false, false
	for _, node := range nodes {
		needSQL = needSQL || node.Kind == "cloudsql_instance"
		needVPC = needVPC || node.Kind == "vpc_connector"
	}
	checkStatus := "complete"
	var messages []string
	if needSQL {
		response, err := e.source.ListSQLInstances(ctx, models.ListSQLInstancesRequest{ProjectID: req.ProjectID})
		if err != nil {
			checkStatus = "partial"
			messages = append(messages, "Cloud SQL: "+err.Error())
		} else {
			sqlByName = make(map[string]models.SQLInstanceSummary, len(response.Instances))
			for _, instance := range response.Instances {
				sqlByName[baseName(instance.Name)] = instance
			}
		}
	}
	if needVPC {
		response, err := e.source.ListVPCConnectors(ctx, models.ListVPCConnectorsRequest{ProjectID: req.ProjectID, Region: d.target.Region})
		if err != nil {
			checkStatus = "partial"
			messages = append(messages, "VPC connector: "+err.Error())
		} else {
			vpcByName = make(map[string]models.VPCConnectorSummary, len(response.Connectors))
			for _, connector := range response.Connectors {
				vpcByName[baseName(connector.Name)] = connector
			}
		}
	}

	for _, node := range nodes {
		observation := models.IncidentDependencyObservation{
			ServiceName: d.target.ServiceName, Kind: node.Kind, Name: safeDependencyName(node),
			Region: node.Region, Status: "unknown", Evidence: topologyEvidence(d.topology, node.ID), Issues: []string{},
		}
		switch node.Kind {
		case "cloudsql_instance":
			if instance, ok := sqlByName[baseName(node.Name)]; ok {
				observation.Status = strings.ToLower(instance.State)
				if !strings.EqualFold(instance.State, "RUNNABLE") {
					observation.Issues = append(observation.Issues, "Cloud SQL state is "+instance.State)
				}
			} else if sqlByName != nil {
				observation.Issues = append(observation.Issues, "Dependency was not found in the Cloud SQL inventory")
			}
		case "vpc_connector":
			if connector, ok := vpcByName[baseName(node.Name)]; ok {
				observation.Status = strings.ToLower(connector.State)
				if !strings.EqualFold(connector.State, "READY") {
					observation.Issues = append(observation.Issues, "VPC connector state is "+connector.State)
				}
			} else if vpcByName != nil {
				observation.Issues = append(observation.Issues, "Dependency was not found in the VPC connector inventory")
			}
		case "pubsub_topic":
			status, issues, notes := e.inspectPubSubDependency(ctx, req, baseName(node.Name))
			observation.Status = status
			observation.Issues = append(observation.Issues, issues...)
			if len(notes) > 0 {
				checkStatus = "partial"
				messages = append(messages, notes...)
			}
		case "cloudrun_service":
			region := node.Region
			if region == "" {
				region = d.target.Region
			}
			metrics, err := e.source.GetMetrics(ctx, models.GetMetricsRequest{
				ProjectID: req.ProjectID, MetricType: "run.googleapis.com/request_count",
				ResourceLabels:  map[string]string{"service_name": baseName(node.Name), "location": region},
				LookbackMinutes: req.LookbackMinutes, AlignmentPeriodSeconds: 60,
				PerSeriesAligner: "ALIGN_SUM", CrossSeriesReducer: "REDUCE_SUM",
				GroupByFields: []string{"metric.labels.response_code_class"}, MaxTimeSeries: 10,
			})
			if err != nil {
				checkStatus = "partial"
				messages = append(messages, "Cloud Run "+node.Name+": "+err.Error())
			} else {
				window := splitRequestMetrics(metrics, e.now().UTC().Add(-time.Duration(req.LookbackMinutes)*time.Minute))
				errorRate := ratio(window.currentErrors, window.currentTotal)
				switch {
				case window.currentTotal == 0:
					observation.Status = "no_data"
					checkStatus = "partial"
					messages = append(messages, "Cloud Run "+node.Name+": no request metric data")
				case window.currentTotal >= 20 && errorRate >= 0.05:
					observation.Status = "degraded"
					observation.Issues = append(observation.Issues, fmt.Sprintf("Downstream Cloud Run 5xx rate is %.2f%%", errorRate*100))
				default:
					observation.Status = "healthy"
				}
			}
		}
		d.dependencies = append(d.dependencies, observation)
	}
	d.coverage = append(d.coverage, models.IncidentCoverageCheck{
		Name: "dependency_health", ServiceName: d.target.ServiceName, Status: checkStatus, Message: strings.Join(messages, "; "),
	})
}

func (e *Engine) inspectPubSubDependency(ctx context.Context, req models.DiagnoseIncidentRequest, topic string) (string, []string, []string) {
	health, err := e.source.InspectTopicHealth(ctx, models.InspectTopicHealthRequest{ProjectID: req.ProjectID, TopicName: topic})
	if err != nil {
		return "unknown", nil, []string{"Pub/Sub topic " + topic + ": " + err.Error()}
	}
	if !health.Exists {
		issues := health.Issues
		if len(issues) == 0 {
			issues = []string{"Pub/Sub topic does not exist"}
		}
		return "unhealthy", issues, nil
	}

	const maxSubscriptions = 2
	subscriptions := health.Subscriptions
	var notes []string
	if len(subscriptions) > maxSubscriptions {
		subscriptions = subscriptions[:maxSubscriptions]
		notes = append(notes, fmt.Sprintf("Pub/Sub topic %s has more than %d subscriptions; lag checks were capped", topic, maxSubscriptions))
	}
	issues := []string{}
	metricFailures := 0
	for _, subscription := range subscriptions {
		subscriptionID := baseName(subscription.SubscriptionName)
		backlog, backlogErr := e.dependencyMetricCurrent(ctx, req, "pubsub.googleapis.com/subscription/num_undelivered_messages", "subscription_id", subscriptionID)
		age, ageErr := e.dependencyMetricCurrent(ctx, req, "pubsub.googleapis.com/subscription/oldest_unacked_message_age", "subscription_id", subscriptionID)
		if backlogErr != nil {
			metricFailures++
			notes = append(notes, fmt.Sprintf("Pub/Sub backlog metric for %s: %v", subscriptionID, backlogErr))
		}
		if ageErr != nil {
			metricFailures++
			notes = append(notes, fmt.Sprintf("Pub/Sub oldest-unacked metric for %s: %v", subscriptionID, ageErr))
		}
		if backlog >= 10000 {
			issues = append(issues, fmt.Sprintf("Subscription %s has %.0f undelivered messages", subscriptionID, backlog))
		}
		if age >= 300 {
			issues = append(issues, fmt.Sprintf("Subscription %s oldest unacked message is %.0f seconds old", subscriptionID, age))
		}
	}
	if len(issues) > 0 {
		return "unhealthy", issues, notes
	}
	if metricFailures > 0 || len(health.Issues) > 0 {
		notes = append(notes, health.Issues...)
		return "unknown", nil, notes
	}
	return "healthy", nil, notes
}

// dependencyMetricCurrent returns the newest gauge sample inside the incident
// window. Pub/Sub backlog metrics describe current state; taking the maximum
// over the whole lookback incorrectly reports a recovered historical spike as
// an active dependency failure.
func (e *Engine) dependencyMetricCurrent(ctx context.Context, req models.DiagnoseIncidentRequest, metricType, label, value string) (float64, error) {
	end := e.now().UTC().Truncate(time.Second)
	start := end.Add(-time.Duration(req.LookbackMinutes) * time.Minute)
	response, err := e.source.GetMetrics(ctx, models.GetMetricsRequest{
		ProjectID: req.ProjectID, MetricType: metricType,
		StartTime: start.Format(time.RFC3339), EndTime: end.Format(time.RFC3339),
		ResourceLabels: map[string]string{label: value}, LookbackMinutes: req.LookbackMinutes,
		AlignmentPeriodSeconds: 60, PerSeriesAligner: "ALIGN_MAX", CrossSeriesReducer: "REDUCE_MAX", MaxTimeSeries: 5,
	})
	if err != nil {
		return 0, err
	}
	if response.NoData || len(response.Series) == 0 {
		return 0, fmt.Errorf("no metric data")
	}
	latestValue := 0.0
	latestTime := time.Time{}
	found := false
	for _, series := range response.Series {
		for _, point := range series.Points {
			pointTime, parseErr := time.Parse(time.RFC3339Nano, point.Timestamp)
			if parseErr != nil || pointTime.Before(start) || pointTime.After(end) {
				continue
			}
			if !found || pointTime.After(latestTime) || (pointTime.Equal(latestTime) && point.Value > latestValue) {
				latestTime = pointTime
				latestValue = point.Value
				found = true
			}
		}
	}
	if !found {
		return 0, fmt.Errorf("no metric data in the incident window")
	}
	return latestValue, nil
}

func topologyEvidence(report models.ServiceTopologyReport, nodeID string) string {
	for _, edge := range report.Edges {
		if edge.To == nodeID {
			return fmt.Sprintf("%s (%s confidence)", edge.Evidence, edge.Confidence)
		}
	}
	return "topology discovery"
}

func baseName(name string) string {
	parts := strings.Split(strings.TrimSuffix(name, "/"), "/")
	return parts[len(parts)-1]
}

func safeDependencyName(node models.TopologyNode) string {
	// Topology can infer an external database or cache from an environment
	// variable value. That value might be a connection URL or credential, so
	// incident output uses a stable opaque identifier for those node kinds.
	switch node.Kind {
	case "external_db", "redis_cache":
		sum := sha256.Sum256([]byte(node.Kind + "|" + node.Name))
		return node.Kind + "-" + hex.EncodeToString(sum[:4])
	default:
		return node.Name
	}
}

func finalizeCoverage(coverage *models.IncidentCoverage) {
	coverage.Complete, coverage.Partial, coverage.Skipped = 0, 0, 0
	for _, check := range coverage.Checks {
		switch check.Status {
		case "complete":
			coverage.Complete++
		case "skipped":
			coverage.Skipped++
		default:
			coverage.Partial++
		}
	}
}
