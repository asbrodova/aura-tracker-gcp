package diagnostics

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/internal/testutil"
	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func TestDiagnoseRanksDeploymentRegression(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	fake := &testutil.FakeGCPService{
		GetServiceDetailsFunc: func(context.Context, models.GetServiceDetailsRequest) (models.ServiceDetails, error) {
			return models.ServiceDetails{
				LatestRevision: "checkout-00002",
				Traffic:        []models.TrafficTarget{{Revision: "checkout-00002", Percent: 100}},
			}, nil
		},
		ListRevisionsFunc: func(context.Context, models.ListRevisionsRequest) (models.ListRevisionsResponse, error) {
			return models.ListRevisionsResponse{Revisions: []models.RevisionSummary{
				{Name: "checkout-00002", CreateTime: now.Add(-35 * time.Minute).Format(time.RFC3339), Ready: true, ConfigFingerprint: "new"},
				{Name: "checkout-00001", CreateTime: now.Add(-6 * time.Hour).Format(time.RFC3339), Ready: true, ConfigFingerprint: "old"},
			}}, nil
		},
		QueryRecentLogsFunc: func(_ context.Context, req models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error) {
			if req.MinSeverity == "ERROR" {
				return models.QueryRecentLogsResponse{Entries: []models.LogEntry{{
					Timestamp: now.Add(-30 * time.Minute).Format(time.RFC3339), Severity: "ERROR",
					Message: "panic: failed request 918273", ResourceLabels: map[string]string{"service_name": "checkout", "revision_name": "checkout-00002"},
				}}}, nil
			}
			return models.QueryRecentLogsResponse{Entries: []models.LogEntry{{
				Timestamp: now.Add(-36 * time.Minute).Format(time.RFC3339), Message: "Cloud Run revision deployment completed",
				Audit: &models.AuditLogDetails{Category: "deployment_change", MethodName: "google.cloud.run.v2.Services.UpdateService"},
			}}}, nil
		},
		GetMetricsFunc: func(_ context.Context, req models.GetMetricsRequest) (models.GetMetricsResponse, error) {
			if req.MetricType == "run.googleapis.com/request_latencies" {
				return models.GetMetricsResponse{MetricType: req.MetricType, Unit: "ms", Series: []models.MetricSeries{{Points: []models.MetricPoint{
					{Timestamp: now.Add(-30 * time.Minute).Format(time.RFC3339), Value: 100},
					{Timestamp: now.Add(-2 * time.Hour).Format(time.RFC3339), Value: 90},
				}}}}, nil
			}
			return models.GetMetricsResponse{MetricType: req.MetricType, Series: []models.MetricSeries{
				{MetricLabels: map[string]string{"response_code_class": "5xx"}, ResourceLabels: map[string]string{"revision_name": "checkout-00002"}, Points: []models.MetricPoint{
					{Timestamp: now.Add(-29 * time.Minute).Format(time.RFC3339), Value: 20},
					{Timestamp: now.Add(-2 * time.Hour).Format(time.RFC3339), Value: 1},
				}},
				{MetricLabels: map[string]string{"response_code_class": "2xx"}, ResourceLabels: map[string]string{"revision_name": "checkout-00002"}, Points: []models.MetricPoint{
					{Timestamp: now.Add(-29 * time.Minute).Format(time.RFC3339), Value: 80},
					{Timestamp: now.Add(-2 * time.Hour).Format(time.RFC3339), Value: 999},
				}},
			}}, nil
		},
	}

	response, err := New(fake, nil, WithClock(func() time.Time { return now })).Diagnose(context.Background(), models.DiagnoseIncidentRequest{
		ProjectID: "prod-project", ServiceName: "checkout", Region: "us-central1",
	})
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	if response.Status != "critical" {
		t.Fatalf("Status = %q, want critical", response.Status)
	}
	if len(response.PossibleRootCauses) == 0 {
		t.Fatal("expected at least one root cause")
	}
	leading := response.PossibleRootCauses[0]
	if leading.Code != "deployment_regression" {
		t.Fatalf("leading cause = %q, want deployment_regression", leading.Code)
	}
	if leading.Likelihood.Band != "high" || leading.Likelihood.Score < 75 {
		t.Fatalf("likelihood = %+v, want high", leading.Likelihood)
	}
	if len(response.Timeline) == 0 || response.Coverage.Complete == 0 {
		t.Fatalf("expected timeline and coverage, got timeline=%d coverage=%+v", len(response.Timeline), response.Coverage)
	}
}

