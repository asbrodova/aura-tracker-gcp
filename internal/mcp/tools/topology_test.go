package tools

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/asbrodova/aura-tracker-gcp/internal/testutil"
	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func callTopology(t *testing.T, args map[string]any) (*mcp.CallToolResult, []models.GetServiceTopologyRequest) {
	t.Helper()
	var seen []models.GetServiceTopologyRequest
	fake := &testutil.FakeGCPService{
		GetServiceTopologyFunc: func(_ context.Context, req models.GetServiceTopologyRequest) (models.ServiceTopologyReport, error) {
			seen = append(seen, req)
			return models.ServiceTopologyReport{ProjectID: req.ProjectID, Project: req.ProjectID, RootService: req.ServiceName}, nil
		},
	}
	tool := NewTopologyTools(fake, slog.Default()).GetServiceTopology()
	req := mcp.CallToolRequest{Params: mcp.CallToolParams{Name: tool.Tool.Name, Arguments: args}}
	result, err := tool.Handler(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return result, seen
}

func TestServiceTopologyAcceptsProjectIDAndDeprecatedProject(t *testing.T) {
	base := map[string]any{"region": "us-central1", "service_name": "api"}
	for name, extra := range map[string]map[string]any{
		"project_id":         {"project_id": "my-project"},
		"deprecated project": {"project": "my-project"},
		"both, same project": {"project_id": "my-project", "project": "my-project"},
	} {
		t.Run(name, func(t *testing.T) {
			args := map[string]any{}
			for k, v := range base {
				args[k] = v
			}
			for k, v := range extra {
				args[k] = v
			}
			result, seen := callTopology(t, args)
			if result.IsError {
				t.Fatalf("unexpected error: %s", resultText(result))
			}
			if len(seen) != 1 || seen[0].ProjectID != "my-project" || seen[0].Project != "my-project" {
				t.Fatalf("service saw %+v, want project_id and project both my-project", seen)
			}
		})
	}
}

func TestServiceTopologyRejectsConflictingProjectArguments(t *testing.T) {
	result, seen := callTopology(t, map[string]any{
		"region": "us-central1", "service_name": "api",
		"project_id": "one", "project": "two",
	})
	if !result.IsError || !strings.Contains(resultText(result), "project_id") {
		t.Fatalf("conflicting project arguments were not refused: %s", resultText(result))
	}
	if len(seen) != 0 {
		t.Fatalf("service was called despite the conflict: %+v", seen)
	}
}

func TestServiceTopologySchemaPrefersProjectID(t *testing.T) {
	tool := NewTopologyTools(&testutil.FakeGCPService{}, slog.Default()).GetServiceTopology().Tool
	if _, ok := tool.InputSchema.Properties["project_id"]; !ok {
		t.Fatal("schema is missing project_id")
	}
	prop, ok := tool.InputSchema.Properties["project"].(map[string]any)
	if !ok {
		t.Fatal("schema dropped the deprecated project argument; older clients would break")
	}
	if description, _ := prop["description"].(string); !strings.HasPrefix(description, "Deprecated") {
		t.Fatalf("project description = %q, want it marked deprecated", description)
	}
}
