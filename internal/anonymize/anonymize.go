// Package anonymize provides a pluggable PII/credential scrubbing layer for
// MCP tool results. All implementations must be safe for concurrent use.
package anonymize

import (
	"context"
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
