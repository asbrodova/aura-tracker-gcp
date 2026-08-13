package drift

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

type fakeCollector struct {
	components []string
	results    map[string]CollectionResult
	errors     map[string]error
}

func (f *fakeCollector) SupportedComponents() []string { return append([]string(nil), f.components...) }
func (f *fakeCollector) Collect(_ context.Context, req CollectionRequest) (CollectionResult, error) {
	key := req.ProjectID + "/" + req.Component
	return f.results[key], f.errors[key]
}

func TestCompareUsesExactEnvironmentNamesAndIsSymmetric(t *testing.T) {
	collector := &fakeCollector{components: []string{"cloudrun"}, results: map[string]CollectionResult{
		"dev-project/cloudrun": {Resources: []Resource{
			{Component: "cloudrun", ResourceType: "cloudrun.service", Name: "api", Config: map[string]any{"scaling": map[string]any{"min_instances": 0}}},
			{Component: "cloudrun", ResourceType: "cloudrun.service", Name: "worker", Config: map[string]any{"image": "dev-project/worker:v1"}},
		}},
		"prod-project/cloudrun": {Resources: []Resource{
			{Component: "cloudrun", ResourceType: "cloudrun.service", Name: "api", Config: map[string]any{"scaling": map[string]any{"min_instances": 3}}},
		}},
	}}
	engine := New(collector, nil, WithClock(func() time.Time { return time.Unix(1_700_000_000, 0) }))
	response, err := engine.Compare(context.Background(), models.CompareEnvironmentsRequest{EnvironmentA: "dev-project", EnvironmentB: "prod-project"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Result != "differences_found" || response.Summary.DifferentResources != 1 || response.Summary.OnlyInEnvironmentA != 1 {
		t.Fatalf("summary = %+v result=%s", response.Summary, response.Result)
	}
	var worker models.ResourceDrift
	for _, value := range response.Resources {
		if value.Name == "worker" {
			worker = value
		}
	}
	if worker.MissingIn != "prod-project" || worker.PresentIn != "dev-project" || !strings.Contains(worker.Summary, "missing in prod-project") {
		t.Fatalf("worker result = %+v", worker)
	}
	api := response.Highlights[0]
	if len(api.FieldDifferences) != 1 || api.FieldDifferences[0].Values[0].Environment != "dev-project" || api.FieldDifferences[0].Values[1].Environment != "prod-project" {
		t.Fatalf("api difference = %+v", api)
	}
}

func TestCompareReportsPlacementAndQualifierDrift(t *testing.T) {
	collector := &fakeCollector{components: []string{"gke_workloads"}, results: map[string]CollectionResult{
		"dev/gke_workloads": {Resources: []Resource{{
			Component: "gke_workloads", ResourceType: "gke.workload.Deployment", Name: "api",
			Location: "us-central1", Qualifier: "cluster-a/prod/Deployment", Config: map[string]any{"replicas": 3},
		}}},
		"prod/gke_workloads": {Resources: []Resource{{
			Component: "gke_workloads", ResourceType: "gke.workload.Deployment", Name: "api",
			Location: "europe-west1", Qualifier: "cluster-b/prod/Deployment", Config: map[string]any{"replicas": 3},
		}}},
	}}
	response, err := New(collector, nil).Compare(context.Background(), models.CompareEnvironmentsRequest{
		EnvironmentA: "dev", EnvironmentB: "prod",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Result != "differences_found" || len(response.Resources) != 1 || response.Resources[0].Status != "different" {
		t.Fatalf("response = %+v", response)
	}
	resource := response.Resources[0]
	if resource.Location != "us-central1 ↔ europe-west1" || resource.Qualifier != "cluster-a/prod/Deployment ↔ cluster-b/prod/Deployment" {
		t.Fatalf("placement summary = %+v", resource)
	}
	if len(resource.FieldDifferences) != 2 || resource.FieldDifferences[0].Category != "placement" || resource.FieldDifferences[1].Category != "placement" {
		t.Fatalf("placement differences = %+v", resource.FieldDifferences)
	}
	paths := resource.FieldDifferences[0].Path + " " + resource.FieldDifferences[1].Path
	if !strings.Contains(paths, "/location") || !strings.Contains(paths, "/qualifier") {
		t.Fatalf("placement paths = %s", paths)
	}
}

type blockingCollector struct {
	release <-chan struct{}
	done    chan<- struct{}
}

func (b *blockingCollector) SupportedComponents() []string { return []string{"cloudrun"} }

func (b *blockingCollector) Collect(context.Context, CollectionRequest) (CollectionResult, error) {
	<-b.release
	b.done <- struct{}{}
	return CollectionResult{}, nil
}

func TestCompareReturnsAtDeadlineWhenCollectorIgnoresCancellation(t *testing.T) {
	release := make(chan struct{})
	done := make(chan struct{}, 2)
	engine := New(&blockingCollector{release: release, done: done}, nil, WithTimeout(25*time.Millisecond))
	started := time.Now()
	response, err := engine.Compare(context.Background(), models.CompareEnvironmentsRequest{EnvironmentA: "dev", EnvironmentB: "prod"})
	elapsed := time.Since(started)
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("comparison blocked for %s after its deadline", elapsed)
	}
	if response.CoverageStatus != "partial" || response.Result != "no_comparable_resources" || len(response.Warnings) == 0 {
		t.Fatalf("timeout response = %+v", response)
	}
	for range 2 {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("collector goroutine did not finish after release")
		}
	}
}

func TestCompareNormalizesProjectReferencesAndUnorderedArrays(t *testing.T) {
	collector := &fakeCollector{components: []string{"cloudrun"}, results: map[string]CollectionResult{
		"dev-project/cloudrun":  {Resources: []Resource{{Component: "cloudrun", ResourceType: "cloudrun.service", Name: "api", Config: map[string]any{"service_account": "runner@dev-project.iam.gserviceaccount.com", "tags": []any{"blue", "green"}, "update_time": "yesterday"}}}},
		"prod-project/cloudrun": {Resources: []Resource{{Component: "cloudrun", ResourceType: "cloudrun.service", Name: "api", Config: map[string]any{"service_account": "runner@prod-project.iam.gserviceaccount.com", "tags": []any{"green", "blue"}, "update_time": "today"}}}},
	}}
	response, err := New(collector, nil).Compare(context.Background(), models.CompareEnvironmentsRequest{EnvironmentA: "dev-project", EnvironmentB: "prod-project", IncludeUnchanged: true})
	if err != nil {
		t.Fatal(err)
	}
	if response.Result != "parity" || len(response.Resources) != 1 || response.Resources[0].Status != "equivalent" {
		t.Fatalf("response = %+v", response)
	}
}

func TestPartialCoverageDoesNotClaimResourceIsMissing(t *testing.T) {
	collector := &fakeCollector{components: []string{"gke"}, results: map[string]CollectionResult{
		"dev/gke":  {Resources: []Resource{{Component: "gke", ResourceType: "gke.cluster", Name: "platform", Config: map[string]any{"network": "main"}}}},
		"prod/gke": {Partial: true, Warnings: []string{"one location could not be listed"}},
	}}
	response, err := New(collector, nil).Compare(context.Background(), models.CompareEnvironmentsRequest{EnvironmentA: "dev", EnvironmentB: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if response.CoverageStatus != "partial" || response.Summary.UnknownDueToCoverage != 1 || response.Summary.OnlyInEnvironmentA != 0 {
		t.Fatalf("response = %+v", response)
	}
	if response.Resources[0].Status != "unknown_due_to_coverage" || response.Resources[0].MissingIn != "" {
		t.Fatalf("resource = %+v", response.Resources[0])
	}
}

func TestCollectionFailureProducesPartialNoComparableResult(t *testing.T) {
	collector := &fakeCollector{
		components: []string{"gke"}, results: map[string]CollectionResult{"dev/gke": {}},
		errors: map[string]error{"prod/gke": errors.New("permission denied")},
	}
	response, err := New(collector, nil).Compare(context.Background(), models.CompareEnvironmentsRequest{EnvironmentA: "dev", EnvironmentB: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Result != "no_comparable_resources" || response.CoverageStatus != "partial" || len(response.Warnings) == 0 {
		t.Fatalf("response = %+v", response)
	}
}

func TestPartialCoverageWithComparableResourcesIsNotReportedAsParity(t *testing.T) {
	resource := Resource{Component: "iam", ResourceType: "iam.service_account", Name: "runner", Config: map[string]any{"disabled": false}}
	collector := &fakeCollector{components: []string{"iam"}, results: map[string]CollectionResult{
		"dev/iam":  {Resources: []Resource{resource}, Partial: true, Warnings: []string{"bindings not covered"}},
		"prod/iam": {Resources: []Resource{resource}, Partial: true, Warnings: []string{"bindings not covered"}},
	}}
	response, err := New(collector, nil).Compare(context.Background(), models.CompareEnvironmentsRequest{EnvironmentA: "dev", EnvironmentB: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Result != "no_differences_observed" || response.CoverageStatus != "partial" || response.Summary.EquivalentResources != 1 {
		t.Fatalf("response = %+v", response)
	}
}

func TestCompareRejectsSameAndUnknownComponents(t *testing.T) {
	engine := New(&fakeCollector{components: []string{"gke"}}, nil)
	if _, err := engine.Compare(context.Background(), models.CompareEnvironmentsRequest{EnvironmentA: "dev", EnvironmentB: "dev"}); err == nil {
		t.Fatal("same environment was accepted")
	}
	if _, err := engine.Compare(context.Background(), models.CompareEnvironmentsRequest{EnvironmentA: "dev", EnvironmentB: "prod", Components: []string{"unknown"}}); err == nil {
		t.Fatal("unknown component was accepted")
	}
}

func TestSummaryTrimsHighlightFieldDetails(t *testing.T) {
	collector := &fakeCollector{components: []string{"cloudrun"}, results: map[string]CollectionResult{
		"dev/cloudrun":  {Resources: []Resource{{Component: "cloudrun", ResourceType: "cloudrun.service", Name: "api", Config: map[string]any{"one": 1, "two": 2, "three": 3, "four": 4}}}},
		"prod/cloudrun": {Resources: []Resource{{Component: "cloudrun", ResourceType: "cloudrun.service", Name: "api", Config: map[string]any{"one": 10, "two": 20, "three": 30, "four": 40}}}},
	}}
	response, err := New(collector, nil).Compare(context.Background(), models.CompareEnvironmentsRequest{
		EnvironmentA: "dev", EnvironmentB: "prod", DetailLevel: "summary",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Resources != nil || len(response.Highlights) != 1 || len(response.Highlights[0].FieldDifferences) != 3 {
		t.Fatalf("summary response = %+v", response)
	}
}

func TestSensitiveConfigurationValuesAreComparedButNeverReturned(t *testing.T) {
	collector := &fakeCollector{components: []string{"gke_workloads"}, results: map[string]CollectionResult{
		"dev/gke_workloads": {Resources: []Resource{{
			Component: "gke_workloads", ResourceType: "gke.workload.Deployment", Name: "api",
			Config: map[string]any{"containers": []any{map[string]any{
				"env_vars": []any{map[string]any{"name": "API_TOKEN", "value": "alpha-secret"}},
			}}},
		}}},
		"prod/gke_workloads": {Resources: []Resource{{
			Component: "gke_workloads", ResourceType: "gke.workload.Deployment", Name: "api",
			Config: map[string]any{"containers": []any{map[string]any{
				"env_vars": []any{map[string]any{"name": "API_TOKEN", "value": "omega-secret"}},
			}}},
		}}},
	}}
	response, err := New(collector, nil).Compare(context.Background(), models.CompareEnvironmentsRequest{
		EnvironmentA: "dev", EnvironmentB: "prod", DetailLevel: "detailed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Summary.FieldDifferences != 1 {
		t.Fatalf("summary = %+v", response.Summary)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	output := string(encoded)
	if strings.Contains(output, "alpha-secret") || strings.Contains(output, "omega-secret") || !strings.Contains(output, "[REDACTED]") {
		t.Fatalf("sensitive response = %s", output)
	}
}

func TestSensitiveKeyVariantsAndContainersAreRedacted(t *testing.T) {
	paths := []string{
		"/client_secret", "/access-token", "/apiKey", "/authorization", "/passwordHash",
		"/credentials/value", "/auth/privateKey", "/annotations/example.com~1innocuous",
		"/connection.string", "/nested/refresh_token", "/nested/cookie",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			value := map[string]any{"nested": []any{"drift-secret"}}
			if got := safeDifferenceValue(path, value, true); got != "[REDACTED]" {
				t.Fatalf("safeDifferenceValue(%q) = %#v, want redaction", path, got)
			}
		})
	}
}

func TestSafeDriftURLPreservesRoutingAndRemovesCredentials(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		want      string
		forbidden []string
	}{
		{
			name:      "absolute URL",
			raw:       "https://alice:p%40ss@example.com:8443/path?token=query-secret&mode=full#fragment-secret",
			want:      "https://example.com:8443/path?mode=%5BREDACTED%5D&token=%5BREDACTED%5D",
			forbidden: []string{"alice", "p%40ss", "query-secret", "fragment-secret"},
		},
		{
			name:      "signed URL",
			raw:       "https://storage.googleapis.com/bucket/object?X-Goog-Credential=credential-secret&X-Goog-Signature=signature-secret",
			want:      "https://storage.googleapis.com/bucket/object?X-Goog-Credential=%5BREDACTED%5D&X-Goog-Signature=%5BREDACTED%5D",
			forbidden: []string{"credential-secret", "signature-secret"},
		},
		{
			name:      "relative target",
			raw:       "/tasks/run?code=oauth-secret#fragment-secret",
			want:      "/tasks/run?code=%5BREDACTED%5D",
			forbidden: []string{"oauth-secret", "fragment-secret"},
		},
		{
			name:      "artifact reference",
			raw:       "us-docker.pkg.dev/project/repository/image?tag=sensitive-tag",
			want:      "us-docker.pkg.dev/project/repository/image?tag=%5BREDACTED%5D",
			forbidden: []string{"sensitive-tag"},
		},
		{
			name:      "opaque DSN",
			raw:       "Server=db;User ID=alice;Password=dsn-secret",
			want:      "[REDACTED]",
			forbidden: []string{"alice", "dsn-secret"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := safeDriftURL(tt.raw)
			if got != tt.want {
				t.Fatalf("safeDriftURL() = %q, want %q", got, tt.want)
			}
			for _, forbidden := range tt.forbidden {
				if strings.Contains(got, forbidden) {
					t.Fatalf("credential %q leaked in %q", forbidden, got)
				}
			}
			if safeDriftURL(got) != got && got != "[REDACTED]" {
				t.Fatalf("URL sanitizer is not idempotent: first=%q second=%q", got, safeDriftURL(got))
			}
		})
	}
}

func TestDriftResponseSanitizesValuesPathsAndDiagnostics(t *testing.T) {
	collector := &fakeCollector{components: []string{"scheduler"}, results: map[string]CollectionResult{
		"env-a/scheduler": {
			Resources: []Resource{{
				Component: "scheduler", ResourceType: "scheduler.job", Name: "nightly", Config: map[string]any{
					"target_ref":  "https://left-user:left-password@example.com/task?token=left-query-secret#left-fragment-secret",
					"annotations": map[string]any{"example.com/owner": "annotation-left-secret"},
					"clients":     []any{map[string]any{"name": "semantic-left-secret", "enabled": true}},
				},
			}},
			Partial: true,
			Warnings: []string{
				"request https://warning-user:warning-password@example.com/path?signature=warning-query-secret#warning-fragment-secret failed; Authorization: Bearer warning-bearer-secret",
			},
		},
		"env-b/scheduler": {
			Resources: []Resource{{
				Component: "scheduler", ResourceType: "scheduler.job", Name: "nightly", Config: map[string]any{
					"target_ref":  "https://right-user:right-password@example.com/task?token=right-query-secret#right-fragment-secret",
					"annotations": map[string]any{"example.com/owner": "annotation-right-secret"},
					"clients":     []any{map[string]any{"name": "semantic-right-secret", "enabled": false}},
				},
			}},
		},
	}}
	response, err := New(collector, nil).Compare(context.Background(), models.CompareEnvironmentsRequest{
		EnvironmentA: "env-a", EnvironmentB: "env-b", DetailLevel: "detailed",
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	output := string(encoded)
	for _, forbidden := range []string{
		"left-user", "left-password", "left-query-secret", "left-fragment-secret",
		"right-user", "right-password", "right-query-secret", "right-fragment-secret",
		"annotation-left-secret", "annotation-right-secret",
		"semantic-left-secret", "semantic-right-secret",
		"warning-user", "warning-password", "warning-query-secret", "warning-fragment-secret", "warning-bearer-secret",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("sensitive sentinel %q leaked: %s", forbidden, output)
		}
	}
	if !strings.Contains(output, "example.com/task") || !strings.Contains(output, "[REDACTED]") {
		t.Fatalf("safe diagnostic structure or redaction marker missing: %s", output)
	}
	if response.Summary.FieldDifferences == 0 || response.CoverageStatus != "partial" {
		t.Fatalf("comparison semantics lost: %+v", response)
	}
}

func TestDriftCollectorErrorDiagnosticsAreSanitized(t *testing.T) {
	collector := &fakeCollector{components: []string{"cloudrun"}, results: map[string]CollectionResult{
		"dev/cloudrun":  {Resources: []Resource{}},
		"prod/cloudrun": {Resources: []Resource{}},
	}, errors: map[string]error{
		"dev/cloudrun": errors.New("GET https://error-user:error-password@example.com/path?api_key=error-query-secret#fragment Authorization: Bearer error-bearer-secret"),
	}}
	response, err := New(collector, nil).Compare(context.Background(), models.CompareEnvironmentsRequest{EnvironmentA: "dev", EnvironmentB: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(response)
	for _, forbidden := range []string{"error-user", "error-password", "error-query-secret", "error-bearer-secret", "#fragment"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("collector error leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestArrayComparisonMatchesNamedObjectsAndSetMembers(t *testing.T) {
	collector := &fakeCollector{components: []string{"gke"}, results: map[string]CollectionResult{
		"dev/gke": {Resources: []Resource{{
			Component: "gke", ResourceType: "gke.cluster", Name: "platform",
			Config: map[string]any{
				"node_pools": []any{map[string]any{"name": "primary", "max_nodes": 3}},
				"tags":       []any{"blue", "green"},
			},
		}}},
		"prod/gke": {Resources: []Resource{{
			Component: "gke", ResourceType: "gke.cluster", Name: "platform",
			Config: map[string]any{
				"node_pools": []any{map[string]any{"name": "primary", "max_nodes": 10}},
				"tags":       []any{"green"},
			},
		}}},
	}}
	response, err := New(collector, nil).Compare(context.Background(), models.CompareEnvironmentsRequest{EnvironmentA: "dev", EnvironmentB: "prod", DetailLevel: "detailed"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Summary.FieldDifferences != 2 {
		t.Fatalf("differences = %+v", response.Resources[0].FieldDifferences)
	}
	paths := []string{response.Resources[0].FieldDifferences[0].Path, response.Resources[0].FieldDifferences[1].Path}
	joined := strings.Join(paths, " ")
	if !strings.Contains(joined, "/node_pools/name=[IDENTITY]/max_nodes") || !strings.Contains(joined, "/tags/0") {
		t.Fatalf("paths = %#v", paths)
	}
	for _, difference := range response.Resources[0].FieldDifferences {
		if difference.Path == "/tags/0" && difference.ChangeType != "missing_in_prod" {
			t.Fatalf("tag difference = %+v", difference)
		}
	}
}
