package anonymize

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// ProjectIDMasker replaces the literal GCP project ID with [GCP_PROJECT_ID] in
// every tool result. It is intentionally independent of the full anonymization
// pipeline so ANONYMIZE_PROJECT_ID=true does not activate IP/email scrubbing.
type ProjectIDMasker struct {
	projectID string
}

// NewProjectIDMasker returns a ProjectIDMasker that replaces projectID with
// the static token [GCP_PROJECT_ID].
func NewProjectIDMasker(projectID string) *ProjectIDMasker {
	return &ProjectIDMasker{projectID: projectID}
}

// Scrub replaces all occurrences of the project ID in text content blocks.
func (m *ProjectIDMasker) Scrub(_ context.Context, result *mcp.CallToolResult) (*mcp.CallToolResult, error) {
	if result == nil {
		return nil, nil
	}
	out := *result
	out.Content = make([]mcp.Content, len(result.Content))
	copy(out.Content, result.Content)
	for i, c := range out.Content {
		switch content := c.(type) {
		case mcp.TextContent:
			content.Text = strings.ReplaceAll(content.Text, m.projectID, "[GCP_PROJECT_ID]")
			out.Content[i] = content
		case mcp.EmbeddedResource:
			textResource, ok := content.Resource.(mcp.TextResourceContents)
			if !ok {
				continue
			}
			textResource.Text = strings.ReplaceAll(textResource.Text, m.projectID, "[GCP_PROJECT_ID]")
			content.Resource = textResource
			out.Content[i] = content
		}
	}
	if out.StructuredContent != nil {
		encoded, err := json.Marshal(out.StructuredContent)
		if err == nil {
			encoded = []byte(strings.ReplaceAll(string(encoded), m.projectID, "[GCP_PROJECT_ID]"))
			var scrubbed any
			if json.Unmarshal(encoded, &scrubbed) == nil {
				out.StructuredContent = scrubbed
			}
		}
	}
	return &out, nil
}

// pairedAnonymizer applies first then second in sequence.
type pairedAnonymizer struct {
	first, second Anonymizer
}

// ChainAnonymizers returns an Anonymizer that runs first, then passes its
// output to second. Unlike NewChainedAnonymizer (which is tied to specific
// concrete types), this works with any Anonymizer implementations.
func ChainAnonymizers(first, second Anonymizer) Anonymizer {
	return &pairedAnonymizer{first: first, second: second}
}

func (p *pairedAnonymizer) Scrub(ctx context.Context, result *mcp.CallToolResult) (*mcp.CallToolResult, error) {
	r, err := p.first.Scrub(ctx, result)
	if err != nil {
		return nil, err
	}
	return p.second.Scrub(ctx, r)
}
