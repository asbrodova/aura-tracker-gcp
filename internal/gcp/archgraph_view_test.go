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
	if got, want := archGraphCacheKey(withFlowLogs), archGraphCacheKey(base); got != want {
		t.Fatalf("unsupported flow-log flag changed cache key: got %q want %q", got, want)
	}

	withLookback := base
	withLookback.LookbackHours = 48
	if archGraphCacheKey(withLookback) == archGraphCacheKey(base) {
		t.Fatal("lookback must change cache key")
	}
}

func TestApplyArchitectureGraphViewDepthDoesNotRerootTruncatedTail(t *testing.T) {
	graph := models.ServerlessGraph{
		ProjectID: "project",
		Nodes:     []models.GraphNode{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "cycle-a"}, {ID: "cycle-b"}},
		Edges: []models.GraphEdge{
			{Source: "a", Target: "b"}, {Source: "b", Target: "c"},
			{Source: "cycle-a", Target: "cycle-b"}, {Source: "cycle-b", Target: "cycle-a"},
		},
	}
	got := applyArchitectureGraphView(graph, models.ExportArchitectureGraphRequest{ProjectID: "project", MaxDepth: 1})
	ids := nodeIDSet(got.Nodes)
	if !ids["a"] || !ids["b"] || ids["c"] {
		t.Fatalf("depth-limited chain = %#v, want a/b but not c", ids)
	}
	if !ids["cycle-a"] || !ids["cycle-b"] {
		t.Fatalf("cyclic component was erased: %#v", ids)
	}
}

func TestBuildArchGroupsScopesSameNamedClustersByLocation(t *testing.T) {
	groups := buildArchGroups("project", []models.GraphNode{
		{ID: "a", Region: "us-central1", ClusterName: "primary", Namespace: "prod"},
		{ID: "b", Region: "europe-west1", ClusterName: "primary", Namespace: "prod"},
	})
	clusterGroups := 0
	namespaceGroups := 0
	for _, group := range groups {
		switch group.Kind {
		case models.GroupKindCluster:
			clusterGroups++
		case models.GroupKindNamespace:
			namespaceGroups++
		}
	}
	if clusterGroups != 2 || namespaceGroups != 2 {
		t.Fatalf("same-named regional clusters were merged: %+v", groups)
	}
}

func TestArchitectureIngressKindNormalizesKubernetesKinds(t *testing.T) {
	if got := architectureIngressKind("Ingress"); got != models.KindGKEIngress {
		t.Fatalf("Ingress kind = %q", got)
	}
	if got := architectureIngressKind("HTTPRoute"); got != models.KindGKEGateway {
		t.Fatalf("HTTPRoute kind = %q", got)
	}
}

func TestValidateArchitectureGraphRequestBoundsInputs(t *testing.T) {
	valid := models.ExportArchitectureGraphRequest{ProjectID: "valid-project", Regions: []string{"us-central1"}, MaxDepth: 2, MaxNodes: 100, LookbackHours: 24}
	if err := validateArchitectureGraphRequest(valid); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	invalid := []models.ExportArchitectureGraphRequest{
		{ProjectID: "bad"},
		{ProjectID: "valid-project", Regions: []string{"../other"}},
		{ProjectID: "valid-project", MaxDepth: 101},
		{ProjectID: "valid-project", MaxNodes: 10001},
		{ProjectID: "valid-project", LookbackHours: 721},
	}
	for _, request := range invalid {
		if err := validateArchitectureGraphRequest(request); err == nil {
			t.Fatalf("invalid request accepted: %+v", request)
		}
	}
}
