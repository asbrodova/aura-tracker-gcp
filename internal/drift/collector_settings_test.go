package drift

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/internal/testutil"
	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func compareComponentWith(t *testing.T, fake *testutil.FakeGCPService, component string) models.CompareEnvironmentsResponse {
	t.Helper()
	response, err := New(NewGCPCollector(fake), nil).Compare(context.Background(), models.CompareEnvironmentsRequest{
		EnvironmentA: "dev-project", EnvironmentB: "prod-project", Components: []string{component},
	})
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func driftPaths(response models.CompareEnvironmentsResponse) string {
	var paths []string
	for _, resource := range response.Resources {
		for _, difference := range resource.FieldDifferences {
			paths = append(paths, resource.Name+":"+difference.Path)
		}
	}
	return strings.Join(paths, " ")
}

func TestTasksDriftComparesRateAndRetrySettings(t *testing.T) {
	fake := &testutil.FakeGCPService{
		ListTaskQueuesFunc: func(_ context.Context, req models.ListTaskQueuesRequest) (models.ListTaskQueuesResponse, error) {
			attempts := int32(100)
			if req.ProjectID == "prod-project" {
				attempts = 5
			}
			return models.ListTaskQueuesResponse{Queues: []models.TaskQueueSummary{{
				Name: "emails", Region: "us-central1", State: "RUNNING",
				RateLimits:  &models.TaskQueueRateLimits{MaxDispatchesPerSecond: 10, MaxConcurrentDispatches: 50},
				RetryConfig: &models.TaskQueueRetryConfig{MaxAttempts: attempts, MinBackoff: "100ms", MaxBackoff: "1h0m0s"},
			}}}, nil
		},
	}
	response := compareComponentWith(t, fake, "tasks")
	if !strings.Contains(driftPaths(response), "/retry_config/max_attempts") {
		t.Fatalf("retry drift not reported: %s", driftPaths(response))
	}
	for _, coverage := range response.Coverage {
		if coverage.Status == "partial" {
			t.Fatalf("tasks coverage is still partial: %+v", response.Coverage)
		}
	}
}

func TestCloudSQLDriftComparesAdvancedSettings(t *testing.T) {
	fake := &testutil.FakeGCPService{
		ListSQLInstancesFunc: func(_ context.Context, req models.ListSQLInstancesRequest) (models.ListSQLInstancesResponse, error) {
			availability, pitr := "REGIONAL", true
			if req.ProjectID == "prod-project" {
				availability, pitr = "ZONAL", false
			}
			return models.ListSQLInstancesResponse{Instances: []models.SQLInstanceSummary{{
				Name: "orders", Region: "us-central1", Tier: "db-custom-2-7680", DatabaseVersion: "POSTGRES_16",
				Settings: &models.SQLInstanceSettings{
					AvailabilityType: availability, DeletionProtection: true,
					Backup:        &models.SQLBackupSettings{Enabled: true, PointInTimeRecovery: pitr},
					DatabaseFlags: map[string]string{"max_connections": "200"},
				},
			}}}, nil
		},
	}
	paths := driftPaths(compareComponentWith(t, fake, "cloudsql"))
	for _, want := range []string{"/settings/availability_type", "/settings/backup/point_in_time_recovery"} {
		if !strings.Contains(paths, want) {
			t.Errorf("missing drift %s in %s", want, paths)
		}
	}
	if strings.Contains(paths, "database_flags") {
		t.Errorf("identical flags reported as drift: %s", paths)
	}
}

func TestWorkflowsDriftComparesSourceHashAndRequestsIt(t *testing.T) {
	// Both environments are collected concurrently, so count the calls
	// that asked for the source hash instead of sharing a plain bool.
	var requested atomic.Int32
	fake := &testutil.FakeGCPService{
		ListWorkflowsFunc: func(_ context.Context, req models.ListWorkflowsRequest) (models.ListWorkflowsResponse, error) {
			if req.RequireSourceHash {
				requested.Add(1)
			}
			hash := "aaaa"
			if req.ProjectID == "prod-project" {
				hash = "bbbb"
			}
			return models.ListWorkflowsResponse{Workflows: []models.WorkflowSummary{{
				Name: "nightly", Region: "us-central1", SourceSHA256: hash, CallLogLevel: "LOG_ERRORS_ONLY",
				ServiceAccount: "projects/" + req.ProjectID + "/serviceAccounts/wf@" + req.ProjectID + ".iam.gserviceaccount.com",
			}}}, nil
		},
	}
	paths := driftPaths(compareComponentWith(t, fake, "workflows"))
	if requested.Load() != 2 {
		t.Fatalf("%d of 2 workflow list calls asked for the source hash", requested.Load())
	}
	if !strings.Contains(paths, "/source_sha256") {
		t.Fatalf("source drift not reported: %s", paths)
	}
	if strings.Contains(paths, "service_account") {
		t.Fatalf("project-specific service account reported as drift: %s", paths)
	}
}

func TestWorkflowWithoutSourceHashIsPartial(t *testing.T) {
	fake := &testutil.FakeGCPService{
		ListWorkflowsFunc: func(context.Context, models.ListWorkflowsRequest) (models.ListWorkflowsResponse, error) {
			return models.ListWorkflowsResponse{Workflows: []models.WorkflowSummary{{Name: "nightly", Region: "us-central1"}}}, nil
		},
	}
	result, err := NewGCPCollector(fake).Collect(context.Background(), CollectionRequest{ProjectID: "project", Component: "workflows"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Partial || !strings.Contains(strings.Join(result.Warnings, " "), "source was not compared") {
		t.Fatalf("result = %+v", result)
	}
}

func TestIAMDriftComparesProjectBindings(t *testing.T) {
	fake := &testutil.FakeGCPService{
		GetResourceIAMBindingsFunc: func(_ context.Context, req models.GetResourceIAMBindingsRequest) (models.GetResourceIAMBindingsResponse, error) {
			if req.URN != "urn:gcp:project:global:"+req.ProjectID+":"+req.ProjectID {
				t.Errorf("unexpected URN %q", req.URN)
			}
			number := "111111111111"
			bindings := []models.IAMBinding{
				{Role: "roles/run.invoker", Members: []string{"serviceAccount:api@" + req.ProjectID + ".iam.gserviceaccount.com"}},
				{Role: "roles/editor", Members: []string{"serviceAccount:" + number + "-compute@developer.gserviceaccount.com"}},
			}
			if req.ProjectID == "prod-project" {
				number = "222222222222"
				bindings = []models.IAMBinding{
					{Role: "roles/run.invoker", Members: []string{"serviceAccount:api@prod-project.iam.gserviceaccount.com", "user:oncall@example.com"}},
					{Role: "roles/editor", Members: []string{"serviceAccount:" + number + "-compute@developer.gserviceaccount.com"}},
					{Role: "roles/storage.admin", Members: []string{"group:ops@example.com"},
						Condition: &models.IAMCondition{Title: "until-2027", Expression: "request.time < timestamp('2027-01-01T00:00:00Z')"}},
				}
			}
			return models.GetResourceIAMBindingsResponse{URN: req.URN, Bindings: bindings}, nil
		},
	}
	response := compareComponentWith(t, fake, "iam")
	var invoker, editor, conditional string
	for _, resource := range response.Resources {
		switch resource.Name {
		case "roles/run.invoker":
			invoker = resource.Status
		case "roles/editor":
			editor = resource.Status
		case "roles/storage.admin [condition: until-2027]":
			conditional = resource.Status
		}
	}
	if invoker == "" || invoker == "equivalent" {
		t.Errorf("roles/run.invoker status = %q, want a reported member difference", invoker)
	}
	if editor != "" && editor != "equivalent" {
		t.Errorf("the default compute account differs only by project number but was reported: %q", editor)
	}
	if conditional == "" {
		t.Errorf("conditional binding missing from %+v", response.Resources)
	}
	for _, coverage := range response.Coverage {
		if coverage.Status == "partial" {
			t.Fatalf("iam coverage is partial: %+v", response.Coverage)
		}
	}
}

func TestProjectBindingResourcesMergesAndNormalizes(t *testing.T) {
	resources := projectBindingResources([]models.IAMBinding{
		{Role: "roles/viewer", Members: []string{"serviceAccount:service-123456@gcp-sa-run.iam.gserviceaccount.com", "user:b@example.com"}},
		{Role: "roles/viewer", Members: []string{"user:a@example.com", "user:b@example.com"}},
	})
	if len(resources) != 1 {
		t.Fatalf("resources = %+v", resources)
	}
	members, _ := resources[0].Config["members"].([]any)
	got := make([]string, 0, len(members))
	for _, member := range members {
		got = append(got, member.(string))
	}
	want := "serviceAccount:service-${PROJECT_NUMBER}@gcp-sa-run.iam.gserviceaccount.com user:a@example.com user:b@example.com"
	if strings.Join(got, " ") != want {
		t.Fatalf("members = %v, want %s", got, want)
	}
}
