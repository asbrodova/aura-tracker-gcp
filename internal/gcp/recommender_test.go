package gcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	recommender "cloud.google.com/go/recommender/apiv1"
	"cloud.google.com/go/recommender/apiv1/recommenderpb"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

type recommenderRoundTripFunc func(*http.Request) (*http.Response, error)

func (f recommenderRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func recommenderJSONResponse(req *http.Request, body string) *http.Response {
	return recommenderHTTPResponse(req, http.StatusOK, nil, body)
}

func recommenderHTTPResponse(req *http.Request, statusCode int, headers http.Header, body string) *http.Response {
	if headers == nil {
		headers = make(http.Header)
	}
	if headers.Get("Content-Type") == "" {
		headers.Set("Content-Type", "application/json")
	}
	return &http.Response{
		StatusCode: statusCode,
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestRecommenderRESTQuotaUsesDetailsAndAutomaticallyRecovers(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	requests := 0
	client := newRecommenderTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return recommenderHTTPResponse(req, http.StatusTooManyRequests, nil, `{
				"error":{"code":429,"message":"quota exhausted","status":"RESOURCE_EXHAUSTED","details":[
					{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"QUOTA_EXCEEDED","domain":"googleapis.com","metadata":{"quota_limit":"ListRecommendationsPerDayPerOrganization"}},
					{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"17s"}
				]}
			}`), nil
		}
		return recommenderJSONResponse(req, `{}`), nil
	})
	adapter := &gcpAdapter{rec: client, recommenderCache: newTTLCache[[]recommenderInsight](time.Hour)}
	adapter.recommenderQuota.now = func() time.Time { return now }

	_, err := adapter.fetchRecommenderInsights(context.Background(), "target-project", "us-central1", recommenderIDCloudSQLIdle, "/instances/db")
	var quotaErr *ports.RecommenderQuotaExhaustedError
	if !errors.As(err, &quotaErr) {
		t.Fatalf("error = %v, want typed quota error", err)
	}
	if quotaErr.Window != recommenderQuotaWindowDaily || !quotaErr.RetryAt.Equal(now.Add(17*time.Second)) {
		t.Fatalf("quota error = %+v", quotaErr)
	}

	// A cache miss during the block must not reach the transport.
	_, err = adapter.fetchRecommenderInsights(context.Background(), "target-project", "us-central1", recommenderIDCloudSQLIdle, "/instances/other")
	if !errors.As(err, &quotaErr) || requests != 1 {
		t.Fatalf("blocked miss: err=%v requests=%d", err, requests)
	}

	now = quotaErr.RetryAt
	got, err := adapter.fetchRecommenderInsights(context.Background(), "target-project", "us-central1", recommenderIDCloudSQLIdle, "/instances/other")
	if err != nil || len(got) != 0 || requests != 2 {
		t.Fatalf("after reset: got=%v err=%v requests=%d", got, err, requests)
	}
}

func TestFetchRecommenderInsightsServesCachedEmptyResultWhileBlocked(t *testing.T) {
	requests := 0
	client := newRecommenderTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return recommenderJSONResponse(req, `{}`), nil
	})
	adapter := &gcpAdapter{rec: client, recommenderCache: newTTLCache[[]recommenderInsight](time.Hour)}
	args := []string{"target-project", "us-central1", recommenderIDCloudSQLIdle, "/instances/db"}
	if got, err := adapter.fetchRecommenderInsights(context.Background(), args[0], args[1], args[2], args[3]); err != nil || len(got) != 0 {
		t.Fatalf("initial result=%v err=%v", got, err)
	}
	_, _ = adapter.recommenderQuota.trip("test", recommenderIDCloudSQLIdle, status.Error(codes.ResourceExhausted, "daily quota exceeded"))
	if got, err := adapter.fetchRecommenderInsights(context.Background(), args[0], args[1], args[2], args[3]); err != nil || len(got) != 0 || requests != 1 {
		t.Fatalf("cached result=%v err=%v requests=%d", got, err, requests)
	}
}

func TestRecommendationIteratorRechecksGateBeforeLazyRequest(t *testing.T) {
	requests := 0
	client := newRecommenderTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return recommenderJSONResponse(req, `{}`), nil
	})
	adapter := &gcpAdapter{rec: client}
	it, err := adapter.activeRecommendations(context.Background(), "test", "target-project", "global", recommenderIDCloudSQLIdle, maxInventoryPageSize)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = adapter.recommenderQuota.trip("test", recommenderIDCloudSQLIdle, status.Error(codes.ResourceExhausted, "daily quota exceeded"))
	_, err = it.Next()
	var quotaErr *ports.RecommenderQuotaExhaustedError
	if !errors.As(err, &quotaErr) || requests != 0 {
		t.Fatalf("lazy request: err=%v requests=%d", err, requests)
	}
}

