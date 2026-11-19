package gcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/api/option"
)

// restRoute is one canned response of a fake Google REST API. A request
// matches when its method is equal, its path ends with path, and every query
// parameter in query has the given value. Routes are tried in order, so put
// the more specific ones (for example a second page) first.
type restRoute struct {
	method string
	path   string
	query  map[string]string
	status int
	body   any
}

// restFake is an httptest server that serves restRoutes to a REST client
// built with its options(). Unmatched requests fail the test.
type restFake struct {
	t      *testing.T
	server *httptest.Server
	routes []restRoute
	mu     sync.Mutex
	calls  []string
}

func newRESTFake(t *testing.T, routes ...restRoute) *restFake {
	t.Helper()
	fake := &restFake{t: t, routes: routes}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *restFake) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
	f.mu.Unlock()
	for _, route := range f.routes {
		if route.method != r.Method || !strings.HasSuffix(r.URL.Path, route.path) || !queryMatches(r, route.query) {
			continue
		}
		status := route.status
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(route.body)
		return
	}
	f.t.Errorf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(apiError(http.StatusNotFound, "NOT_FOUND", "no fake route"))
}

func queryMatches(r *http.Request, query map[string]string) bool {
	for key, value := range query {
		if r.URL.Query().Get(key) != value {
			return false
		}
	}
	return true
}

// options points a google.golang.org/api client at the fake.
func (f *restFake) options() []option.ClientOption {
	return []option.ClientOption{
		option.WithEndpoint(f.server.URL + "/"),
		option.WithoutAuthentication(),
		option.WithHTTPClient(f.server.Client()),
	}
}

func (f *restFake) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// apiError is the JSON error body Google APIs return.
func apiError(code int, status, message string) map[string]any {
	return map[string]any{"error": map[string]any{"code": code, "status": status, "message": message}}
}

// testAdapter returns an adapter with the timeouts and caches the methods
// under test expect; set the SDK clients on the result.
func testAdapter() *gcpAdapter {
	return &gcpAdapter{
		callTimeout:  10 * time.Second,
		graphTimeout: 10 * time.Second,
		regionsCache: newTTLCache[regionDiscovery](time.Minute),
	}
}
