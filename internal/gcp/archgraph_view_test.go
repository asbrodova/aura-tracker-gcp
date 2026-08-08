package gcp

import (
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func TestApplyArchitectureGraphViewPreservesIntegrity(t *testing.T) {
	graph := models.ServerlessGraph{
		ProjectID: "project",
		Nodes: []models.GraphNode{
			{ID: "a", Kind: models.KindCloudRunService, Region: "us-central1", ProjectID: "project"},
			{ID: "b", Kind: models.KindCloudSQLInstance, Region: "us-central1", ProjectID: "project"},
			{ID: "c", Kind: models.KindCloudRunService, Region: "europe-west1", ProjectID: "project"},
			{ID: "external", Kind: models.KindExternalEndpoint, ProjectID: "project"},
		},
		Edges: []models.GraphEdge{
			{Source: "a", Target: "b", Type: models.EdgeReadsFromDB},
			{Source: "a", Target: "external", Type: models.EdgeInvokes},
			{Source: "c", Target: "b", Type: models.EdgeReadsFromDB},
		},
	}

	got := applyArchitectureGraphView(graph, models.ExportArchitectureGraphRequest{
		ProjectID: "project",
		Regions:   []string{"us-central1"},
	})
	if len(got.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2: %+v", len(got.Nodes), got.Nodes)
	}
	if len(got.Edges) != 1 || got.Edges[0].Source != "a" || got.Edges[0].Target != "b" {
		t.Fatalf("edges were not pruned with nodes: %+v", got.Edges)
	}
	for _, group := range got.Groups {
		for _, member := range group.Members {
			if member != "a" && member != "b" {
				t.Fatalf("group %q contains filtered member %q", group.ID, member)
			}
		}
	}
}

func TestApplyArchitectureGraphViewCapsNodesDeterministically(t *testing.T) {
	graph := models.ServerlessGraph{
		ProjectID: "project",
		Nodes: []models.GraphNode{
			{ID: "c", ProjectID: "project"},
			{ID: "a", ProjectID: "project"},
			{ID: "b", ProjectID: "project"},
		},
		Edges: []models.GraphEdge{{Source: "a", Target: "b"}, {Source: "b", Target: "c"}},
	}

	got := applyArchitectureGraphView(graph, models.ExportArchitectureGraphRequest{ProjectID: "project", MaxNodes: 2})
	if !got.Truncated {
		t.Fatal("expected truncated graph")
	}
	if len(got.Nodes) != 2 || got.Nodes[0].ID != "a" || got.Nodes[1].ID != "b" {
		t.Fatalf("unexpected deterministic cap: %+v", got.Nodes)
	}
	if len(got.Edges) != 1 || got.Edges[0].Source != "a" || got.Edges[0].Target != "b" {
		t.Fatalf("edges were not capped with nodes: %+v", got.Edges)
	}
}

func TestArchGraphCacheKeyIncludesCollectionOptionsOnly(t *testing.T) {
	base := models.ExportArchitectureGraphRequest{ProjectID: "project", LookbackHours: 24}
	withViewOptions := base
	withViewOptions.MaxNodes = 10
	withViewOptions.IncludeExternal = true
	if got, want := archGraphCacheKey(withViewOptions), archGraphCacheKey(base); got != want {
		t.Fatalf("view option changed cache key: got %q want %q", got, want)
	}

	withFlowLogs := base
	withFlowLogs.EnableFlowLogInference = true
	if archGraphCacheKey(withFlowLogs) == archGraphCacheKey(base) {
		t.Fatal("flow-log inference must change cache key")
	}

	withLookback := base
	withLookback.LookbackHours = 48
	if archGraphCacheKey(withLookback) == archGraphCacheKey(base) {
		t.Fatal("lookback must change cache key")
	}
}