func TestDiagnoseRequiresExplicitScopeWhenProductionCannotBeInferred(t *testing.T) {
	t.Parallel()
	fake := &testutil.FakeGCPService{
		ListServicesFunc: func(context.Context, models.ListServicesRequest) (models.ListServicesResponse, error) {
			return models.ListServicesResponse{Services: []models.ServiceSummary{
				{Name: "checkout", Region: "us-central1", Labels: map[string]string{"environment": "staging"}},
			}}, nil
		},
	}
	response, err := New(fake, nil).Diagnose(context.Background(), models.DiagnoseIncidentRequest{ProjectID: "project"})
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	if response.Status != "needs_scope" || len(response.Scope.Candidates) != 1 || len(response.Scope.Targets) != 0 {
		t.Fatalf("unexpected scope response: %+v", response)
	}
}

func TestDiagnoseKeepsPartialEvidenceWhenCollectorFails(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	fake := &testutil.FakeGCPService{
		GetServiceDetailsFunc: func(context.Context, models.GetServiceDetailsRequest) (models.ServiceDetails, error) {
			return models.ServiceDetails{}, errors.New("permission denied")
		},
		QueryRecentLogsFunc: func(_ context.Context, req models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error) {
			if req.MinSeverity == "ERROR" {
				return models.QueryRecentLogsResponse{Entries: []models.LogEntry{{Timestamp: now.Add(-5 * time.Minute).Format(time.RFC3339), Severity: "ERROR", Message: "database connection refused"}}}, nil
			}
			return models.QueryRecentLogsResponse{}, nil
		},
	}
	response, err := New(fake, nil, WithClock(func() time.Time { return now })).Diagnose(context.Background(), models.DiagnoseIncidentRequest{
		ProjectID: "project", ServiceName: "api", Region: "europe-west1",
	})
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	if response.Status != "degraded" || response.Coverage.Partial == 0 || len(response.PossibleRootCauses) == 0 {
		t.Fatalf("partial diagnosis was discarded: %+v", response)
	}
}

