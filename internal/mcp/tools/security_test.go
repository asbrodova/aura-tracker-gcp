package tools

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

type fakeSecurityAuditor struct {
	req models.SecurityAuditRequest
}

func (f *fakeSecurityAuditor) Audit(_ context.Context, req models.SecurityAuditRequest) (models.SecurityAuditReport, error) {
	f.req = req
	score := 88
	return models.SecurityAuditReport{ProjectID: req.ProjectID, Score: &score, SummaryMarkdown: "# Security posture: 88/100"}, nil
}

func TestProjectSecurityAuditReturnsStructuredAndFallbackContent(t *testing.T) {
	auditor := &fakeSecurityAuditor{}
	tools := NewSecurityTools(auditor, slog.Default())
	result, err := tools.projectSecurityAuditHandler(context.Background(), mcp.CallToolRequest{}, models.SecurityAuditRequest{ProjectID: "p1", Refresh: true})
	if err != nil {
		t.Fatal(err)
	}
	if auditor.req.ProjectID != "p1" || !auditor.req.Refresh {
		t.Fatalf("request not forwarded: %+v", auditor.req)
	}
	if result.StructuredContent == nil || len(result.Content) == 0 {
		t.Fatalf("expected structured and fallback content: %+v", result)
	}
}

func TestProjectSecurityAuditDescriptionRoutesNaturalLanguage(t *testing.T) {
	tool := NewSecurityTools(&fakeSecurityAuditor{}, slog.Default()).ProjectSecurityAudit().Tool
	for _, phrase := range []string{"I need a project audit", "audit my project", "security score", "cost-only"} {
		if !strings.Contains(tool.Description, phrase) {
			t.Errorf("tool description missing routing phrase %q", phrase)
		}
	}
}
