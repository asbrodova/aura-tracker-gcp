package anonymize

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

const maxDLPBytes = 500_000 // GCP DLP API hard limit is 524,288 bytes

// DLPAnonymizer implements Anonymizer using the GCP Data Loss Prevention API.
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

func (d *DLPAnonymizer) Scrub(ctx context.Context, result *mcp.CallToolResult) (*mcp.CallToolResult, error) {
	if result == nil {
		return nil, nil
	}

	reg := newTokenRegistry()
	var allFindings []Finding

	out := *result
	out.Content = make([]mcp.Content, len(result.Content))
	copy(out.Content, result.Content)

	for i, c := range out.Content {
		tc, ok := c.(mcp.TextContent)
		if !ok {
			continue
		}
		masked, findings, err := d.scrubString(ctx, tc.Text, reg, i)
		if err != nil {
			return nil, err
		}
		tc.Text = masked
		out.Content[i] = tc
		allFindings = append(allFindings, findings...)
	}

	if out.StructuredContent != nil {
		b, err := json.Marshal(out.StructuredContent)
		if err == nil {
			masked, findings, err := d.scrubString(ctx, string(b), reg, -1)
			if err != nil {
				return nil, err
			}
			allFindings = append(allFindings, findings...)
			var sc any
			if json.Unmarshal([]byte(masked), &sc) == nil {
				out.StructuredContent = sc
			}
		}
	}

	if d.auditOnly {
		return buildAuditResult(allFindings)
	}
	return &out, nil
}

func (d *DLPAnonymizer) scrubString(ctx context.Context, text string, reg *tokenRegistry, contentIdx int) (string, []Finding, error) {
	if text == "" {
		return text, nil, nil
	}
	if len(text) > maxDLPBytes {
		return text, nil, nil
	}
	resp, err := d.svc.InspectText(ctx, ports.DLPInspectRequest{
		Content:   text,
		InfoTypes: d.infoTypes,
		ProjectID: d.projectID,
	})
	if err != nil {
		return "", nil, fmt.Errorf("anonymize: dlp inspect: %w", err)
	}
	if len(resp.Findings) == 0 {
		return text, nil, nil
	}
	masked, findings := maskByOffsets(text, resp.Findings, reg)
	for i := range findings {
		findings[i].ContentIndex = contentIdx
	}
	return masked, findings, nil
}

// maskByOffsets replaces each DLP finding span in src with a stable token,
// processing findings from right to left so earlier byte offsets stay valid.
func maskByOffsets(src string, findings []models.DLPFinding, reg *tokenRegistry) (string, []Finding) {
	sorted := make([]models.DLPFinding, len(findings))
	copy(sorted, findings)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Offset > sorted[j].Offset
	})

	b := []byte(src)
	var out []Finding
	for _, f := range sorted {
		end := f.Offset + f.Length
		if f.Offset < 0 || end > len(b) {
			continue
		}
		key := f.Quote
		if key == "" {
			key = string(b[f.Offset:end])
		}
		token := reg.tokenFor(f.InfoType, key)
		b = append(b[:f.Offset], append([]byte(token), b[end:]...)...)
		out = append(out, Finding{
			PatternName: f.InfoType,
			MatchCount:  1,
		})
	}
	return string(b), out
}

// Compile-time interface check.
var _ Anonymizer = (*DLPAnonymizer)(nil)
