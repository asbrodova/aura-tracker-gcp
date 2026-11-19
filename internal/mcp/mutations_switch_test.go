package mcp

import (
	"log/slog"
	"slices"
	"sort"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

var knownMutationTools = []string{
	"gcp_cloudrun_update_traffic",
	"gcp_export_recommendations_to_bq",
	"gcp_gke_scale_deployment",
}

func mutationToolNames(tools map[string]*server.ServerTool) []string {
	var names []string
	for name, tool := range tools {
		if isMutationTool(*tool) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// Without MUTATIONS_ENABLED the server must be read-only: no tool that
// changes resources is registered, even when the export is configured.
func TestMutationToolsAreNotRegisteredByDefault(t *testing.T) {
	s := New(&mockSvc{}, slog.Default(), "test", WithRecommenderExport("recommendations"))
	registered := s.ListTools()
	for _, name := range knownMutationTools {
		if _, ok := registered[name]; ok {
			t.Errorf("mutation tool %q is registered without WithMutations", name)
		}
	}
	if names := mutationToolNames(registered); len(names) != 0 {
		t.Errorf("tools with ReadOnlyHint=false registered in read-only mode: %v", names)
	}
}

// Every tool must declare ReadOnlyHint, because the switch uses it to tell
// mutations apart; and the tools marked as mutations must be exactly the
// known set, so a new write tool cannot slip past the switch or the docs.
func TestWithMutationsRegistersExactlyTheMutationTools(t *testing.T) {
	s := New(&mockSvc{}, slog.Default(), "test", WithMutations(), WithRecommenderExport("recommendations"))
	registered := s.ListTools()
	for name, tool := range registered {
		if tool.Tool.Annotations.ReadOnlyHint == nil {
			t.Errorf("tool %q does not declare ReadOnlyHint", name)
		}
	}
	if got := mutationToolNames(registered); !slices.Equal(got, knownMutationTools) {
		t.Fatalf("mutation tools = %v, want %v (update knownMutationTools and the docs together)", got, knownMutationTools)
	}
}

func TestWritePermissionsFollowTheMutationSwitch(t *testing.T) {
	modules := map[string]bool{ModuleGKE: true, ModuleCloudRun: true, ModuleAura: true}
	writes := []string{"container.clusters.update", "run.services.update"}

	readOnly := permissionsForModules(modules, false)
	for _, permission := range writes {
		if slices.Contains(readOnly, permission) {
			t.Errorf("read-only catalog includes write permission %q", permission)
		}
	}
	withMutations := permissionsForModules(modules, true)
	for _, permission := range writes {
		if !slices.Contains(withMutations, permission) {
			t.Errorf("catalog with mutations is missing %q", permission)
		}
	}
	if !slices.Contains(withMutations, "container.clusters.list") || !slices.Contains(readOnly, "container.clusters.list") {
		t.Error("read permissions must not depend on the switch")
	}
}
