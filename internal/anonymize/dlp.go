package anonymize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

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
	if configured := strings.TrimSpace(cfg.DLP.ProjectID); configured != "" {
		projectID = configured
	}
	return &DLPAnonymizer{
		svc:       svc,
		infoTypes: cfg.DLP.InfoTypes,
		projectID: projectID,
		auditOnly: cfg.AuditOnly,
	}
}

func (d *DLPAnonymizer) Scrub(ctx context.Context, result *mcp.CallToolResult) (*mcp.CallToolResult, error) {
	out, findings, err := d.scrub(ctx, result)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, nil
	}
	if d.auditOnly {
		return buildAuditResult(findings)
	}
	return out, nil
}

func (d *DLPAnonymizer) scrub(ctx context.Context, result *mcp.CallToolResult) (*mcp.CallToolResult, []Finding, error) {
	if result == nil {
		return nil, nil, nil
	}

	reg := newTokenRegistry()
	var allFindings []Finding

	out := *result
	meta, findings, err := d.scrubMeta(ctx, result.Meta, reg)
	if err != nil {
		return nil, nil, err
	}
	out.Meta = meta
	allFindings = append(allFindings, findings...)
	out.Content = make([]mcp.Content, len(result.Content))

	for i, content := range result.Content {
		scrubbed, findings, scrubErr := d.scrubContent(ctx, content, reg, i)
		if scrubErr != nil {
			return nil, nil, scrubErr
		}
		out.Content[i] = scrubbed
		allFindings = append(allFindings, findings...)
	}

	if out.StructuredContent != nil {
		b, err := json.Marshal(out.StructuredContent)
		if err != nil {
			return nil, nil, fmt.Errorf("anonymize: marshal structured content: %w", err)
		}
		masked, findings, err := d.scrubString(ctx, string(b), reg, -1)
		if err != nil {
			return nil, nil, err
		}
		allFindings = append(allFindings, findings...)
		var sc any
		if err := json.Unmarshal([]byte(masked), &sc); err != nil {
			return nil, nil, fmt.Errorf("anonymize: unmarshal scrubbed structured content: %w", err)
		}
		out.StructuredContent = sc
	}
	return &out, allFindings, nil
}

func (d *DLPAnonymizer) scrubContent(ctx context.Context, content mcp.Content, reg *tokenRegistry, contentIdx int) (mcp.Content, []Finding, error) {
	if content == nil {
		return nil, nil, errors.New("anonymize: nil MCP content withheld")
	}
	var err error
	content, err = prepareScrubbableContent(content)
	if err != nil {
		return nil, nil, err
	}

	// Scrub textual payloads directly to preserve provider offsets, then scrub
	// the complete serialized envelope with the payload blanked. This covers all
	// metadata and link fields without inspecting the same text twice.
	var payload string
	var hasPayload bool
	switch typed := content.(type) {
	case mcp.TextContent:
		payload = typed.Text
		hasPayload = true
		typed.Text = ""
		content = typed
	case mcp.EmbeddedResource:
		if resource, ok := typed.Resource.(mcp.TextResourceContents); ok {
			payload = resource.Text
			hasPayload = true
			resource.Text = "__ANONYMIZE_TEXT_PAYLOAD__"
			typed.Resource = resource
			content = typed
		}
	}

	scrubbed, findings, err := d.scrubSerializedContent(ctx, content, reg, contentIdx)
	if err != nil {
		return nil, nil, err
	}
	if !hasPayload {
		return scrubbed, findings, nil
	}

	payload, payloadFindings, err := d.scrubString(ctx, payload, reg, contentIdx)
	if err != nil {
		return nil, nil, err
	}
	findings = append(findings, payloadFindings...)
	switch typed := scrubbed.(type) {
	case mcp.TextContent:
		typed.Text = payload
		return typed, findings, nil
	case mcp.EmbeddedResource:
		resource, ok := typed.Resource.(mcp.TextResourceContents)
		if !ok {
			return nil, nil, errors.New("anonymize: text resource changed type while scrubbing")
		}
		resource.Text = payload
		typed.Resource = resource
		return typed, findings, nil
	default:
		return nil, nil, errors.New("anonymize: content changed type while scrubbing")
	}
}

func (d *DLPAnonymizer) scrubSerializedContent(ctx context.Context, content mcp.Content, reg *tokenRegistry, contentIdx int) (mcp.Content, []Finding, error) {
	encoded, err := json.Marshal(content)
	if err != nil {
		return nil, nil, fmt.Errorf("anonymize: marshal MCP content: %w", err)
	}
	masked, findings, err := d.scrubString(ctx, string(encoded), reg, contentIdx)
	if err != nil {
		return nil, nil, err
	}
	scrubbed, err := parseScrubbedContent([]byte(masked))
	if err != nil {
		return nil, nil, fmt.Errorf("anonymize: decode scrubbed MCP content: %w", err)
	}
	return scrubbed, findings, nil
}

func (d *DLPAnonymizer) scrubMeta(ctx context.Context, meta *mcp.Meta, reg *tokenRegistry) (*mcp.Meta, []Finding, error) {
	if meta == nil {
		return nil, nil, nil
	}
	encoded, err := json.Marshal(meta)
	if err != nil {
		return nil, nil, fmt.Errorf("anonymize: marshal result metadata: %w", err)
	}
	masked, findings, err := d.scrubString(ctx, string(encoded), reg, -1)
	if err != nil {
		return nil, nil, err
	}
	var out mcp.Meta
	if err := json.Unmarshal([]byte(masked), &out); err != nil {
		return nil, nil, fmt.Errorf("anonymize: decode scrubbed result metadata: %w", err)
	}
	return &out, findings, nil
}

func (d *DLPAnonymizer) scrubString(ctx context.Context, text string, reg *tokenRegistry, contentIdx int) (string, []Finding, error) {
	if text == "" {
		return text, nil, nil
	}
	if len(text) > maxDLPBytes {
		return "", nil, fmt.Errorf("anonymize: DLP content exceeds %d-byte inspection limit", maxDLPBytes)
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
		if f.Quote != "" && string(b[f.Offset:end]) != f.Quote {
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
