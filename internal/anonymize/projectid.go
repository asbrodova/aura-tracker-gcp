package anonymize

import (
	"context"
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
		tc, ok := c.(mcp.TextContent)
		if !ok {
			continue
		}
		tc.Text = strings.ReplaceAll(tc.Text, m.projectID, "[GCP_PROJECT_ID]")
		out.Content[i] = tc
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
