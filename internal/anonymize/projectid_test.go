package anonymize

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestProjectIDMaskerScrubsEmbeddedAndStructuredContent(t *testing.T) {
	const project = "my-sensitive-project"
	result := &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewEmbeddedResource(mcp.TextResourceContents{
			URI: "diagram://architecture/test.svg", MIMEType: "image/svg+xml",
			Text: `<svg><text>` + project + `</text></svg>`,
		})},
		StructuredContent: map[string]any{"source": project},
	}
	got, err := NewProjectIDMasker(project).Scrub(context.Background(), result)
	if err != nil {
		t.Fatalf("Scrub() error = %v", err)
	}
	embedded := got.Content[0].(mcp.EmbeddedResource)
	resource := embedded.Resource.(mcp.TextResourceContents)
	if strings.Contains(resource.Text, project) || !strings.Contains(resource.Text, "[GCP_PROJECT_ID]") {
		t.Fatalf("embedded resource was not masked: %s", resource.Text)
	}
	structured, _ := json.Marshal(got.StructuredContent)
	if strings.Contains(string(structured), project) || !strings.Contains(string(structured), "[GCP_PROJECT_ID]") {
		t.Fatalf("structured content was not masked: %s", structured)
	}
}

func TestProjectIDReplacerUsesAliasesAcrossAllTextSurfaces(t *testing.T) {
	replacer := NewProjectIDReplacer(map[string]string{
		"company-project-123": "dev",
		"company-project":     "shared",
	})
	result := &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent("projects/company-project-123 and company-project"),
			mcp.ResourceLink{Type: "resource_link", URI: "gcp://company-project-123/logs", Name: "company-project-123", Description: "company-project"},
		},
		StructuredContent: map[string]any{
			"project_id": "company-project-123",
			"path":       "projects/company-project-123/resources/one",
		},
	}
	got, err := replacer.Scrub(context.Background(), result)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, "company-project") {
		t.Fatalf("project ID leaked: %s", text)
	}
	if !strings.Contains(text, "dev") || !strings.Contains(text, "shared") {
		t.Fatalf("aliases missing: %s", text)
	}
}

func TestProjectIDReplacerScrubsResourcesAndPrompts(t *testing.T) {
	replacer := NewProjectIDReplacer(map[string]string{"private-project": "prod"})
	resources, err := replacer.ScrubResourceContents([]mcp.ResourceContents{
		mcp.TextResourceContents{URI: "gcp://private-project/storage", Text: `{"project_id":"private-project"}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	resource := resources[0].(mcp.TextResourceContents)
	if strings.Contains(resource.URI+resource.Text, "private-project") || !strings.Contains(resource.Text, "prod") {
		t.Fatalf("resource not scrubbed: %+v", resource)
	}

	prompt, err := replacer.ScrubPromptResult(&mcp.GetPromptResult{
		Description: "Audit private-project",
		Messages: []mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent("Use private-project")),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(prompt)
	if strings.Contains(string(encoded), "private-project") || !strings.Contains(string(encoded), "prod") {
		t.Fatalf("prompt not scrubbed: %s", encoded)
	}
}

func TestProjectIDReplacerWithholdsOpaqueContent(t *testing.T) {
	replacer := NewProjectIDReplacer(map[string]string{"private-project": "prod"})
	_, err := replacer.Scrub(context.Background(), &mcp.CallToolResult{
		Content: []mcp.Content{mcp.ImageContent{Type: "image", Data: "opaque", MIMEType: "image/png"}},
	})
	if err == nil {
		t.Fatal("expected opaque content to be withheld")
	}
}
