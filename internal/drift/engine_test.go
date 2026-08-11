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
	if !strings.Contains(joined, "/node_pools/name=primary/max_nodes") || !strings.Contains(joined, "/tags/0") {
		t.Fatalf("paths = %#v", paths)
	}
	for _, difference := range response.Resources[0].FieldDifferences {
		if difference.Path == "/tags/0" && difference.ChangeType != "missing_in_prod" {
			t.Fatalf("tag difference = %+v", difference)
		}
	}
}
