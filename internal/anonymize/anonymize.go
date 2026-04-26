// Package anonymize provides a pluggable PII/credential scrubbing layer for
// MCP tool results. All implementations must be safe for concurrent use.
package anonymize

import (
	"context"

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
