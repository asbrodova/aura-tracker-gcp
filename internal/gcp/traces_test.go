package gcp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/api/cloudtrace/v1"
	"google.golang.org/api/option"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

type traceRoundTripFunc func(*http.Request) (*http.Response, error)

func (f traceRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTraceTestAdapter(t *testing.T, roundTrip traceRoundTripFunc) *gcpAdapter {
	t.Helper()
	svc, err := cloudtrace.NewService(context.Background(), option.WithHTTPClient(&http.Client{Transport: roundTrip}))
	if err != nil {
		t.Fatal(err)
	}
	return &gcpAdapter{
		traceClient:  svc,
		traceBackend: "trace",
		limiter:      rate.NewLimiter(rate.Inf, 1),
		callTimeout:  time.Second,
	}
}

func jsonHTTPResponse(req *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestListTraceDependencyEdgesUsesCompleteViewTargetProjectAndContinuation(t *testing.T) {
	var requests []*http.Request
	adapter := newTraceTestAdapter(t, func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.Clone(req.Context()))
		switch len(requests) {
		case 1:
			return jsonHTTPResponse(req, `{
				"traces":[{"projectId":"target-project","traceId":"a","spans":[
					{"spanId":"1","name":"/caller","labels":{"g.co/agent":"caller"}},
					{"spanId":"2","parentSpanId":"1","name":"/callee","labels":{"g.co/agent":"callee"}}
				]}],
				"nextPageToken":"next-1"
			}`), nil
		case 2:
			return jsonHTTPResponse(req, `{
				"traces":[{"projectId":"target-project","traceId":"b","spans":[
					{"spanId":"3","name":"/caller","labels":{"g.co/agent":"caller"}},
					{"spanId":"4","parentSpanId":"3","name":"/callee","labels":{"g.co/agent":"callee"}}
				]}]
			}`), nil
		default:
			t.Fatalf("unexpected trace request %d", len(requests))
			return nil, nil
		}
	})

	got, err := adapter.ListTraceDependencyEdges(context.Background(), models.ListTraceDependencyEdgesRequest{
		ProjectID: "target-project",
		PageSize:  37,
		PageToken: "start-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(requests))
	}
	assertTraceListRequest(t, requests[0].URL, "target-project", "COMPLETE", "37", "start-token")
	assertTraceListRequest(t, requests[1].URL, "target-project", "COMPLETE", "37", "next-1")
	if got.Truncated || got.NextPageToken != "" {
		t.Fatalf("complete response reported continuation: %+v", got)
	}
	if got.TracesScanned != 2 {
		t.Fatalf("traces scanned = %d, want 2", got.TracesScanned)
	}
	if len(got.Edges) != 1 || got.Edges[0].Caller != "caller" || got.Edges[0].Callee != "callee" || got.Edges[0].SampleCount != 2 {
		t.Fatalf("edges = %+v", got.Edges)
	}
}

func TestListTraceDependencyEdgesSignalsScanCap(t *testing.T) {
	requests := 0
	adapter := newTraceTestAdapter(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return jsonHTTPResponse(req, fmt.Sprintf(`{"nextPageToken":"next-%d"}`, requests)), nil
	})

	got, err := adapter.ListTraceDependencyEdges(context.Background(), models.ListTraceDependencyEdgesRequest{ProjectID: "target-project"})
	if err != nil {
		t.Fatal(err)
	}
	if requests != maxTraceDependencyPages {
		t.Fatalf("request count = %d, want %d", requests, maxTraceDependencyPages)
	}
	if !got.Truncated || got.NextPageToken != fmt.Sprintf("next-%d", maxTraceDependencyPages) {
		t.Fatalf("continuation = (%q, %v)", got.NextPageToken, got.Truncated)
	}
}

func TestListTraceServicesUsesRootSpanViewAndTargetProject(t *testing.T) {
	var requestURL *url.URL
	adapter := newTraceTestAdapter(t, func(req *http.Request) (*http.Response, error) {
		copyURL := *req.URL
		requestURL = &copyURL
		return jsonHTTPResponse(req, `{
			"traces":[{"projectId":"target-project","traceId":"a","spans":[
				{"spanId":"1","name":"/ignored","labels":{"g.co/r/cloud_run_revision/service_name":"checkout"}}
			]}],
			"nextPageToken":"more"
		}`), nil
	})

	got, err := adapter.ListTraceServices(context.Background(), models.ListTraceServicesRequest{
		ProjectID: "target-project",
		PageSize:  25,
		PageToken: "start",
	})
	if err != nil {
		t.Fatal(err)
	}
	if requestURL == nil {
		t.Fatal("trace API was not called")
	}
	assertTraceListRequest(t, requestURL, "target-project", "ROOTSPAN", "25", "start")
	if len(got.Services) != 1 || got.Services[0].Name != "checkout" {
		t.Fatalf("services = %+v", got.Services)
	}
	if !got.Truncated || got.NextPageToken != "more" {
		t.Fatalf("continuation = (%q, %v)", got.NextPageToken, got.Truncated)
	}
}

func assertTraceListRequest(t *testing.T, requestURL *url.URL, projectID, view, pageSize, pageToken string) {
	t.Helper()
	wantPath := "/v1/projects/" + projectID + "/traces"
	if requestURL.EscapedPath() != wantPath {
		t.Fatalf("request path = %q, want %q", requestURL.EscapedPath(), wantPath)
	}
	query := requestURL.Query()
	if query.Get("view") != view {
		t.Fatalf("view = %q, want %q", query.Get("view"), view)
	}
	if query.Get("pageSize") != pageSize {
		t.Fatalf("pageSize = %q, want %q", query.Get("pageSize"), pageSize)
	}
	if query.Get("pageToken") != pageToken {
		t.Fatalf("pageToken = %q, want %q", query.Get("pageToken"), pageToken)
	}
}