func TestRecommendationIteratorRechecksGateBeforeLaterPages(t *testing.T) {
	requests := 0
	client := newRecommenderTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		if requests > 1 {
			t.Fatalf("quota-blocked second page reached transport")
		}
		return recommenderJSONResponse(req, `{"recommendations":[{"name":"recommendations/one"}],"nextPageToken":"next"}`), nil
	})
	adapter := &gcpAdapter{rec: client}
	it, err := adapter.activeRecommendations(context.Background(), "test", "target-project", "global", recommenderIDCloudSQLIdle, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := it.Next(); err != nil {
		t.Fatal(err)
	}
	_, _ = adapter.recommenderQuota.trip("test", recommenderIDCloudSQLIdle, status.Error(codes.ResourceExhausted, "daily quota exceeded"))
	_, err = it.Next()
	var quotaErr *ports.RecommenderQuotaExhaustedError
	if !errors.As(err, &quotaErr) || requests != 1 {
		t.Fatalf("later page: err=%v requests=%d", err, requests)
	}
}

func newRecommenderTestClient(t *testing.T, transport recommenderRoundTripFunc) *recommender.Client {
	t.Helper()
	client, err := recommender.NewRESTClient(context.Background(), option.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestFetchRecommenderInsightsFollowsPagesAndUsesRecommendationSubtype(t *testing.T) {
	requests := 0
	client := newRecommenderTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		if !strings.Contains(req.URL.Path, "/projects/target-project/locations/us-central1/recommenders/") {
			t.Fatalf("request path = %q", req.URL.Path)
		}
		switch req.URL.Query().Get("pageToken") {
		case "":
			return recommenderJSONResponse(req, `{"recommendations":[{
				"name":"recommendations/other","recommenderSubtype":"OVERPROVISIONED_CLOUD_SQL_INSTANCE",
				"content":{"operationGroups":[{"operations":[{"resource":"//sqladmin.googleapis.com/projects/target-project/instances/other"}]}]}
			}],"nextPageToken":"next"}`), nil
		case "next":
			return recommenderJSONResponse(req, `{"recommendations":[{
				"name":"recommendations/target","recommenderSubtype":"IDLE_CLOUD_SQL_INSTANCE",
				"description":"The instance is idle",
				"content":{"operationGroups":[{"operations":[{"resource":"//sqladmin.googleapis.com/projects/target-project/instances/target"}]}]}
			}]}`), nil
		default:
			t.Fatalf("unexpected page token %q", req.URL.Query().Get("pageToken"))
			return nil, nil
		}
	})
	adapter := &gcpAdapter{rec: client, recommenderCache: newTTLCache[[]recommenderInsight](time.Hour)}

	got, err := adapter.fetchRecommenderInsights(context.Background(), "target-project", "us-central1", recommenderIDCloudSQLOverpro, "/instances/target")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(got) != 1 || got[0].subtype != "idle" || got[0].description != "The instance is idle" {
		t.Fatalf("insights = %+v, requests = %d", got, requests)
	}
}

func TestCostRecommendationsExactLimitCanStillBeComplete(t *testing.T) {
	client := newRecommenderTestClient(t, func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, recommenderIDCloudSQLIdle) {
			return recommenderJSONResponse(req, `{"recommendations":[{
				"name":"recommendations/one","recommenderSubtype":"IDLE_CLOUD_SQL_INSTANCE",
				"content":{"operationGroups":[{"operations":[{"resource":"//sqladmin.googleapis.com/projects/project-1/instances/db"}]}]}
			}]}`), nil
		}
		return recommenderJSONResponse(req, `{}`), nil
	})
	adapter := &gcpAdapter{rec: client, enableCostReasoning: true, callTimeout: time.Second}

	got, err := adapter.ListCostRecommendations(context.Background(), models.ListCostRecommendationsRequest{ProjectID: "project-1", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Recommendations) != 1 || !got.Complete || got.Recommendations[0].Subtype != "idle_cloud_sql_instance" {
		t.Fatalf("recommendations = %+v", got)
	}
}

