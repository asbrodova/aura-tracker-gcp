package tools

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

type fakeArchGraphService struct{}

func (fakeArchGraphService) ExportArchitectureGraph(context.Context, models.ExportArchitectureGraphRequest) (models.ServerlessGraph, error) {
	return models.ServerlessGraph{}, nil
}

type fakeDiagramGenerator struct {
	response models.ArchitectureDiagramResponse
	captured models.GenerateArchitectureDiagramRequest
	err      error
}

func (f *fakeDiagramGenerator) Generate(_ context.Context, req models.GenerateArchitectureDiagramRequest) (models.ArchitectureDiagramResponse, error) {
	f.captured = req
	return f.response, f.err
}

func TestArchitectureDiagramToolDefinition(t *testing.T) {
	module := NewArchGraphTools(fakeArchGraphService{}, &fakeDiagramGenerator{}, slog.Default())
	registered := module.GetTools()
	if len(registered) != 2 {
		t.Fatalf("tool count = %d, want 2", len(registered))
	}
	tool := registered[1].Tool
	if tool.Name != "gcp_generate_architecture_diagram" {
		t.Fatalf("tool name = %q", tool.Name)
	}
	for _, property := range []string{
		"project_id", "environment", "whole_project", "format", "regions", "direction",
		"group_by", "max_depth", "max_nodes", "min_confidence", "include_external", "lookback_hours",
	} {
		if _, ok := tool.InputSchema.Properties[property]; !ok {
			t.Errorf("input schema missing %q", property)
		}
	}
	if tool.OutputSchema.Type != "object" {
		t.Fatal("diagram tool must advertise an output schema")
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Fatal("diagram tool must be annotated read-only")
	}
}

func TestArchitectureGraphToolDefinitionExposesSupportedViewOptions(t *testing.T) {
	tool := NewArchGraphTools(fakeArchGraphService{}, &fakeDiagramGenerator{}, slog.Default()).GetTools()[0].Tool
	for _, property := range []string{"project_id", "regions", "include_external", "max_depth", "max_nodes", "lookback_hours"} {
		if _, ok := tool.InputSchema.Properties[property]; !ok {
			t.Errorf("input schema missing %q", property)
		}
	}
	if _, ok := tool.InputSchema.Properties["enable_flow_log_inference"]; ok {
		t.Fatal("unsupported flow-log inference must not be advertised")
	}
}

func TestArchitectureDiagramToolReturnsDisplayAndStructuredContent(t *testing.T) {
	generator := &fakeDiagramGenerator{response: models.ArchitectureDiagramResponse{
		Status: models.DiagramStatusComplete,
		Format: models.DiagramFormatMermaid,
		Source: "flowchart LR\n  a --> b",
	}}
	tool := NewArchGraphTools(fakeArchGraphService{}, generator, slog.Default()).GetTools()[1]
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]any{
		"project_id":  "project",
		"environment": "production",
		"format":      "mermaid",
	}

	result, err := tool.Handler(context.Background(), request)
	if err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("unexpected result: %+v", result)
	}
	if generator.captured.ProjectID != "project" || generator.captured.Environment != "production" {
		t.Fatalf("request was not propagated: %+v", generator.captured)
	}
	content, ok := result.Content[0].(mcp.TextContent)
	if !ok || !strings.HasPrefix(content.Text, "```mermaid") {
		t.Fatalf("missing Mermaid display content: %+v", result.Content)
	}
	if _, ok := result.StructuredContent.(models.ArchitectureDiagramResponse); !ok {
		t.Fatalf("unexpected structured content type %T", result.StructuredContent)
	}
}

func TestArchitectureDiagramResultEmbedsSVG(t *testing.T) {
	result := architectureDiagramResult(models.ArchitectureDiagramResponse{
		Status: models.DiagramStatusComplete,
		Format: models.DiagramFormatSVG,
		Source: `<svg xmlns="http://www.w3.org/2000/svg"></svg>`,
	})
	if len(result.Content) != 2 {
		t.Fatalf("content count = %d, want 2", len(result.Content))
	}
	embedded, ok := result.Content[1].(mcp.EmbeddedResource)
	if !ok {
		t.Fatalf("SVG is not an embedded resource: %T", result.Content[1])
	}
	resource, ok := embedded.Resource.(mcp.TextResourceContents)
	if !ok || resource.MIMEType != "image/svg+xml" || !strings.HasPrefix(resource.URI, "diagram://architecture/") {
		t.Fatalf("unexpected SVG resource: %+v", embedded.Resource)
	}
}
