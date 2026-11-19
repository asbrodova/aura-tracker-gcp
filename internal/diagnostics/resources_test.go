package diagnostics

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/internal/testutil"
	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func causeCodes(response models.DiagnoseIncidentResponse) string {
	codes := []string{}
	for _, cause := range response.PossibleRootCauses {
		codes = append(codes, cause.Code)
	}
	return strings.Join(codes, ",")
}

func TestNormalizeResourceTypeValidatesScope(t *testing.T) {
	t.Parallel()
	for name, request := range map[string]models.DiagnoseIncidentRequest{
		"unknown type":           {ProjectID: "p", ResourceType: "vm"},
		"function without name":  {ProjectID: "p", ResourceType: "cloud_function"},
		"gke without cluster":    {ProjectID: "p", ResourceType: "gke_workload", ServiceName: "web", Region: "us-central1"},
		"gke without location":   {ProjectID: "p", ResourceType: "gke_workload", ServiceName: "web", ClusterName: "prod"},
		"gke unknown kind":       {ProjectID: "p", ResourceType: "gke_workload", ServiceName: "web", ClusterName: "prod", Region: "us-central1", WorkloadKind: "CronJob"},
		"cloud run with cluster": {ProjectID: "p", ServiceName: "api", ClusterName: "prod"},
	} {
		request := request
		if err := normalizeRequest(&request); err == nil {
			t.Errorf("%s: request was accepted", name)
		}
	}
	request := models.DiagnoseIncidentRequest{ProjectID: "p", ResourceType: "GKE_Workload", ServiceName: "web", ClusterName: "prod", Region: "us-central1", WorkloadKind: "statefulset"}
	if err := normalizeRequest(&request); err != nil {
		t.Fatal(err)
	}
	if request.ResourceType != resourceGKEWorkload || request.Namespace != "default" || request.WorkloadKind != "StatefulSet" {
		t.Fatalf("normalized = %+v", request)
	}
}

