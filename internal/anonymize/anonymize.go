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
	if len(contents) == 0 {
		return contents, nil
	}
	wrapped := &mcp.CallToolResult{Content: make([]mcp.Content, len(contents))}
	for i, content := range contents {
		switch typed := content.(type) {
		case mcp.TextResourceContents:
			wrapped.Content[i] = mcp.NewEmbeddedResource(typed)
		case mcp.BlobResourceContents:
			wrapped.Content[i] = mcp.NewEmbeddedResource(typed)
		default:
			return nil, fmt.Errorf("anonymize: unsupported resource content %T withheld", content)
		}
	}

	result, err := a.Scrub(ctx, wrapped)
	if err != nil {
		return nil, err
	}
	if audit, ok := auditResultContent(result); ok {
		return []mcp.ResourceContents{mcp.TextResourceContents{
			URI:      "anonymize://audit",
			MIMEType: "application/json",
			Text:     audit.Text,
		}}, nil
	}
	if result == nil || len(result.Content) != len(contents) {
		return nil, errors.New("anonymize: resource scrubber returned invalid content")
	}
	out := make([]mcp.ResourceContents, len(contents))
	for i, content := range result.Content {
		embedded, ok := content.(mcp.EmbeddedResource)
		if !ok || embedded.Resource == nil {
			return nil, errors.New("anonymize: resource scrubber returned invalid embedded content")
		}
		out[i] = embedded.Resource
	}
	return out, nil
}

// ScrubPromptResult applies a configured anonymizer to every prompt message.
func ScrubPromptResult(ctx context.Context, a Anonymizer, prompt *mcp.GetPromptResult) (*mcp.GetPromptResult, error) {
	if prompt == nil {
		return nil, nil
	}
	wrapped := &mcp.CallToolResult{
		Result:            prompt.Result,
		Content:           make([]mcp.Content, len(prompt.Messages)),
		StructuredContent: map[string]any{"description": prompt.Description},
	}
	for i, message := range prompt.Messages {
		wrapped.Content[i] = message.Content
	}
	result, err := a.Scrub(ctx, wrapped)
	if err != nil {
		return nil, err
	}
	if audit, ok := auditResultContent(result); ok {
		return &mcp.GetPromptResult{
			Description: "Anonymization audit report",
			Messages: []mcp.PromptMessage{
				mcp.NewPromptMessage(mcp.RoleUser, audit),
			},
		}, nil
	}
	if result == nil || len(result.Content) != len(prompt.Messages) {
		return nil, errors.New("anonymize: prompt scrubber returned invalid content")
	}
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		return nil, errors.New("anonymize: prompt scrubber returned invalid description")
	}
	description, ok := structured["description"].(string)
	if !ok {
		return nil, errors.New("anonymize: prompt scrubber returned non-text description")
	}
	out := *prompt
	out.Result = result.Result
	out.Description = description
	out.Messages = make([]mcp.PromptMessage, len(prompt.Messages))
	for i, message := range prompt.Messages {
		out.Messages[i] = mcp.PromptMessage{Role: message.Role, Content: result.Content[i]}
	}
	return &out, nil
}

func auditResultContent(result *mcp.CallToolResult) (mcp.TextContent, bool) {
	if result == nil || len(result.Content) != 1 {
		return mcp.TextContent{}, false
	}
	switch result.StructuredContent.(type) {
	case AuditReport, *AuditReport:
		text, ok := result.Content[0].(mcp.TextContent)
		return text, ok
	default:
		return mcp.TextContent{}, false
	}
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
