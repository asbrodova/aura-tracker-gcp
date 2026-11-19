package prompts

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/internal/environments"
	"github.com/asbrodova/aura-tracker-gcp/internal/testutil"
)

func testRegistry(t *testing.T) *environments.Registry {
	t.Helper()
	registry, err := environments.NewRegistry([]environments.Environment{
		{ProjectID: "private-dev-123", Alias: "dev", Default: true},
		{ProjectID: "private-prod-345", Alias: "prod"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func allPrompts(p *GCPPrompts) []server.ServerPrompt {
	return []server.ServerPrompt{p.AuditSecurityPosture(), p.OptimizeBigQueryCosts(), p.IncidentResponseHelper()}
}

func promptText(t *testing.T, prompt server.ServerPrompt, args map[string]string) string {
	t.Helper()
	result, err := prompt.Handler(context.Background(), mcp.GetPromptRequest{Params: mcp.GetPromptParams{Name: prompt.Prompt.Name, Arguments: args}})
	if err != nil {
		t.Fatalf("%s: %v", prompt.Prompt.Name, err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("%s: %d messages, want 1", prompt.Prompt.Name, len(result.Messages))
	}
	text, ok := result.Messages[0].Content.(mcp.TextContent)
	if !ok {
		t.Fatalf("%s: message content is %T, want text", prompt.Prompt.Name, result.Messages[0].Content)
	}
	return result.Description + "\n" + text.Text
}

func TestPromptsShowAliasesNeverProjectIDs(t *testing.T) {
	p := NewGCPPrompts(&testutil.FakeGCPService{}, slog.Default(), testRegistry(t), "")
	for _, prompt := range allPrompts(p) {
		for _, selector := range []string{"PROD", "private-prod-345"} {
			text := promptText(t, prompt, map[string]string{"project_id": selector})
			if !strings.Contains(text, `"prod"`) {
				t.Errorf("%s(%s) does not name the prod alias:\n%s", prompt.Prompt.Name, selector, text)
			}
			if strings.Contains(text, "private-prod-345") {
				t.Errorf("%s(%s) leaks the private project ID", prompt.Prompt.Name, selector)
			}
		}
		if text := promptText(t, prompt, nil); !strings.Contains(text, `"dev"`) {
			t.Errorf("%s without project_id did not use the default environment", prompt.Prompt.Name)
		}
	}
}

func TestPromptsRejectUnknownEnvironment(t *testing.T) {
	p := NewGCPPrompts(&testutil.FakeGCPService{}, slog.Default(), testRegistry(t), "")
	for _, prompt := range allPrompts(p) {
		_, err := prompt.Handler(context.Background(), mcp.GetPromptRequest{Params: mcp.GetPromptParams{Arguments: map[string]string{"project_id": "staging"}}})
		if err == nil || !strings.Contains(err.Error(), "available environments: dev, prod") {
			t.Errorf("%s: error = %v, want the available environments listed", prompt.Prompt.Name, err)
		}
	}
}

func TestPromptsWithPlaceholderTreatItAsDefault(t *testing.T) {
	p := NewGCPPrompts(&testutil.FakeGCPService{}, slog.Default(), testRegistry(t), "your-project")
	prompt := p.AuditSecurityPosture()
	if !strings.Contains(prompt.Prompt.Arguments[0].Description, "'your-project'") {
		t.Fatalf("project_id description = %q, want the placeholder", prompt.Prompt.Arguments[0].Description)
	}
	if text := promptText(t, prompt, map[string]string{"project_id": "your-project"}); !strings.Contains(text, `"dev"`) {
		t.Fatalf("placeholder did not resolve to the default environment:\n%s", text)
	}
}

func TestPromptsWithoutEnvironmentsRequireProjectID(t *testing.T) {
	p := NewGCPPrompts(&testutil.FakeGCPService{}, slog.Default(), nil, "")
	for _, prompt := range allPrompts(p) {
		if got := prompt.Prompt.Arguments[0].Description; got != "GCP project ID" {
			t.Errorf("%s project_id description = %q", prompt.Prompt.Name, got)
		}
		if _, err := prompt.Handler(context.Background(), mcp.GetPromptRequest{}); err == nil || !strings.Contains(err.Error(), "project_id is required") {
			t.Errorf("%s without project_id: error = %v", prompt.Prompt.Name, err)
		}
		if text := promptText(t, prompt, map[string]string{"project_id": "plain-project"}); !strings.Contains(text, `"plain-project"`) {
			t.Errorf("%s did not use the explicit project", prompt.Prompt.Name)
		}
	}
}

func TestPromptOptionalArguments(t *testing.T) {
	p := NewGCPPrompts(&testutil.FakeGCPService{}, slog.Default(), testRegistry(t), "")

	security := promptText(t, p.AuditSecurityPosture(), nil)
	if !strings.Contains(security, "focus: all") {
		t.Errorf("security audit focus did not default to all:\n%s", security)
	}
	if text := promptText(t, p.AuditSecurityPosture(), map[string]string{"focus": "iam"}); !strings.Contains(text, "focus: iam") {
		t.Errorf("security audit ignored focus=iam")
	}

	allDatasets := promptText(t, p.OptimizeBigQueryCosts(), nil)
	if !strings.Contains(allDatasets, "gcp://dev/bigquery/datasets") {
		t.Errorf("BigQuery prompt without dataset does not start from the dataset list:\n%s", allDatasets)
	}
	oneDataset := promptText(t, p.OptimizeBigQueryCosts(), map[string]string{"dataset_id": "analytics"})
	if !strings.Contains(oneDataset, "gcp://dev/bigquery/analytics/tables") || strings.Contains(oneDataset, "bigquery/datasets") {
		t.Errorf("BigQuery prompt with dataset_id does not scope to it:\n%s", oneDataset)
	}

	incident := promptText(t, p.IncidentResponseHelper(), map[string]string{"service_name": "api", "region": "us-central1"})
	for _, want := range []string{`environment="production"`, `service_name="api"`, `region="us-central1"`} {
		if !strings.Contains(incident, want) {
			t.Errorf("incident prompt missing %s:\n%s", want, incident)
		}
	}
	if text := promptText(t, p.IncidentResponseHelper(), map[string]string{"environment": "staging"}); !strings.Contains(text, `environment="staging"`) {
		t.Errorf("incident prompt ignored environment=staging")
	}
	if strings.Contains(incident, "cluster_name=") || !strings.Contains(incident, `resource_type="cloud_run_service"`) {
		t.Errorf("Cloud Run incident prompt should default the resource type and omit GKE arguments:\n%s", incident)
	}
	gke := promptText(t, p.IncidentResponseHelper(), map[string]string{
		"resource_type": "gke_workload", "service_name": "checkout", "region": "us-central1", "cluster_name": "prod", "namespace": "shop",
	})
	for _, want := range []string{`resource_type="gke_workload"`, `cluster_name="prod"`, `namespace="shop"`, `service_name="checkout"`} {
		if !strings.Contains(gke, want) {
			t.Errorf("GKE incident prompt missing %s:\n%s", want, gke)
		}
	}
	if strings.Contains(gke, "workload_kind=") {
		t.Errorf("an omitted workload_kind was passed to the tool:\n%s", gke)
	}
}
