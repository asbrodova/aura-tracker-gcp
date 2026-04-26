package anonymize

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

// ChainedAnonymizer applies LocalScrubber then DLPAnonymizer in sequence.
// The local step always runs with masking on (auditOnly=false) so its output
// feeds the DLP step, which honours its own auditOnly setting.
type ChainedAnonymizer struct {
	local *LocalScrubber
	dlp   *DLPAnonymizer
}

func NewChainedAnonymizer(local *LocalScrubber, dlp *DLPAnonymizer) *ChainedAnonymizer {
	return &ChainedAnonymizer{local: local, dlp: dlp}
}

func (c *ChainedAnonymizer) Scrub(ctx context.Context, result *mcp.CallToolResult) (*mcp.CallToolResult, error) {
	intermediate, err := c.local.Scrub(ctx, result)
	if err != nil {
		return nil, err
	}
	return c.dlp.Scrub(ctx, intermediate)
}

// Compile-time interface check.
var _ Anonymizer = (*ChainedAnonymizer)(nil)
