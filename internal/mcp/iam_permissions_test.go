package mcp

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

type permissionCaptureSvc struct {
	*mockSvc
	requests []models.TestPermissionsRequest
}

func (s *permissionCaptureSvc) TestPermissions(_ context.Context, req models.TestPermissionsRequest) (models.TestPermissionsResponse, error) {
	s.requests = append(s.requests, req)
	return models.TestPermissionsResponse{ProjectID: req.ProjectID}, nil
}

// TestIAMTestPermissionsDefaultsToEnabledModuleCatalog guards against the tool
// drifting back to a hand-maintained probe list: with no explicit permissions
// it must check exactly the capability catalog of the enabled modules, the
// same set the gcp://{project}/iam/my-permissions resource reports.
func TestIAMTestPermissionsDefaultsToEnabledModuleCatalog(t *testing.T) {
	enabled := map[string]bool{ModuleIAM: true, ModuleFunctions: true}
	svc := &permissionCaptureSvc{mockSvc: &mockSvc{}}
	s := New(svc, slog.Default(), "test",
		WithModules(enabled),
		WithEnvironments(testEnvironmentRegistry(t)),
	)

	response := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"gcp_iam_test_permissions","arguments":{"project_id":"dev"}}}`)
	if len(svc.requests) != 1 {
		t.Fatalf("TestPermissions calls = %d, want 1; response=%s", len(svc.requests), response)
	}
	got := svc.requests[0].Permissions
	if want := permissionsForModules(enabled); !slices.Equal(got, want) {
		t.Fatalf("default probe = %v\nwant catalog %v", got, want)
	}
	for _, required := range []string{"cloudfunctions.functions.list", "iam.serviceAccounts.list", "storage.buckets.list"} {
		if !slices.Contains(got, required) {
			t.Errorf("default probe missing %q", required)
		}
	}
	for _, unrelated := range []string{"compute.forwardingRules.list", "spanner.instances.list", "container.clusters.update"} {
		if slices.Contains(got, unrelated) {
			t.Errorf("default probe includes %q from a module that is not enabled", unrelated)
		}
	}
}

func TestIAMTestPermissionsDefaultProbeFollowsModuleSelection(t *testing.T) {
	probe := func(enabled map[string]bool) []string {
		t.Helper()
		svc := &permissionCaptureSvc{mockSvc: &mockSvc{}}
		s := New(svc, slog.Default(), "test", WithModules(enabled), WithEnvironments(testEnvironmentRegistry(t)))
		handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"gcp_iam_test_permissions","arguments":{"project_id":"dev"}}}`)
		if len(svc.requests) != 1 {
			t.Fatalf("TestPermissions calls = %d, want 1", len(svc.requests))
		}
		return svc.requests[0].Permissions
	}

	iamOnly := probe(map[string]bool{ModuleIAM: true})
	withNetworking := probe(map[string]bool{ModuleIAM: true, ModuleNetworking: true})
	if slices.Contains(iamOnly, "compute.networks.list") {
		t.Fatal("iam-only probe includes networking permissions")
	}
	if !slices.Contains(withNetworking, "compute.networks.list") {
		t.Fatal("enabling networking did not add its permissions to the probe")
	}
}

func TestIAMTestPermissionsKeepsExplicitPermissions(t *testing.T) {
	svc := &permissionCaptureSvc{mockSvc: &mockSvc{}}
	s := New(svc, slog.Default(), "test",
		WithModules(map[string]bool{ModuleIAM: true}),
		WithEnvironments(testEnvironmentRegistry(t)),
	)

	response := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"gcp_iam_test_permissions","arguments":{"project_id":"dev","permissions":["run.services.update"]}}}`)
	if len(svc.requests) != 1 {
		t.Fatalf("TestPermissions calls = %d, want 1; response=%s", len(svc.requests), response)
	}
	if got := svc.requests[0].Permissions; !slices.Equal(got, []string{"run.services.update"}) {
		t.Fatalf("explicit permissions replaced: %v", got)
	}
	if strings.Contains(response, `"isError":true`) {
		t.Fatalf("explicit permission check failed: %s", response)
	}
}
