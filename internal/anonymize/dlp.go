package anonymize

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/asbrodova/aura-tracker-gcp/ports"
)

// DLPAnonymizer implements Anonymizer using the GCP Data Loss Prevention API.
// Phase 2: interface compliance is verified at compile time, but full
// implementation is deferred until cloud.google.com/go/dlp is added.
type DLPAnonymizer struct {
	svc       ports.DLPService
	infoTypes []string
	projectID string
	auditOnly bool
}

func NewDLPAnonymizer(svc ports.DLPService, cfg Config, projectID string) *DLPAnonymizer {
	return &DLPAnonymizer{
		svc:       svc,
		infoTypes: cfg.DLP.InfoTypes,
		projectID: projectID,
		auditOnly: cfg.AuditOnly,
	}
}

func (d *DLPAnonymizer) Scrub(_ context.Context, _ *mcp.CallToolResult) (*mcp.CallToolResult, error) {
	return nil, fmt.Errorf("anonymize: DLP backend not yet implemented (Phase 2)")
}

// Compile-time interface check.
var _ Anonymizer = (*DLPAnonymizer)(nil)