func TestDiagnoseCorrelatesUnhealthyDependency(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	fake := &testutil.FakeGCPService{
		GetServiceTopologyFunc: func(context.Context, models.GetServiceTopologyRequest) (models.ServiceTopologyReport, error) {
			return models.ServiceTopologyReport{
				RootService: "api",
				Nodes: []models.TopologyNode{
					{ID: "cloudrun:api", Kind: "cloudrun_service", Name: "api", Region: "us-central1"},
					{ID: "pubsub:orders", Kind: "pubsub_topic", Name: "orders"},
				},
				Edges: []models.TopologyEdge{{From: "cloudrun:api", To: "pubsub:orders", Evidence: "env_var:ORDERS_TOPIC", Confidence: "medium"}},
			}, nil
		},
		InspectTopicHealthFunc: func(context.Context, models.InspectTopicHealthRequest) (models.TopicHealthReport, error) {
			return models.TopicHealthReport{TopicName: "orders", Exists: true, Healthy: true, Subscriptions: []models.SubscriptionLag{{SubscriptionName: "projects/project/subscriptions/orders-worker"}}}, nil
		},
		GetMetricsFunc: func(_ context.Context, req models.GetMetricsRequest) (models.GetMetricsResponse, error) {
			if strings.Contains(req.MetricType, "num_undelivered_messages") {
				return models.GetMetricsResponse{Series: []models.MetricSeries{{Points: []models.MetricPoint{{Timestamp: now.Add(-time.Minute).Format(time.RFC3339), Value: 20000}}}}}, nil
			}
			if strings.Contains(req.MetricType, "oldest_unacked_message_age") {
				return models.GetMetricsResponse{Series: []models.MetricSeries{{Points: []models.MetricPoint{{Timestamp: now.Add(-time.Minute).Format(time.RFC3339), Value: 600}}}}}, nil
			}
			return models.GetMetricsResponse{}, nil
		},
		QueryRecentLogsFunc: func(_ context.Context, req models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error) {
			if req.MinSeverity == "ERROR" {
				return models.QueryRecentLogsResponse{Entries: []models.LogEntry{{
					Timestamp: now.Add(-10 * time.Minute).Format(time.RFC3339), Severity: "ERROR", Message: "pubsub publish deadline exceeded",
				}}}, nil
			}
			return models.QueryRecentLogsResponse{}, nil
		},
	}
	response, err := New(fake, nil, WithClock(func() time.Time { return now })).Diagnose(context.Background(), models.DiagnoseIncidentRequest{
		ProjectID: "project", ServiceName: "api", Region: "us-central1",
	})
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	if len(response.Dependencies) != 1 || response.Dependencies[0].Status != "unhealthy" {
		t.Fatalf("dependency observations = %+v", response.Dependencies)
	}
	if len(response.PossibleRootCauses) == 0 || response.PossibleRootCauses[0].Code != "dependency_failure" {
		t.Fatalf("root causes = %+v", response.PossibleRootCauses)
	}
	if response.PossibleRootCauses[0].Likelihood.Band != "high" {
		t.Fatalf("dependency likelihood = %+v", response.PossibleRootCauses[0].Likelihood)
	}
}

func TestDiagnoseOnlyRanksRelevantCurrentPlatformEvent(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	platformMessage := "Cloud Run incident affecting us-central1"
	fake := &testutil.FakeGCPService{
		QueryRecentLogsFunc: func(_ context.Context, req models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error) {
			switch {
			case req.MinSeverity == "ERROR":
				return models.QueryRecentLogsResponse{Entries: []models.LogEntry{{Timestamp: now.Add(-5 * time.Minute).Format(time.RFC3339), Severity: "ERROR", Message: "request failed"}}}, nil
			case req.Filter != "" && req.Filter != `(logName:"cloudaudit.googleapis.com%2Factivity")`:
				if contains(req.Filter, "servicehealth.googleapis.com") {
					return models.QueryRecentLogsResponse{Entries: []models.LogEntry{{Timestamp: now.Add(-8 * time.Minute).Format(time.RFC3339), Message: platformMessage}}}, nil
				}
			}
			return models.QueryRecentLogsResponse{}, nil
		},
	}
	response, err := New(fake, nil, WithClock(func() time.Time { return now })).Diagnose(context.Background(), models.DiagnoseIncidentRequest{
		ProjectID: "project", ServiceName: "api", Region: "us-central1", IncludePlatformHealth: true,
	})
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	if len(response.PossibleRootCauses) == 0 || response.PossibleRootCauses[0].Code != "platform_incident" {
		t.Fatalf("root causes = %+v", response.PossibleRootCauses)
	}
	if response.PossibleRootCauses[0].Likelihood.Score != 75 {
		t.Fatalf("platform likelihood = %+v", response.PossibleRootCauses[0].Likelihood)
	}
}

func TestDiagnoseUsesInconclusiveWhenCollectorsFailWithoutSymptoms(t *testing.T) {
	t.Parallel()
	fake := &testutil.FakeGCPService{
		GetServiceDetailsFunc: func(context.Context, models.GetServiceDetailsRequest) (models.ServiceDetails, error) {
			return models.ServiceDetails{}, errors.New("monitoring identity cannot read the service")
		},
	}
	response, err := New(fake, nil).Diagnose(context.Background(), models.DiagnoseIncidentRequest{
		ProjectID: "project", ServiceName: "api", Region: "us-central1",
	})
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	if response.Status != "inconclusive" || response.Coverage.Partial == 0 {
		t.Fatalf("status=%q coverage=%+v, want inconclusive with partial coverage", response.Status, response.Coverage)
	}
}

