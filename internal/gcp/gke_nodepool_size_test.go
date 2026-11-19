package gcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"

	containerpb "cloud.google.com/go/container/apiv1/containerpb"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

func TestParseInstanceGroupManagerURL(t *testing.T) {
	project, zone, name, ok := parseInstanceGroupManagerURL("https://www.googleapis.com/compute/v1/projects/p-1/zones/us-central1-a/instanceGroupManagers/gke-pool-grp")
	if !ok || project != "p-1" || zone != "us-central1-a" || name != "gke-pool-grp" {
		t.Fatalf("got (%q, %q, %q, %v)", project, zone, name, ok)
	}
	if _, _, _, ok := parseInstanceGroupManagerURL("https://example.com/not/an/instance/group"); ok {
		t.Fatal("unrelated URL parsed as an instance group")
	}
}

func TestNodePoolSizeFallsBackToInitialCountWithWarning(t *testing.T) {
	a := &gcpAdapter{}
	np := &containerpb.NodePool{InitialNodeCount: 2, Locations: []string{"us-central1-a", "us-central1-b", "us-central1-c"}}
	size := a.currentNodePoolSize(context.Background(), "p", np)
	if size.perZone != 2 || size.zones != 3 || size.total != 6 || size.source != "initial_node_count" {
		t.Fatalf("size = %+v", size)
	}
	if len(size.warnings) != 1 || !strings.Contains(size.warnings[0], "may be out of date") {
		t.Fatalf("warnings = %v", size.warnings)
	}
}

func newFakeComputeService(t *testing.T, targetSizes map[string]int64) *compute.Service {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/instanceGroupManagers/") {
			http.NotFound(w, r)
			return
		}
		size, ok := targetSizes[path.Base(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"name": path.Base(r.URL.Path), "targetSize": size})
	}))
	t.Cleanup(srv.Close)
	svc, err := compute.NewService(context.Background(), option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication(), option.WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func igmURL(zone, name string) string {
	return "https://www.googleapis.com/compute/v1/projects/p/zones/" + zone + "/instanceGroupManagers/" + name
}

func TestNodePoolSizeReadsInstanceGroups(t *testing.T) {
	a := &gcpAdapter{computeSvc: newFakeComputeService(t, map[string]int64{"grp-a": 4, "grp-b": 4})}
	np := &containerpb.NodePool{
		InitialNodeCount:  1,
		Locations:         []string{"us-central1-a", "us-central1-b"},
		InstanceGroupUrls: []string{igmURL("us-central1-a", "grp-a"), igmURL("us-central1-b", "grp-b")},
	}
	size := a.currentNodePoolSize(context.Background(), "p", np)
	if size.perZone != 4 || size.total != 8 || size.zones != 2 || size.uneven || size.source != "instance_groups" || len(size.warnings) != 0 {
		t.Fatalf("size = %+v", size)
	}
	if size.zoneSizes["us-central1-a"] != 4 || size.zoneSizes["us-central1-b"] != 4 {
		t.Fatalf("zone sizes = %v", size.zoneSizes)
	}
}

func TestNodePoolSizeReportsUnevenZones(t *testing.T) {
	a := &gcpAdapter{computeSvc: newFakeComputeService(t, map[string]int64{"grp-a": 2, "grp-b": 5})}
	np := &containerpb.NodePool{InstanceGroupUrls: []string{igmURL("us-central1-a", "grp-a"), igmURL("us-central1-b", "grp-b")}}
	size := a.currentNodePoolSize(context.Background(), "p", np)
	if !size.uneven || size.perZone != 5 || size.total != 7 {
		t.Fatalf("size = %+v", size)
	}
	if len(size.warnings) != 1 || !strings.Contains(size.warnings[0], "different sizes") {
		t.Fatalf("warnings = %v", size.warnings)
	}
}

func TestNodePoolSizeFallsBackWhenInstanceGroupLookupFails(t *testing.T) {
	a := &gcpAdapter{computeSvc: newFakeComputeService(t, map[string]int64{})}
	np := &containerpb.NodePool{InitialNodeCount: 3, InstanceGroupUrls: []string{igmURL("us-central1-a", "missing")}}
	size := a.currentNodePoolSize(context.Background(), "p", np)
	if size.source != "initial_node_count" || size.perZone != 3 || len(size.warnings) != 1 {
		t.Fatalf("size = %+v", size)
	}
}
