package resources

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/internal/environments"
	"github.com/asbrodova/aura-tracker-gcp/internal/testutil"
	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// captureService records the project ID and key argument of every resource
// call. When fail is set, every call returns it.
type captureService struct {
	mu    sync.Mutex
	calls []string
	fail  error
}

func (c *captureService) record(call string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, call)
	return c.fail
}

func (c *captureService) fake() *testutil.FakeGCPService {
	return &testutil.FakeGCPService{
		ListDatasetsFunc: func(_ context.Context, req models.ListDatasetsRequest) (models.ListDatasetsResponse, error) {
			return models.ListDatasetsResponse{}, c.record("ListDatasets " + req.ProjectID)
		},
		ListTablesFunc: func(_ context.Context, req models.ListTablesRequest) (models.ListTablesResponse, error) {
			return models.ListTablesResponse{}, c.record("ListTables " + req.ProjectID + " " + req.DatasetID)
		},
		GetTableSchemaFunc: func(_ context.Context, req models.GetTableSchemaRequest) (models.TableSchemaResponse, error) {
			return models.TableSchemaResponse{}, c.record("GetTableSchema " + req.ProjectID + " " + req.DatasetID + "." + req.TableID)
		},
		ListServicesFunc: func(_ context.Context, req models.ListServicesRequest) (models.ListServicesResponse, error) {
			return models.ListServicesResponse{}, c.record("ListServices " + req.ProjectID + " " + req.Region)
		},
		GetServiceDetailsFunc: func(_ context.Context, req models.GetServiceDetailsRequest) (models.ServiceDetails, error) {
			return models.ServiceDetails{ServiceSummary: models.ServiceSummary{Name: req.ServiceName}}, c.record("GetServiceDetails " + req.ProjectID + " " + req.Region + "/" + req.ServiceName)
		},
		ListRevisionsFunc: func(_ context.Context, req models.ListRevisionsRequest) (models.ListRevisionsResponse, error) {
			return models.ListRevisionsResponse{}, c.record("ListRevisions " + req.ProjectID + " " + req.Region + "/" + req.ServiceName)
		},
		ListBucketsFunc: func(_ context.Context, req models.ListBucketsRequest) (models.ListBucketsResponse, error) {
			return models.ListBucketsResponse{}, c.record("ListBuckets " + req.ProjectID)
		},
		GetBucketMetadataFunc: func(_ context.Context, req models.GetBucketMetadataRequest) (models.BucketMetadataResponse, error) {
			return models.BucketMetadataResponse{}, c.record("GetBucketMetadata " + req.ProjectID + " " + req.BucketName)
		},
		TestPermissionsFunc: func(_ context.Context, req models.TestPermissionsRequest) (models.TestPermissionsResponse, error) {
			if err := c.record("TestPermissions " + req.ProjectID + " " + strings.Join(req.Permissions, ",")); err != nil {
				return models.TestPermissionsResponse{}, err
			}
			results := make([]models.PermissionResult, 0, len(req.Permissions))
			for i, permission := range req.Permissions {
				results = append(results, models.PermissionResult{Permission: permission, Allowed: i%2 == 0})
			}
			return models.TestPermissionsResponse{ProjectID: req.ProjectID, Results: results}, nil
		},
	}
}

func (c *captureService) take() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	calls := c.calls
	c.calls = nil
	return calls
}