func TestCostRecommendationsServesCacheBeforeQuotaGate(t *testing.T) {
	requests := 0
	client := newRecommenderTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return recommenderJSONResponse(req, `{}`), nil
	})
	adapter := &gcpAdapter{
		rec: client, enableCostReasoning: true, callTimeout: time.Second,
		costRecommendationCache: newTTLCache[models.ListCostRecommendationsResponse](time.Hour),
	}
	req := models.ListCostRecommendationsRequest{ProjectID: "project-1"}
	first, err := adapter.ListCostRecommendations(context.Background(), req)
	if err != nil || !first.Available || !first.Complete {
		t.Fatalf("initial result=%+v err=%v", first, err)
	}
	initialRequests := requests
	for _, recommenderID := range costRecommenderIDs {
		_, _ = adapter.recommenderQuota.trip("test", recommenderID, status.Error(codes.ResourceExhausted, "daily quota exceeded"))
	}
	second, err := adapter.ListCostRecommendations(context.Background(), models.ListCostRecommendationsRequest{ProjectID: "project-1", Limit: 100})
	if err != nil || !second.Available || requests != initialRequests {
		t.Fatalf("cached result=%+v err=%v requests=%d want=%d", second, err, requests, initialRequests)
	}
}

func TestCostRecommendationsDoesNotCacheIncompleteResults(t *testing.T) {
	requests := 0
	client := newRecommenderTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return recommenderJSONResponse(req, `{"recommendations":[
			{"name":"recommendations/one","content":{"operationGroups":[{"operations":[{"resource":"//compute.googleapis.com/projects/project-1/zones/us-central1-a/instances/one"}]}]}},
			{"name":"recommendations/two","content":{"operationGroups":[{"operations":[{"resource":"//compute.googleapis.com/projects/project-1/zones/us-central1-a/instances/two"}]}]}}
		]}`), nil
	})
	adapter := &gcpAdapter{
		rec: client, enableCostReasoning: true, callTimeout: time.Second,
		costRecommendationCache: newTTLCache[models.ListCostRecommendationsResponse](time.Hour),
	}
	req := models.ListCostRecommendationsRequest{ProjectID: "project-1", Limit: 1}
	for call := 1; call <= 2; call++ {
		got, err := adapter.ListCostRecommendations(context.Background(), req)
		if err != nil || got.Complete || len(got.Recommendations) != 1 {
			t.Fatalf("call %d: result=%+v err=%v", call, got, err)
		}
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2 because incomplete results must not be cached", requests)
	}
}

func TestQuotaTrippedByAuraBlocksSameRecommenderInCost(t *testing.T) {
	requests := 0
	client := newRecommenderTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return recommenderHTTPResponse(req, http.StatusTooManyRequests, nil,
			`{"error":{"code":429,"message":"daily quota exceeded","status":"RESOURCE_EXHAUSTED"}}`), nil
	})
	adapter := &gcpAdapter{rec: client, enableCostReasoning: true, callTimeout: time.Second}
	_, auraErr := adapter.fetchRecommenderInsights(context.Background(), "project-1", "us-central1", recommenderIDCloudSQLIdle, "/instances/db")
	var quotaErr *ports.RecommenderQuotaExhaustedError
	if !errors.As(auraErr, &quotaErr) {
		t.Fatalf("Aura error = %v, want quota error", auraErr)
	}
	_, costErr := adapter.ListCostRecommendations(context.Background(), models.ListCostRecommendationsRequest{ProjectID: "project-1"})
	if !errors.As(costErr, &quotaErr) || requests != 1 {
		t.Fatalf("cost error=%v requests=%d, want shared short-circuit", costErr, requests)
	}
}

func TestSecurityRecommendationsTripsAndObservesQuotaGate(t *testing.T) {
	requests := 0
	client := newRecommenderTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return recommenderHTTPResponse(req, http.StatusTooManyRequests, nil,
			`{"error":{"code":429,"message":"rate limit exceeded","status":"RESOURCE_EXHAUSTED"}}`), nil
	})
	adapter := &gcpAdapter{rec: client, callTimeout: time.Second}
	req := models.SecurityFactsRequest{ProjectID: "project-1"}
	_, err := adapter.ListSecurityRecommendations(context.Background(), req)
	var quotaErr *ports.RecommenderQuotaExhaustedError
	if !errors.As(err, &quotaErr) || quotaErr.RecommenderID != "google.iam.policy.Recommender" || requests != 1 {
		t.Fatalf("first call: err=%v requests=%d", err, requests)
	}
	_, err = adapter.ListSecurityRecommendations(context.Background(), req)
	if !errors.As(err, &quotaErr) || requests != 1 {
		t.Fatalf("blocked call: err=%v requests=%d", err, requests)
	}
}

