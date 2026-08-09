// Package anonymize provides a pluggable PII/credential scrubbing layer for
// MCP tool results. All implementations must be safe for concurrent use.
package anonymize

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/mark3labs/mcp-go/mcp"
)

// Anonymizer scrubs one tool result before it reaches the LLM.
type Anonymizer interface {
	Scrub(ctx context.Context, result *mcp.CallToolResult) (*mcp.CallToolResult, error)
}

// Finding records one matched PII/credential occurrence.
type Finding struct {
	PatternName  string `json:"pattern_name"`
	JSONPath     string `json:"json_path,omitempty"` // dot-separated path, e.g. "clusters[0].endpoint"
	ContentIndex int    `json:"content_index"`       // index into CallToolResult.Content
	MatchCount   int    `json:"match_count"`
}

// AuditReport is returned in place of the real result when audit_only is true.
// Developers use it to tune patterns safely before enabling real masking.
type AuditReport struct {
	TotalMatches int       `json:"total_matches"`
	Findings     []Finding `json:"findings"`
	PatternsSeen []string  `json:"patterns_seen"` // sorted; one entry per matching rule
}

// NoopAnonymizer is the identity implementation — returns the result unchanged.
// Used when anonymization is disabled.
type NoopAnonymizer struct{}

func (NoopAnonymizer) Scrub(_ context.Context, r *mcp.CallToolResult) (*mcp.CallToolResult, error) {
	return r, nil
}

// buildAuditResult constructs a CallToolResult whose content is an AuditReport
// JSON summary of the supplied findings. Shared by LocalScrubber and DLPAnonymizer.
func buildAuditResult(findings []Finding) (*mcp.CallToolResult, error) {
	seen := map[string]struct{}{}
	for _, f := range findings {
		seen[f.PatternName] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for k := range seen {
		names = append(names, k)
	}
	sort.Strings(names)

	total := 0
	for _, f := range findings {
		total += f.MatchCount
	}

	report := AuditReport{
		TotalMatches: total,
		Findings:     findings,
		PatternsSeen: names,
	}
	return mcp.NewToolResultJSON(report)
}

// ScrubResourceContents applies a configured anonymizer to textual MCP
// resources. Binary resources are left unchanged because their blob is not
// interpreted as text by the protocol.
func ScrubResourceContents(ctx context.Context, a Anonymizer, contents []mcp.ResourceContents) ([]mcp.ResourceContents, error) {
	out := make([]mcp.ResourceContents, len(contents))
	copy(out, contents)
	for i, content := range out {
		text, ok := content.(mcp.TextResourceContents)
		if !ok {
			continue
		}
		result, err := a.Scrub(ctx, &mcp.CallToolResult{
			Content:           []mcp.Content{mcp.NewTextContent(text.Text)},
			StructuredContent: text.Meta,
		})
		if err != nil {
			return nil, err
		}
		if result == nil || len(result.Content) != 1 {
			return nil, errors.New("anonymize: resource scrubber returned invalid content")
		}
		masked, ok := result.Content[0].(mcp.TextContent)
		if !ok {
			return nil, errors.New("anonymize: resource scrubber returned non-text content")
		}
		text.Text = masked.Text
		if result.StructuredContent == nil {
			text.Meta = nil
		} else if meta, ok := result.StructuredContent.(map[string]any); ok {
			text.Meta = meta
		} else {
			return nil, errors.New("anonymize: resource metadata scrubber returned invalid content")
		}
		out[i] = text
	}
	return out, nil
}

// ScrubPromptResult applies a configured anonymizer to every prompt message.
func ScrubPromptResult(ctx context.Context, a Anonymizer, prompt *mcp.GetPromptResult) (*mcp.GetPromptResult, error) {
	if prompt == nil {
		return nil, nil
	}
	out := *prompt
	out.Messages = append([]mcp.PromptMessage(nil), prompt.Messages...)
	for i := range out.Messages {
		result, err := a.Scrub(ctx, &mcp.CallToolResult{Content: []mcp.Content{out.Messages[i].Content}})
		if err != nil {
			return nil, err
		}
		if result == nil || len(result.Content) != 1 {
			return nil, errors.New("anonymize: prompt scrubber returned invalid content")
		}
		out.Messages[i].Content = result.Content[0]
	}
	return &out, nil
}

// ScrubError prevents non-tool error channels from bypassing privacy filters.
func ScrubError(ctx context.Context, a Anonymizer, source error) error {
	if source == nil {
		return nil
	}
	result, err := a.Scrub(ctx, mcp.NewToolResultError(source.Error()))
	if err != nil || result == nil || len(result.Content) != 1 {
		return errors.New("anonymize: scrub failed; error withheld")
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		return errors.New("anonymize: scrub failed; error withheld")
	}
	return fmt.Errorf("%s", text.Text)
}