func testRegistry(t *testing.T) *environments.Registry {
	t.Helper()
	registry, err := environments.NewRegistry([]environments.Environment{
		{ProjectID: "private-dev-123", Alias: "dev", Default: true},
		{ProjectID: "private-prod-345", Alias: "prod"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

type resourceSet struct {
	bq  *BigQueryResources
	cr  *CloudRunResources
	gcs *StorageResources
	iam *IAMResources
}

func newResourceSet(svc *testutil.FakeGCPService, registry *environments.Registry, placeholder string) resourceSet {
	log := slog.New(slog.DiscardHandler)
	return resourceSet{
		bq:  NewBigQueryResources(svc, log, registry, placeholder),
		cr:  NewCloudRunResources(svc, log, registry, placeholder),
		gcs: NewStorageResources(svc, log, registry, placeholder),
		iam: NewIAMResources(svc, log, registry, placeholder, []string{"run.services.list", "storage.buckets.list"}),
	}
}

func (s resourceSet) templates() []server.ServerResourceTemplate {
	return []server.ServerResourceTemplate{
		s.bq.TableListTemplate(), s.bq.TableSchemaTemplate(),
		s.cr.ServiceSnapshotTemplate(), s.cr.RevisionsTemplate(),
		s.gcs.BucketMetadataTemplate(), s.gcs.ObjectListTemplate(),
	}
}

func readTemplate(t *testing.T, templates []server.ServerResourceTemplate, name, uri string) (string, error) {
	t.Helper()
	for _, template := range templates {
		if template.Template.Name != name {
			continue
		}
		contents, err := template.Handler(context.Background(), mcp.ReadResourceRequest{Params: mcp.ReadResourceParams{URI: uri}})
		if err != nil {
			return "", err
		}
		return singleText(t, contents, uri), nil
	}
	t.Fatalf("no template named %q", name)
	return "", nil
}

func singleText(t *testing.T, contents []mcp.ResourceContents, uri string) string {
	t.Helper()
	if len(contents) != 1 {
		t.Fatalf("%s: %d contents, want 1", uri, len(contents))
	}
	text, ok := contents[0].(mcp.TextResourceContents)
	if !ok || text.URI != uri || text.MIMEType != "application/json" || !json.Valid([]byte(text.Text)) {
		t.Fatalf("%s: unexpected contents %#v", uri, contents[0])
	}
	return text.Text
}

func TestResourceTemplatesResolveAliasesBeforeCallingService(t *testing.T) {
	capture := &captureService{}
	templates := newResourceSet(capture.fake(), testRegistry(t), "").templates()
	for _, tc := range []struct {
		name, uri string
		want      []string
	}{
		{"BigQuery Tables", "gcp://prod/bigquery/sales/tables", []string{"ListTables private-prod-345 sales"}},
		{"BigQuery Table Schema", "gcp://PROD/bigquery/sales/orders/schema", []string{"GetTableSchema private-prod-345 sales.orders"}},
		{"Cloud Run Service Snapshot", "gcp://dev/cloudrun/us-central1/api", []string{"GetServiceDetails private-dev-123 us-central1/api"}},
		{"Cloud Run Service Revisions", "gcp://prod/cloudrun/europe-west1/api/revisions", []string{
			"GetServiceDetails private-prod-345 europe-west1/api",
			"ListRevisions private-prod-345 europe-west1/api",
		}},
		{"Cloud Storage Bucket Metadata", "gcp://private-prod-345/storage/assets", []string{"GetBucketMetadata private-prod-345 assets"}},
		{"Cloud Storage Objects", "gcp://dev/storage/assets/objects", []string{"GetBucketMetadata private-dev-123 assets"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := readTemplate(t, templates, tc.name, tc.uri); err != nil {
				t.Fatalf("read %s: %v", tc.uri, err)
			}
			if got := capture.take(); !slices.Equal(got, tc.want) {
				t.Fatalf("service calls = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResourceTemplatesRejectUnknownEnvironmentsAndMalformedURIs(t *testing.T) {
	capture := &captureService{}
	templates := newResourceSet(capture.fake(), testRegistry(t), "").templates()
	for _, tc := range []struct {
		name, uri, want string
	}{
		{"BigQuery Tables", "gcp://staging/bigquery/sales/tables", "available environments: dev, prod"},
		{"BigQuery Tables", "gcp://dev/bigquery/sales", "invalid BigQuery tables URI"},
		{"BigQuery Table Schema", "gcp://dev/bigquery/sales/orders", "invalid BigQuery schema URI"},
		{"Cloud Run Service Snapshot", "gcp://dev/run/us-central1/api", "invalid Cloud Run service URI"},
		{"Cloud Run Service Revisions", "gcp://staging/cloudrun/us-central1/api/revisions", "available environments"},
		{"Cloud Storage Bucket Metadata", "gcp://dev/gcs/assets", "invalid Cloud Storage bucket URI"},
		{"Cloud Storage Objects", "gcp://staging/storage/assets/objects", "available environments"},
	} {
		_, err := readTemplate(t, templates, tc.name, tc.uri)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %s: error = %v, want %q", tc.name, tc.uri, err, tc.want)
		}
	}
	if calls := capture.take(); len(calls) != 0 {
		t.Fatalf("service called for rejected URIs: %v", calls)
	}
}

func TestResourcePlaceholderResolvesToDefaultEnvironment(t *testing.T) {
	capture := &captureService{}
	templates := newResourceSet(capture.fake(), testRegistry(t), "your-project").templates()
	if _, err := readTemplate(t, templates, "BigQuery Tables", "gcp://your-project/bigquery/sales/tables"); err != nil {
		t.Fatal(err)
	}
	if got := capture.take(); !slices.Equal(got, []string{"ListTables private-dev-123 sales"}) {
		t.Fatalf("service calls = %v", got)
	}
}

func TestResourceTemplatesWithoutRegistryPassSelectorThrough(t *testing.T) {
	capture := &captureService{}
	templates := newResourceSet(capture.fake(), nil, "").templates()
	if _, err := readTemplate(t, templates, "Cloud Storage Bucket Metadata", "gcp://plain-project/storage/assets"); err != nil {
		t.Fatal(err)
	}
	if got := capture.take(); !slices.Equal(got, []string{"GetBucketMetadata plain-project assets"}) {
		t.Fatalf("service calls = %v", got)
	}
}

func TestResourceServiceErrorsAreWrapped(t *testing.T) {
	cause := errors.New("backend unavailable")
	capture := &captureService{fail: cause}
	set := newResourceSet(capture.fake(), testRegistry(t), "")
	for _, tc := range []struct {
		name, uri, prefix string
	}{
		{"BigQuery Tables", "gcp://dev/bigquery/sales/tables", "bigquery tables"},
		{"BigQuery Table Schema", "gcp://dev/bigquery/sales/orders/schema", "bigquery schema"},
		{"Cloud Run Service Snapshot", "gcp://dev/cloudrun/us-central1/api", "cloudrun service"},
		{"Cloud Run Service Revisions", "gcp://dev/cloudrun/us-central1/api/revisions", "cloudrun revisions"},
		{"Cloud Storage Bucket Metadata", "gcp://dev/storage/assets", "storage bucket"},
		{"Cloud Storage Objects", "gcp://dev/storage/assets/objects", "storage objects"},
	} {
		_, err := readTemplate(t, set.templates(), tc.name, tc.uri)
		if !errors.Is(err, cause) || !strings.HasPrefix(err.Error(), tc.prefix+":") {
			t.Errorf("%s: error = %v, want %q wrapping the cause", tc.name, err, tc.prefix)
		}
	}
	dev := testRegistry(t).Default()
	for _, resource := range []server.ServerResource{set.bq.DatasetList(dev), set.cr.ServiceList(dev), set.gcs.BucketList(dev), set.iam.MyPermissions(dev)} {
		if _, err := resource.Handler(context.Background(), mcp.ReadResourceRequest{}); !errors.Is(err, cause) {
			t.Errorf("%s: error = %v, want the cause", resource.Resource.URI, err)
		}
	}
}

func TestStaticResourcesUseDisplayNameInURIAndProjectIDInCalls(t *testing.T) {
	capture := &captureService{}
	set := newResourceSet(capture.fake(), testRegistry(t), "")
	prod, err := testRegistry(t).Resolve("prod")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		resource server.ServerResource
		uri      string
		call     string
	}{
		{set.bq.DatasetList(prod), "gcp://prod/bigquery/datasets", "ListDatasets private-prod-345"},
		{set.cr.ServiceList(prod), "gcp://prod/cloudrun/services", "ListServices private-prod-345 -"},
		{set.gcs.BucketList(prod), "gcp://prod/storage/buckets", "ListBuckets private-prod-345"},
		{set.iam.MyPermissions(prod), "gcp://prod/iam/my-permissions", "TestPermissions private-prod-345 run.services.list,storage.buckets.list"},
	} {
		if tc.resource.Resource.URI != tc.uri {
			t.Errorf("resource URI = %q, want %q", tc.resource.Resource.URI, tc.uri)
		}
		contents, err := tc.resource.Handler(context.Background(), mcp.ReadResourceRequest{})
		if err != nil {
			t.Fatalf("%s: %v", tc.uri, err)
		}
		singleText(t, contents, tc.uri)
		if got := capture.take(); !slices.Equal(got, []string{tc.call}) {
			t.Errorf("%s: service calls = %v, want [%s]", tc.uri, got, tc.call)
		}
	}
}

func TestMyPermissionsSplitsGrantedAndDenied(t *testing.T) {
	capture := &captureService{}
	set := newResourceSet(capture.fake(), testRegistry(t), "")
	contents, err := set.iam.MyPermissions(testRegistry(t).Default()).Handler(context.Background(), mcp.ReadResourceRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var report models.MyPermissionsReport
	if err := json.Unmarshal([]byte(singleText(t, contents, "gcp://dev/iam/my-permissions")), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Granted) != 1 || report.Granted[0].Permission != "run.services.list" ||
		len(report.Denied) != 1 || report.Denied[0].Permission != "storage.buckets.list" {
		t.Fatalf("report = %+v", report)
	}
}

func TestBuildPermissionsReportUsesEmptySlices(t *testing.T) {
	report := buildPermissionsReport("p", nil)
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"granted":[]`) || !strings.Contains(string(data), `"denied":[]`) {
		t.Fatalf("empty report should encode empty arrays, got %s", data)
	}
}