func TestDiagnoseGKEWorkloadWithUnschedulableAndCrashingPods(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	fake := &testutil.FakeGCPService{
		GetGKEWorkloadDetailsFunc: func(_ context.Context, req models.GetGKEWorkloadDetailsRequest) (models.GKEWorkloadDetails, error) {
			if req.ClusterName != "prod" || req.Namespace != "shop" || req.Name != "web" || req.Kind != "Deployment" || req.Location != "us-central1" {
				t.Errorf("unexpected workload request %+v", req)
			}
			var details models.GKEWorkloadDetails
			details.Replicas, details.ReadyReplicas = 3, 1
			return details, nil
		},
		QueryRecentLogsFunc: func(_ context.Context, req models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error) {
			switch {
			case strings.Contains(req.Filter, "/logs/events"):
				return models.QueryRecentLogsResponse{Entries: []models.LogEntry{
					{Timestamp: now.Add(-10 * time.Minute).Format(time.RFC3339), Severity: "WARNING", Message: `{"reason":"FailedScheduling","message":"0/3 nodes are available"}`},
					{Timestamp: now.Add(-9 * time.Minute).Format(time.RFC3339), Severity: "WARNING", Message: `{"reason":"BackOff","message":"Back-off restarting failed container"}`},
				}}, nil
			case strings.Contains(req.Filter, `resource.type="k8s_container"`):
				if !strings.Contains(req.Filter, `resource.labels.pod_name:"web-"`) {
					t.Errorf("container log filter is not scoped to the workload's pods: %s", req.Filter)
				}
				return models.QueryRecentLogsResponse{Entries: []models.LogEntry{
					{Timestamp: now.Add(-8 * time.Minute).Format(time.RFC3339), Severity: "ERROR", Message: "container web was OOMKilled"},
				}}, nil
			}
			return models.QueryRecentLogsResponse{}, nil
		},
		GetMetricsFunc: func(_ context.Context, req models.GetMetricsRequest) (models.GetMetricsResponse, error) {
			if req.MetricType != "kubernetes.io/container/restart_count" {
				t.Errorf("unexpected metric %s", req.MetricType)
			}
			point := func(minutesAgo int, value float64) models.MetricPoint {
				return models.MetricPoint{Timestamp: now.Add(-time.Duration(minutesAgo) * time.Minute).Format(time.RFC3339), Value: value}
			}
			return models.GetMetricsResponse{Series: []models.MetricSeries{
				{ResourceLabels: map[string]string{"pod_name": "web-7d9f-abcde"}, Points: []models.MetricPoint{point(20, 4), point(5, 4), point(120, 1)}},
				{ResourceLabels: map[string]string{"pod_name": "worker-55c-xyz"}, Points: []models.MetricPoint{point(5, 50)}},
			}}, nil
		},
	}
	response, err := New(fake, nil, WithClock(func() time.Time { return now })).Diagnose(context.Background(), models.DiagnoseIncidentRequest{
		ProjectID: "project", ResourceType: "gke_workload", ServiceName: "web", ClusterName: "prod", Region: "us-central1", Namespace: "shop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "degraded" && response.Status != "critical" {
		t.Fatalf("status = %q", response.Status)
	}
	codes := causeCodes(response)
	for _, want := range []string{"gke_workload_unavailable", "gke_container_crash_loop"} {
		if !strings.Contains(codes, want) {
			t.Errorf("causes %s are missing %s", codes, want)
		}
	}
	workload := response.ServiceSymptoms[0].Workload
	if workload == nil || workload.Restarts != 8 || workload.BaselineRestarts != 1 || workload.WarningEvents["FailedScheduling"] != 1 {
		t.Fatalf("workload symptoms = %+v (the other workload's restarts must not count)", workload)
	}
	for _, cause := range response.PossibleRootCauses {
		if cause.Code == "gke_container_crash_loop" && !strings.Contains(cause.Title, "OOM") {
			t.Errorf("OOM evidence did not shape the title: %q", cause.Title)
		}
	}
}

func TestDiagnoseGen1FunctionUsesExecutionMetrics(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	ts := func(minutesAgo int) string {
		return now.Add(-time.Duration(minutesAgo) * time.Minute).Format(time.RFC3339)
	}
	fake := &testutil.FakeGCPService{
		ListFunctionsFunc: func(context.Context, models.ListFunctionsRequest) (models.ListFunctionsResponse, error) {
			return models.ListFunctionsResponse{Functions: []models.FunctionSummary{
				{Name: "projects/p/locations/us-central1/functions/resize", Region: "us-central1", Generation: 1},
				{Name: "projects/p/locations/us-central1/functions/other", Region: "us-central1", Generation: 1},
			}}, nil
		},
		GetFunctionDetailsFunc: func(_ context.Context, req models.GetFunctionDetailsRequest) (models.FunctionDetails, error) {
			var details models.FunctionDetails
			details.Name, details.Region, details.Generation, details.Status, details.LastModified = req.FunctionName, req.Region, 1, "ACTIVE", ts(30)
			return details, nil
		},
		GetMetricsFunc: func(_ context.Context, req models.GetMetricsRequest) (models.GetMetricsResponse, error) {
			if req.MetricType != "cloudfunctions.googleapis.com/function/execution_count" {
				return models.GetMetricsResponse{}, nil
			}
			if req.ResourceLabels["function_name"] != "resize" {
				t.Errorf("metrics not scoped to the function: %+v", req.ResourceLabels)
			}
			return models.GetMetricsResponse{Series: []models.MetricSeries{
				{MetricLabels: map[string]string{"status": "ok"}, Points: []models.MetricPoint{{Timestamp: ts(10), Value: 70}, {Timestamp: ts(120), Value: 400}}},
				{MetricLabels: map[string]string{"status": "crash"}, Points: []models.MetricPoint{{Timestamp: ts(10), Value: 30}}},
			}}, nil
		},
		QueryRecentLogsFunc: func(_ context.Context, req models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error) {
			if req.ResourceType == "cloud_function" {
				return models.QueryRecentLogsResponse{Entries: []models.LogEntry{{Timestamp: ts(9), Severity: "ERROR", Message: "TypeError: cannot read property 'width' of undefined"}}}, nil
			}
			return models.QueryRecentLogsResponse{}, nil
		},
	}
	response, err := New(fake, nil, WithClock(func() time.Time { return now })).Diagnose(context.Background(), models.DiagnoseIncidentRequest{
		ProjectID: "project", ResourceType: "cloud_function", ServiceName: "resize",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Scope.Targets) != 1 || response.Scope.Targets[0].Region != "us-central1" || response.Scope.Targets[0].Generation != 1 {
		t.Fatalf("scope = %+v", response.Scope)
	}
	if rate := response.ServiceSymptoms[0].ErrorRate.Current; rate < 0.29 || rate > 0.31 {
		t.Fatalf("error rate = %v, want 0.3 (30 crashed of 100 executions)", rate)
	}
	codes := causeCodes(response)
	if !strings.Contains(codes, "deployment_regression") {
		t.Fatalf("a deployment 30 minutes ago was not correlated: %s", codes)
	}
	for _, cause := range response.PossibleRootCauses {
		if cause.Code == "deployment_regression" && cause.Title != "Recent Cloud Functions deployment regression" {
			t.Errorf("title = %q", cause.Title)
		}
	}
}

func TestDiagnoseGen2FunctionUsesItsCloudRunService(t *testing.T) {
	t.Parallel()
	var detailsFor string
	fake := &testutil.FakeGCPService{
		ListFunctionsFunc: func(context.Context, models.ListFunctionsRequest) (models.ListFunctionsResponse, error) {
			return models.ListFunctionsResponse{Functions: []models.FunctionSummary{{Name: "thumbnails", Region: "europe-west1", Generation: 2}}}, nil
		},
		GetFunctionDetailsFunc: func(context.Context, models.GetFunctionDetailsRequest) (models.FunctionDetails, error) {
			return models.FunctionDetails{CloudRunService: "projects/p/locations/europe-west1/services/thumbnails-svc"}, nil
		},
		GetServiceDetailsFunc: func(_ context.Context, req models.GetServiceDetailsRequest) (models.ServiceDetails, error) {
			detailsFor = req.ServiceName
			return models.ServiceDetails{}, nil
		},
	}
	response, err := New(fake, nil).Diagnose(context.Background(), models.DiagnoseIncidentRequest{
		ProjectID: "project", ResourceType: "cloud_function", ServiceName: "thumbnails",
	})
	if err != nil {
		t.Fatal(err)
	}
	if detailsFor != "thumbnails-svc" {
		t.Fatalf("Cloud Run collectors read service %q, want thumbnails-svc", detailsFor)
	}
	target := response.Scope.Targets[0]
	if target.FunctionName != "thumbnails" || target.ServiceName != "thumbnails-svc" || target.Generation != 2 {
		t.Fatalf("target = %+v", target)
	}
}

func TestEventReasonFallsBackToMessageText(t *testing.T) {
	t.Parallel()
	for message, want := range map[string]string{
		`{"reason":"Unhealthy","message":"Readiness probe failed"}`: "Unhealthy",
		"Back-off restarting failed container web":                  "BackOff",
		"Memory cgroup out of memory: Killed process":               "OOMKilling",
		"0/3 nodes are available: 3 Insufficient cpu":               "FailedScheduling",
		"Failed to pull image gcr.io/x/web:bad":                     "ImagePullBackOff",
		"something else":                                            "Other",
	} {
		if got := eventReason(message); got != want {
			t.Errorf("eventReason(%q) = %q, want %q", message, got, want)
		}
	}
}
