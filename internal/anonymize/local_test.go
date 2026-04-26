package anonymize

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.TextContent{Type: "text", Text: s}},
	}
}

func resultText(r *mcp.CallToolResult) string {
	if len(r.Content) == 0 {
		return ""
	}
	tc, ok := r.Content[0].(mcp.TextContent)
	if !ok {
		return ""
	}
	return tc.Text
}

func newScrubber(t *testing.T, extra ...PatternConfig) *LocalScrubber {
	t.Helper()
	s, err := NewLocalScrubber(Config{Patterns: extra})
	if err != nil {
		t.Fatalf("NewLocalScrubber: %v", err)
	}
	return s
}

// --- plain-text tests ---

func TestScrubInternalIP(t *testing.T) {
	s := newScrubber(t)
	got, _ := s.Scrub(context.Background(), textResult("host is 192.168.1.1"))
	if strings.Contains(resultText(got), "192.168.1.1") {
		t.Error("internal IP not masked")
	}
	if !strings.Contains(resultText(got), "[INTERNAL_IP_1]") {
		t.Errorf("expected [INTERNAL_IP_1], got %q", resultText(got))
	}
}

func TestScrubPublicIP(t *testing.T) {
	s := newScrubber(t)
	got, _ := s.Scrub(context.Background(), textResult("server 8.8.8.8 responded"))
	if strings.Contains(resultText(got), "8.8.8.8") {
		t.Error("public IP not masked")
	}
}

func TestScrubEmail(t *testing.T) {
	s := newScrubber(t)
	got, _ := s.Scrub(context.Background(), textResult("contact admin@example.com for help"))
	if strings.Contains(resultText(got), "admin@example.com") {
		t.Error("email not masked")
	}
	if !strings.Contains(resultText(got), "[EMAIL_1]") {
		t.Errorf("expected [EMAIL_1], got %q", resultText(got))
	}
}

func TestScrubServiceAccount(t *testing.T) {
	s := newScrubber(t)
	sa := "myapp-svc@my-project-123.iam.gserviceaccount.com"
	got, _ := s.Scrub(context.Background(), textResult("sa: "+sa))
	if strings.Contains(resultText(got), sa) {
		t.Error("service account not masked")
	}
}

func TestScrubGCPAPIKey(t *testing.T) {
	s := newScrubber(t)
	key := "AIzaSyDdI0hCZtE6vySjMm-WEfRq3CPzqKqqsHI"
	got, _ := s.Scrub(context.Background(), textResult("key="+key))
	if strings.Contains(resultText(got), key) {
		t.Error("GCP API key not masked")
	}
}

// Same raw value → same token (stable across multiple occurrences).
func TestTokenStability(t *testing.T) {
	s := newScrubber(t)
	got, _ := s.Scrub(context.Background(), textResult("ip 192.168.1.1 and again 192.168.1.1"))
	text := resultText(got)
	if strings.Count(text, "[INTERNAL_IP_1]") != 2 {
		t.Errorf("expected [INTERNAL_IP_1] twice, got %q", text)
	}
}

// Different values get different indices.
func TestTokenIncrement(t *testing.T) {
	s := newScrubber(t)
	got, _ := s.Scrub(context.Background(), textResult("a=192.168.1.1 b=192.168.1.2"))
	text := resultText(got)
	if !strings.Contains(text, "[INTERNAL_IP_1]") || !strings.Contains(text, "[INTERNAL_IP_2]") {
		t.Errorf("expected two distinct tokens, got %q", text)
	}
}

// --- JSON walk tests ---

func TestScrubJSONStringValue(t *testing.T) {
	payload := `{"endpoint":"192.168.0.5","status":"ok"}`
	s := newScrubber(t)
	got, _ := s.Scrub(context.Background(), textResult(payload))
	text := resultText(got)
	if strings.Contains(text, "192.168.0.5") {
		t.Error("IP in JSON value not masked")
	}
	if !strings.Contains(text, "INTERNAL_IP") {
		t.Errorf("expected INTERNAL_IP token in JSON, got %q", text)
	}
	// "status" value should be unchanged
	if !strings.Contains(text, `"status":"ok"`) {
		t.Errorf("non-PII value mutated: %q", text)
	}
}

func TestScrubJSONNestedArray(t *testing.T) {
	payload := `{"clusters":[{"endpoint":"10.0.0.1"},{"endpoint":"10.0.0.2"}]}`
	s := newScrubber(t)
	got, _ := s.Scrub(context.Background(), textResult(payload))
	text := resultText(got)
	if strings.Contains(text, "10.0.0.1") || strings.Contains(text, "10.0.0.2") {
		t.Error("IPs in nested array not masked")
	}
}

func TestWhitelistedKeyNotMasked(t *testing.T) {
	payload := `{"cluster_name":"my-cluster","endpoint":"192.168.5.5"}`
	s, err := NewLocalScrubber(Config{JSONKeyWhitelist: []string{"cluster_name"}})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Scrub(context.Background(), textResult(payload))
	text := resultText(got)
	if !strings.Contains(text, `"cluster_name":"my-cluster"`) {
		t.Errorf("whitelisted key value was masked: %q", text)
	}
	if strings.Contains(text, "192.168.5.5") {
		t.Error("non-whitelisted IP not masked")
	}
}

func TestScrubIsErrorResult(t *testing.T) {
	r := mcp.NewToolResultError("permission denied on 192.168.1.1")
	s := newScrubber(t)
	got, _ := s.Scrub(context.Background(), r)
	text := resultText(got)
	if strings.Contains(text, "192.168.1.1") {
		t.Error("IP in error message not masked")
	}
	if !got.IsError {
		t.Error("IsError flag lost after scrub")
	}
}

func TestScrubNilResult(t *testing.T) {
	s := newScrubber(t)
	got, err := s.Scrub(context.Background(), nil)
	if err != nil || got != nil {
		t.Errorf("expected (nil, nil), got (%v, %v)", got, err)
	}
}

// --- custom pattern test ---

func TestCustomPattern(t *testing.T) {
	s, err := NewLocalScrubber(Config{
		Patterns: []PatternConfig{
			{Name: "ticket", Regex: `TICKET-\d+`, ReplacementTemplate: "[TICKET_${INDEX}]"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Scrub(context.Background(), textResult("see TICKET-1234"))
	text := resultText(got)
	if strings.Contains(text, "TICKET-1234") {
		t.Error("custom pattern not applied")
	}
	if !strings.Contains(text, "[TICKET_1]") {
		t.Errorf("expected [TICKET_1], got %q", text)
	}
}

// --- audit mode test ---

func TestAuditMode(t *testing.T) {
	s, err := NewLocalScrubber(Config{AuditOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Scrub(context.Background(), textResult("host 10.0.0.1 user admin@corp.com"))
	text := resultText(got)

	// Content should be an AuditReport, not the original text.
	if strings.Contains(text, "10.0.0.1") || strings.Contains(text, "admin@corp.com") {
		t.Error("raw PII visible in audit mode result")
	}
	var report AuditReport
	if err := json.Unmarshal([]byte(text), &report); err != nil {
		t.Fatalf("audit result is not valid JSON AuditReport: %v", err)
	}
	if report.TotalMatches == 0 {
		t.Error("expected non-zero TotalMatches in audit report")
	}
	if len(report.PatternsSeen) == 0 {
		t.Error("expected PatternsSeen to be populated")
	}
}
