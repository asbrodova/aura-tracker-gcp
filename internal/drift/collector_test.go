package drift

import (
	"context"
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/internal/testutil"
	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func TestCloudRunCollectorIgnoresGeneratedLatestRevisionIdentity(t *testing.T) {
	fake := &testutil.FakeGCPService{
		ListServicesFunc: func(_ context.Context, req models.ListServicesRequest) (models.ListServicesResponse, error) {
			return models.ListServicesResponse{Services: []models.ServiceSummary{{Name: "api", Region: "us-central1"}}}, nil
		},
		GetServiceDetailsFunc: func(_ context.Context, req models.GetServiceDetailsRequest) (models.ServiceDetails, error) {
			revision := "api-00001-old"
			if req.ProjectID == "prod-project" {
				revision = "api-00042-new"
			}
			return models.ServiceDetails{
				ServiceSummary: models.ServiceSummary{Name: "api", Region: "us-central1"},
				LatestRevision: revision,
				Traffic:        []models.TrafficTarget{{Revision: revision, Percent: 100}},
			}, nil
		},
		ListRevisionsFunc: func(_ context.Context, req models.ListRevisionsRequest) (models.ListRevisionsResponse, error) {
			revision := "api-00001-old"
			if req.ProjectID == "prod-project" {
				revision = "api-00042-new"
			}
			return models.ListRevisionsResponse{Revisions: []models.RevisionSummary{{
				Name: revision, ServiceName: "api", Region: "us-central1", Ready: true,
				ConfigFingerprint: "sha256:same-safe-configuration",
				Containers:        []models.RevisionContainer{{Image: "us-docker.pkg.dev/shared/api:v1"}},
			}}}, nil
		},
	}
	engine := New(NewGCPCollector(fake), nil)
	response, err := engine.Compare(context.Background(), models.CompareEnvironmentsRequest{
		EnvironmentA: "dev-project", EnvironmentB: "prod-project", Components: []string{"cloudrun"}, IncludeUnchanged: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Result != "parity" || len(response.Resources) != 1 || response.Resources[0].Status != "equivalent" {
		t.Fatalf("response = %+v", response)
	}
}

func TestBigQueryResourceFilterCanSelectTableWithoutDatasetName(t *testing.T) {
	fake := &testutil.FakeGCPService{
		ListDatasetsFunc: func(context.Context, models.ListDatasetsRequest) (models.ListDatasetsResponse, error) {
			return models.ListDatasetsResponse{Datasets: []models.DatasetSummary{{ID: "analytics", Location: "US"}}}, nil
		},
		ListTablesFunc: func(context.Context, models.ListTablesRequest) (models.ListTablesResponse, error) {
			return models.ListTablesResponse{Tables: []models.TableSummary{{ID: "orders", Type: "TABLE"}, {ID: "customers", Type: "TABLE"}}}, nil
		},
		GetTableSchemaFunc: func(_ context.Context, req models.GetTableSchemaRequest) (models.TableSchemaResponse, error) {
			return models.TableSchemaResponse{DatasetID: req.DatasetID, TableID: req.TableID}, nil
		},
	}
	result, err := NewGCPCollector(fake).Collect(context.Background(), CollectionRequest{
		ProjectID: "project", Component: "bigquery", ResourceNames: []string{"orders"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Resources) != 1 || result.Resources[0].ResourceType != "bigquery.table" || result.Resources[0].Name != "orders" {
		t.Fatalf("resources = %+v", result.Resources)
	}
}
