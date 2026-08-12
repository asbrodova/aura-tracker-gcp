package tools

import (
	"strings"
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func TestAuraSummaryPresentationUnavailable(t *testing.T) {
	aura, status := auraSummaryPresentation(models.AuraReport{
		Score:          0,
		Band:           models.AuraBandUnavailable,
		CoverageStatus: "unavailable",
		Reasons:        []string{"metrics fetch failed"},
	})
	if aura != "⚪ N/A" {
		t.Fatalf("aura = %q, want unavailable presentation", aura)
	}
	if !strings.HasPrefix(status, "Unavailable") || strings.Contains(status, "Critical") {
		t.Fatalf("status = %q, want unavailable and not critical", status)
	}
}

func TestAuraSummaryPresentationScored(t *testing.T) {
	aura, status := auraSummaryPresentation(models.AuraReport{
		Score: 42,
		Band:  models.AuraBandRed,
	})
	if aura != "🔴 42" || status != "Critical" {
		t.Fatalf("presentation = (%q, %q)", aura, status)
	}
}

func TestAuraDashboardInstructionExplainsUnavailable(t *testing.T) {
	if !strings.Contains(auraDashboardInstruction, "N/A") || !strings.Contains(auraDashboardInstruction, "Telemetry unavailable") {
		t.Fatal("dashboard instructions must keep unavailable resources out of score bands")
	}
}
