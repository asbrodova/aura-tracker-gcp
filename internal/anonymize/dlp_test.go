package anonymize

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

// mockDLPService implements ports.DLPService for testing.
type mockDLPService struct {
	resp ports.DLPInspectResponse
	err  error
}

func (m *mockDLPService) InspectText(_ context.Context, _ ports.DLPInspectRequest) (ports.DLPInspectResponse, error) {
	return m.resp, m.err
}

func dlpTextResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{mcp.TextContent{Text: s}}}
}

func dlpGetText(r *mcp.CallToolResult) string {
	if tc, ok := r.Content[0].(mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

func newDLP(svc ports.DLPService, auditOnly bool) *DLPAnonymizer {
	return NewDLPAnonymizer(svc, Config{AuditOnly: auditOnly}, "proj")
}

// --- maskByOffsets unit tests ---

func TestMaskByOffsets_Single(t *testing.T) {
	findings := []models.DLPFinding{{InfoType: "EMAIL_ADDRESS", Offset: 5, Length: 9, Quote: "a@b.com00"}}
	reg := newTokenRegistry()
	got, _ := maskByOffsets("hello a@b.com00 world", findings, reg)
	if !strings.Contains(got, "[EMAIL_ADDRESS_1]") {
		t.Errorf("expected token in output, got %q", got)
	}
	if strings.Contains(got, "a@b.com00") {
		t.Errorf("original value should be replaced, got %q", got)
	}
}

func TestMaskByOffsets_TwoNonAdjacent(t *testing.T) {
	src := "ip=1.2.3.4 email=a@b.com"
	findings := []models.DLPFinding{
		{InfoType: "IP_ADDRESS", Offset: 3, Length: 7, Quote: "1.2.3.4"},
		{InfoType: "EMAIL_ADDRESS", Offset: 17, Length: 7, Quote: "a@b.com"},
	}
	reg := newTokenRegistry()
	got, _ := maskByOffsets(src, findings, reg)
	if strings.Contains(got, "1.2.3.4") || strings.Contains(got, "a@b.com") {
		t.Errorf("original values should be replaced, got %q", got)
	}
	if !strings.Contains(got, "[IP_ADDRESS_1]") || !strings.Contains(got, "[EMAIL_ADDRESS_1]") {
		t.Errorf("both tokens expected, got %q", got)
	}
}

func TestMaskByOffsets_TokenStability(t *testing.T) {
	src := "a@b.com and a@b.com"
	findings := []models.DLPFinding{
		{InfoType: "EMAIL_ADDRESS", Offset: 0, Length: 7, Quote: "a@b.com"},
		{InfoType: "EMAIL_ADDRESS", Offset: 12, Length: 7, Quote: "a@b.com"},
	}
	reg := newTokenRegistry()
	got, _ := maskByOffsets(src, findings, reg)
	// Both occurrences of the same value must get the same token index.
	if strings.Count(got, "[EMAIL_ADDRESS_1]") != 2 {
		t.Errorf("expected two [EMAIL_ADDRESS_1] tokens, got %q", got)
	}
}

func TestMaskByOffsets_TokenIncrement(t *testing.T) {
	src := "a@b.com and c@d.com"
	findings := []models.DLPFinding{
		{InfoType: "EMAIL_ADDRESS", Offset: 0, Length: 7, Quote: "a@b.com"},
		{InfoType: "EMAIL_ADDRESS", Offset: 12, Length: 7, Quote: "c@d.com"},
	}
	reg := newTokenRegistry()
	got, _ := maskByOffsets(src, findings, reg)
	if !strings.Contains(got, "[EMAIL_ADDRESS_1]") || !strings.Contains(got, "[EMAIL_ADDRESS_2]") {
		t.Errorf("expected _1 and _2 tokens, got %q", got)
	}
}

func TestMaskByOffsets_FallbackQuote(t *testing.T) {
	src := "hello world"
	findings := []models.DLPFinding{{InfoType: "X", Offset: 6, Length: 5, Quote: ""}}
	reg := newTokenRegistry()
	got, _ := maskByOffsets(src, findings, reg)
	if strings.Contains(got, "world") {
		t.Errorf("value should be masked even without Quote, got %q", got)
	}
}

func TestMaskByOffsets_OutOfBoundsGuard(t *testing.T) {
	src := "short"
	findings := []models.DLPFinding{{InfoType: "X", Offset: 100, Length: 5}}
	reg := newTokenRegistry()
	got, _ := maskByOffsets(src, findings, reg)
	if got != "short" {
		t.Errorf("out-of-bounds finding should be skipped, got %q", got)
	}
}

// --- DLPAnonymizer.Scrub tests ---

func TestDLPAnonymizer_NoFindings(t *testing.T) {
	svc := &mockDLPService{resp: ports.DLPInspectResponse{}}
	d := newDLP(svc, false)
	in := dlpTextResult("hello world")
	out, err := d.Scrub(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if dlpGetText(out) != "hello world" {
		t.Errorf("content should be unchanged, got %q", dlpGetText(out))
	}
}

func TestDLPAnonymizer_SingleFinding(t *testing.T) {
	svc := &mockDLPService{resp: ports.DLPInspectResponse{
		Findings: []models.DLPFinding{{InfoType: "EMAIL_ADDRESS", Offset: 6, Length: 7, Quote: "a@b.com"}},
	}}
	d := newDLP(svc, false)
	out, err := d.Scrub(context.Background(), dlpTextResult("hello a@b.com!"))
	if err != nil {
		t.Fatal(err)
	}
	got := dlpGetText(out)
	if strings.Contains(got, "a@b.com") {
		t.Errorf("email should be masked, got %q", got)
	}
	if !strings.Contains(got, "[EMAIL_ADDRESS_1]") {
		t.Errorf("expected token, got %q", got)
	}
}

func TestDLPAnonymizer_AuditOnly(t *testing.T) {
	svc := &mockDLPService{resp: ports.DLPInspectResponse{
		Findings: []models.DLPFinding{{InfoType: "EMAIL_ADDRESS", Offset: 0, Length: 7, Quote: "a@b.com"}},
	}}
	d := newDLP(svc, true)
	out, err := d.Scrub(context.Background(), dlpTextResult("a@b.com"))
	if err != nil {
		t.Fatal(err)
	}
	raw := dlpGetText(out)
	var report AuditReport
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		t.Fatalf("expected AuditReport JSON, got %q: %v", raw, err)
	}
	if report.TotalMatches != 1 {
		t.Errorf("TotalMatches = %d, want 1", report.TotalMatches)
	}
	if len(report.PatternsSeen) == 0 || report.PatternsSeen[0] != "EMAIL_ADDRESS" {
		t.Errorf("PatternsSeen = %v, want [EMAIL_ADDRESS]", report.PatternsSeen)
	}
}

func TestDLPAnonymizer_InspectError(t *testing.T) {
	svc := &mockDLPService{err: context.DeadlineExceeded}
	d := newDLP(svc, false)
	_, err := d.Scrub(context.Background(), dlpTextResult("some text"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDLPAnonymizer_NilInput(t *testing.T) {
	svc := &mockDLPService{}
	d := newDLP(svc, false)
	out, err := d.Scrub(context.Background(), nil)
	if err != nil || out != nil {
		t.Errorf("nil input: got (%v, %v), want (nil, nil)", out, err)
	}
}

func TestDLPAnonymizer_OversizedContent(t *testing.T) {
	svc := &mockDLPService{} // InspectText should NOT be called
	d := newDLP(svc, false)
	big := strings.Repeat("x", maxDLPBytes+1)
	out, err := d.Scrub(context.Background(), dlpTextResult(big))
	if err != nil {
		t.Fatal(err)
	}
	if dlpGetText(out) != big {
		t.Error("oversized content should be returned unmodified")
	}
}

func TestDLPAnonymizer_StructuredContent(t *testing.T) {
	// {"email":"a@b.com"} — value starts at byte offset 10
	svc := &mockDLPService{resp: ports.DLPInspectResponse{
		Findings: []models.DLPFinding{{InfoType: "EMAIL_ADDRESS", Offset: 10, Length: 7, Quote: "a@b.com"}},
	}}
	d := newDLP(svc, false)
	result := &mcp.CallToolResult{
		StructuredContent: map[string]any{"email": "a@b.com"},
	}
	out, err := d.Scrub(context.Background(), result)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out.StructuredContent)
	if strings.Contains(string(b), "a@b.com") {
		t.Errorf("structured content email should be masked, got %s", b)
	}
}

// --- ChainedAnonymizer test ---

func TestChainedAnonymizer(t *testing.T) {
	// Local scrubber masks IPs; mock DLP returns an email finding.
	local, err := NewLocalScrubber(Config{AuditOnly: false})
	if err != nil {
		t.Fatal(err)
	}
	svc := &mockDLPService{resp: ports.DLPInspectResponse{
		Findings: []models.DLPFinding{{InfoType: "EMAIL_ADDRESS", Offset: 13, Length: 7, Quote: "a@b.com"}},
	}}
	dlpAnon := newDLP(svc, false)
	chained := NewChainedAnonymizer(local, dlpAnon)

	// Input has both an IP (caught by local) and email (caught by DLP mock).
	in := dlpTextResult("ip=10.0.0.1 em=a@b.com")
	out, err := chained.Scrub(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	got := dlpGetText(out)
	if strings.Contains(got, "10.0.0.1") {
		t.Errorf("IP should be masked by local scrubber, got %q", got)
	}
	if strings.Contains(got, "a@b.com") {
		t.Errorf("email should be masked by DLP, got %q", got)
	}
}