func TestApplyDetailLevelBoundsVerboseCollections(t *testing.T) {
	t.Parallel()
	response := models.DiagnoseIncidentResponse{
		PossibleRootCauses: make([]models.IncidentRootCause, 6),
		Timeline:           make([]models.IncidentTimelineEvent, 20),
		Symptoms:           []models.IncidentServiceSymptoms{{ErrorPatterns: make([]models.IncidentErrorPattern, 4)}},
	}
	for i := range response.PossibleRootCauses {
		response.PossibleRootCauses[i].Evidence = make([]models.IncidentEvidence, 6)
		response.PossibleRootCauses[i].ContradictingEvidence = make([]models.IncidentEvidence, 4)
		response.PossibleRootCauses[i].SuggestedInvestigation = make([]models.IncidentInvestigation, 4)
	}
	applyDetailLevel(&response, "summary")
	if len(response.PossibleRootCauses) != 3 || len(response.Timeline) != 15 || len(response.Symptoms[0].ErrorPatterns) != 1 {
		t.Fatalf("summary bounds not applied: causes=%d timeline=%d patterns=%d", len(response.PossibleRootCauses), len(response.Timeline), len(response.Symptoms[0].ErrorPatterns))
	}
	if len(response.PossibleRootCauses[0].Evidence) != 3 || len(response.PossibleRootCauses[0].ContradictingEvidence) != 2 || len(response.PossibleRootCauses[0].SuggestedInvestigation) != 2 {
		t.Fatalf("summary cause bounds not applied: %+v", response.PossibleRootCauses[0])
	}
}

func TestSafeDependencyNameRedactsConnectionValues(t *testing.T) {
	t.Parallel()
	node := models.TopologyNode{Kind: "external_db", Name: "postgres://user:secret@10.0.0.1/db"}
	first := safeDependencyName(node)
	second := safeDependencyName(node)
	if first != second || strings.Contains(first, "secret") || strings.Contains(first, "postgres") {
		t.Fatalf("unsafe or unstable dependency name %q", first)
	}
}

func TestDependencyCollectorErrorIsNotRankedAsDependencyFailure(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	fake := &testutil.FakeGCPService{
		GetServiceTopologyFunc: func(context.Context, models.GetServiceTopologyRequest) (models.ServiceTopologyReport, error) {
			return models.ServiceTopologyReport{
				RootService: "api",
				Nodes: []models.TopologyNode{
					{ID: "cloudrun:api", Kind: "cloudrun_service", Name: "api"},
					{ID: "pubsub:orders", Kind: "pubsub_topic", Name: "orders"},
				},
			}, nil
		},
		InspectTopicHealthFunc: func(context.Context, models.InspectTopicHealthRequest) (models.TopicHealthReport, error) {
			return models.TopicHealthReport{}, errors.New("permission denied")
		},
		QueryRecentLogsFunc: func(_ context.Context, req models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error) {
			if req.MinSeverity == "ERROR" {
				return models.QueryRecentLogsResponse{Entries: []models.LogEntry{{Timestamp: now.Add(-5 * time.Minute).Format(time.RFC3339), Severity: "ERROR", Message: "request handler failed"}}}, nil
			}
			return models.QueryRecentLogsResponse{}, nil
		},
	}
	response, err := New(fake, nil, WithClock(func() time.Time { return now })).Diagnose(context.Background(), models.DiagnoseIncidentRequest{
		ProjectID: "project", ServiceName: "api", Region: "us-central1",
	})
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	for _, cause := range response.PossibleRootCauses {
		if cause.Code == "dependency_failure" {
			t.Fatalf("collector permission error was treated as a dependency failure: %+v", cause)
		}
	}
	if response.Coverage.Partial == 0 {
		t.Fatal("dependency collector error was not reported as partial coverage")
	}
}

func contains(value, fragment string) bool {
	return len(value) >= len(fragment) && strings.Contains(value, fragment)
}
