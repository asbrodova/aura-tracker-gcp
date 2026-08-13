package mcp

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestEverySelectableModuleDeclaresPermissions(t *testing.T) {
	modules := append(append([]string(nil), AllModules...), ModuleRecommenderExport)
	for _, module := range modules {
		permissions, ok := modulePermissions[module]
		if !ok {
			t.Errorf("module %q has no capability catalog entry", module)
			continue
		}
		if len(permissions) == 0 {
			t.Errorf("module %q declares no permissions", module)
		}
	}
}

func TestPermissionsForModulesAreScopedAndIncludeResources(t *testing.T) {
	permissions := permissionsForModules(map[string]bool{ModuleMonitoring: true})
	set := make(map[string]bool, len(permissions))
	for _, permission := range permissions {
		if set[permission] {
			t.Fatalf("duplicate permission %q", permission)
		}
		set[permission] = true
	}
	for _, required := range []string{"monitoring.timeSeries.list", "cloudtrace.traces.list", "storage.buckets.list", "run.services.list"} {
		if !set[required] {
			t.Errorf("permission set missing %q", required)
		}
	}
	if set["compute.forwardingRules.list"] {
		t.Fatal("networking permission leaked into monitoring-only capability set")
	}
}

func TestActiveModuleSetExcludesUnavailableOptionalModules(t *testing.T) {
	modules := []ToolModule{duplicateToolModule{name: ModuleMonitoring}}
	active := activeModuleSet(modules, nil)
	if !active[ModuleMonitoring] || active[ModuleCost] || active[ModuleRecommenderExport] {
		t.Fatalf("active modules = %#v", active)
	}
}

func TestSetupScriptDefaultModulesMatchRegistry(t *testing.T) {
	data, err := os.ReadFile("../../scripts/setup-iam.sh")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?m)^DEFAULT_MODULES="([^"]+)"$`).FindSubmatch(data)
	if len(match) != 2 {
		t.Fatal("setup script DEFAULT_MODULES declaration not found")
	}
	got := strings.Split(string(match[1]), ",")
	if strings.Join(got, ",") != strings.Join(AllModules, ",") {
		t.Fatalf("setup modules drifted from registry\nsetup: %v\nserver: %v", got, AllModules)
	}
}
