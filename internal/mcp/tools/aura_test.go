package tools

import (
	"strings"
	"testing"
	"time"

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

func TestProjectAuraRecommenderNoteUsesLatestQuotaDeadline(t *testing.T) {
	earlier := time.Date(2026, time.August, 13, 1, 2, 3, 0, time.UTC)
	later := earlier.Add(45 * time.Minute)
	note := projectAuraRecommenderNote([]models.AuraReport{
		{RecommenderNote: "earlier note", RecommenderRetryAt: earlier},
		{RecommenderNote: "later note", RecommenderRetryAt: later},
	})

	if !strings.Contains(note, later.Format(time.RFC3339)) {
		t.Fatalf("note %q does not contain latest retry deadline", note)
	}
	if strings.Contains(note, earlier.Format(time.RFC3339)) {
		t.Fatalf("note %q misleadingly reports an earlier partial-recovery deadline", note)
	}
}

func TestProjectAuraRecommenderNotePreservesUndatedFallback(t *testing.T) {
	const fallback = "quota state has no known retry time"
	if got := projectAuraRecommenderNote([]models.AuraReport{{RecommenderNote: fallback}}); got != fallback {
		t.Fatalf("note = %q, want %q", got, fallback)
	}
}
