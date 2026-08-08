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
