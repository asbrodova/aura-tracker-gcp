package gcp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/option"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

type bigQueryRoundTripFunc func(*http.Request) (*http.Response, error)

func (f bigQueryRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func bigQueryJSONResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func newBigQueryTestAdapter(t *testing.T, transport bigQueryRoundTripFunc) *gcpAdapter {
	t.Helper()
	client, err := bigquery.NewClient(context.Background(), "startup-project", option.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return &gcpAdapter{bq: client, callTimeout: time.Second}
}

func TestListDatasetsSupportsOpaquePaginationAndTargetProject(t *testing.T) {
	t.Parallel()
	listRequests := 0
	adapter := newBigQueryTestAdapter(t, func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/bigquery/v2/projects/target-project/datasets":
			listRequests++
			if req.URL.Query().Get("maxResults") != "1" {
				t.Fatalf("maxResults = %q", req.URL.Query().Get("maxResults"))
			}
			switch req.URL.Query().Get("pageToken") {
			case "start":
				return bigQueryJSONResponse(req, http.StatusOK, `{"datasets":[{"datasetReference":{"projectId":"target-project","datasetId":"one"}}],"nextPageToken":"next"}`), nil
			case "next":
				return bigQueryJSONResponse(req, http.StatusOK, `{"datasets":[{"datasetReference":{"projectId":"target-project","datasetId":"two"}}]}`), nil
			default:
				t.Fatalf("unexpected page token %q", req.URL.Query().Get("pageToken"))
			}
		case "/bigquery/v2/projects/target-project/datasets/one":
			return bigQueryJSONResponse(req, http.StatusOK, `{"datasetReference":{"projectId":"target-project","datasetId":"one"},"location":"US"}`), nil
		case "/bigquery/v2/projects/target-project/datasets/two":
			return bigQueryJSONResponse(req, http.StatusOK, `{"datasetReference":{"projectId":"target-project","datasetId":"two"},"location":"EU"}`), nil
		default:
			t.Fatalf("unexpected BigQuery path %q", req.URL.Path)
		}
		return nil, nil
	})

	first, err := adapter.ListDatasets(context.Background(), models.ListDatasetsRequest{ProjectID: "target-project", PageSize: 1, PageToken: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Datasets) != 1 || first.Datasets[0].ID != "one" || !first.Truncated || first.NextPageToken != "next" {
		t.Fatalf("first page = %+v", first)
	}
	second, err := adapter.ListDatasets(context.Background(), models.ListDatasetsRequest{ProjectID: "target-project", PageSize: 1, PageToken: first.NextPageToken})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Datasets) != 1 || second.Datasets[0].ID != "two" || second.Truncated || second.NextPageToken != "" || listRequests != 2 {
		t.Fatalf("second page = %+v; list requests = %d", second, listRequests)
	}
}

func TestListTablesSupportsPaginationAndReportsMetadataFailures(t *testing.T) {
	t.Parallel()
	adapter := newBigQueryTestAdapter(t, func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/bigquery/v2/projects/target-project/datasets/analytics/tables":
			return bigQueryJSONResponse(req, http.StatusOK, `{"tables":[
				{"tableReference":{"projectId":"target-project","datasetId":"analytics","tableId":"orders"}},
				{"tableReference":{"projectId":"target-project","datasetId":"analytics","tableId":"restricted"}}
			],"nextPageToken":"more"}`), nil
		case "/bigquery/v2/projects/target-project/datasets/analytics/tables/orders":
			return bigQueryJSONResponse(req, http.StatusOK, `{"tableReference":{"projectId":"target-project","datasetId":"analytics","tableId":"orders"},"type":"TABLE","numRows":"12","numBytes":"1000000000","timePartitioning":{"field":"created_at"}}`), nil
		case "/bigquery/v2/projects/target-project/datasets/analytics/tables/restricted":
			return bigQueryJSONResponse(req, http.StatusForbidden, `{"error":{"code":403,"message":"permission bigquery.tables.get denied"}}`), nil
		default:
			t.Fatalf("unexpected BigQuery path %q", req.URL.Path)
			return nil, nil
		}
	})

	got, err := adapter.ListTables(context.Background(), models.ListTablesRequest{ProjectID: "target-project", DatasetID: "analytics", PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tables) != 1 || got.Tables[0].ID != "orders" || got.Tables[0].PartitionField != "created_at" || got.Tables[0].SizeGB != 1 {
		t.Fatalf("tables = %+v", got.Tables)
	}
	if !got.Truncated || got.NextPageToken != "more" || len(got.Errors) != 1 || len(got.Errors[0].MissingIAMPermissions) != 1 {
		t.Fatalf("partial page = %+v", got)
	}
}