func TestRecommendationExportPreflightAvoidsAllNetworkWork(t *testing.T) {
	bqRequests := 0
	adapter := newBigQueryTestAdapter(t, func(req *http.Request) (*http.Response, error) {
		bqRequests++
		t.Fatalf("unexpected BigQuery request %s", req.URL)
		return nil, nil
	})
	recRequests := 0
	adapter.rec = newRecommenderTestClient(t, func(req *http.Request) (*http.Response, error) {
		recRequests++
		t.Fatalf("unexpected Recommender request %s", req.URL)
		return nil, nil
	})
	lastID := recommenderIDs[len(recommenderIDs)-1]
	_, _ = adapter.recommenderQuota.trip("test", lastID, status.Error(codes.ResourceExhausted, "daily quota exceeded"))
	_, err := adapter.ExportRecommendationsToBQ(context.Background(), models.ExportRecommendationsToBQRequest{
		ProjectID: "project-1", Dataset: "recommendations", Table: "active",
	})
	var quotaErr *ports.RecommenderQuotaExhaustedError
	if !errors.As(err, &quotaErr) || quotaErr.RecommenderID != lastID || recRequests != 0 || bqRequests != 0 {
		t.Fatalf("export: err=%v rec_requests=%d bq_requests=%d", err, recRequests, bqRequests)
	}
}

func TestRecommendationExportQuotaMidFetchDoesNotMutateBigQuery(t *testing.T) {
	bqRequests := 0
	adapter := newBigQueryTestAdapter(t, func(req *http.Request) (*http.Response, error) {
		bqRequests++
		t.Fatalf("unexpected BigQuery request %s", req.URL)
		return nil, nil
	})
	recRequests := 0
	adapter.rec = newRecommenderTestClient(t, func(req *http.Request) (*http.Response, error) {
		recRequests++
		if strings.Contains(req.URL.Path, recommenderIDs[1]) {
			return recommenderHTTPResponse(req, http.StatusTooManyRequests, nil,
				`{"error":{"code":429,"message":"daily quota exceeded","status":"RESOURCE_EXHAUSTED"}}`), nil
		}
		return recommenderJSONResponse(req, `{}`), nil
	})
	_, err := adapter.ExportRecommendationsToBQ(context.Background(), models.ExportRecommendationsToBQRequest{
		ProjectID: "project-1", Dataset: "recommendations", Table: "active",
	})
	var quotaErr *ports.RecommenderQuotaExhaustedError
	if !errors.As(err, &quotaErr) || recRequests != 2 || bqRequests != 0 {
		t.Fatalf("export: err=%v rec_requests=%d bq_requests=%d", err, recRequests, bqRequests)
	}
}

func TestRecommendationCategoryPrefersAPISubtype(t *testing.T) {
	recommendation := &recommenderpb.Recommendation{RecommenderSubtype: "IDLE_CLOUD_SQL_INSTANCE"}
	if got := recommendationCategory(recommendation, recommenderIDCloudSQLOverpro); got != "idle" {
		t.Fatalf("category = %q, want idle", got)
	}
	if got := recommendationSubtype(recommendation, recommenderIDCloudSQLOverpro); got != "idle_cloud_sql_instance" {
		t.Fatalf("subtype = %q", got)
	}
	if got := classifyRecommenderID("google.example.Unknown"); got != "other" {
		t.Fatalf("unknown recommender category = %q", got)
	}
}

func TestAuraQuotaCacheAndMessageBoundary(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	retryAt := now.Add(time.Minute)
	report := models.AuraReport{RecommenderRetryAt: retryAt}
	if !auraCacheUsable(report, retryAt.Add(-time.Nanosecond)) {
		t.Fatal("quota-degraded Aura report was not usable before retry_at")
	}
	if auraCacheUsable(report, retryAt) {
		t.Fatal("quota-degraded Aura report remained usable at retry_at")
	}
	note := recommenderQuotaNote(retryAt)
	if !strings.Contains(note, retryAt.Format(time.RFC3339)) || strings.Contains(strings.ToLower(note), "session") {
		t.Fatalf("quota note = %q", note)
	}

	encoded, err := json.Marshal(models.AuraReport{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "recommender_retry_at") {
		t.Fatalf("zero retry_at was serialized: %s", encoded)
	}
}
