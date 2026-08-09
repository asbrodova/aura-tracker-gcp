package tools

import (
	"context"
	"log/slog"
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

type fakeCloudRunMutationService struct{}

func (fakeCloudRunMutationService) ListServices(context.Context, models.ListServicesRequest) (models.ListServicesResponse, error) {
	return models.ListServicesResponse{}, nil
}
func (fakeCloudRunMutationService) GetServiceDetails(context.Context, models.GetServiceDetailsRequest) (models.ServiceDetails, error) {
	return models.ServiceDetails{}, nil
}
func (fakeCloudRunMutationService) ListRevisions(context.Context, models.ListRevisionsRequest) (models.ListRevisionsResponse, error) {
	return models.ListRevisionsResponse{}, nil
}
func (fakeCloudRunMutationService) UpdateTraffic(context.Context, models.UpdateTrafficRequest) (models.UpdateTrafficResponse, error) {
	return models.UpdateTrafficResponse{}, nil
}
func (fakeCloudRunMutationService) ListJobs(context.Context, models.ListJobsRequest) (models.ListJobsResponse, error) {
	return models.ListJobsResponse{}, nil
}
func (fakeCloudRunMutationService) GetJobDetails(context.Context, models.GetJobDetailsRequest) (models.JobDetails, error) {
	return models.JobDetails{}, nil
}
func (fakeCloudRunMutationService) ListJobExecutions(context.Context, models.ListJobExecutionsRequest) (models.ListJobExecutionsResponse, error) {
	return models.ListJobExecutionsResponse{}, nil
}

func TestCloudRunTrafficMutationSchemaExposesTargets(t *testing.T) {
	tool := NewCloudRunTools(fakeCloudRunMutationService{}, slog.Default()).UpdateTraffic().Tool
	raw, ok := tool.InputSchema.Properties["traffic"]
	if !ok {
		t.Fatal("traffic property is missing")
	}
	property, ok := raw.(map[string]any)
	if !ok || property["type"] != "array" {
		t.Fatalf("traffic property = %#v", raw)
	}
	items, ok := property["items"].(map[string]any)
	if !ok || items["type"] != "object" {
		t.Fatalf("traffic items = %#v", property["items"])
	}
	properties, ok := items["properties"].(map[string]any)
	if !ok {
		t.Fatalf("traffic item properties = %#v", items["properties"])
	}
	for _, name := range []string{"revision", "percent", "tag"} {
		if _, exists := properties[name]; !exists {
			t.Errorf("traffic item schema missing %q", name)
		}
	}
}

type fakeRecommendationExportService struct{}

func (fakeRecommendationExportService) ExportRecommendationsToBQ(context.Context, models.ExportRecommendationsToBQRequest) (models.ExportRecommendationsToBQResponse, error) {
	return models.ExportRecommendationsToBQResponse{}, nil
}

func TestRecommendationExportSchemaRequiresConfirmationFlow(t *testing.T) {
	tool := NewRecommenderExportTools(fakeRecommendationExportService{}, slog.Default()).ExportRecommendationsToBQ().Tool
	for _, property := range []string{"project_id", "dataset", "table", "dry_run", "confirm_plan_id"} {
		if _, exists := tool.InputSchema.Properties[property]; !exists {
			t.Errorf("schema missing %q", property)
		}
	}
}
