package gcp

import (
	"errors"
	"testing"

	"google.golang.org/api/iterator"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func TestCoverageSignalPresence(t *testing.T) {
	sentinel := errors.New("permission denied")
	tests := []struct {
		name    string
		err     error
		present bool
		wantErr error
	}{
		{name: "first result", present: true},
		{name: "empty inventory", err: iterator.Done},
		{name: "API error", err: sentinel, wantErr: sentinel},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			present, err := coverageSignalPresence(test.err)
			if present != test.present {
				t.Fatalf("present = %v, want %v", present, test.present)
			}
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestBuildRecommendationsDoesNotTreatUnavailableSignalsAsAbsent(t *testing.T) {
	coverage := models.NodeCoverage{
		NodeName: "checkout",
		Coverage: models.ObservabilityBlock{
			UnavailableSignals: []string{"metrics", "traces", "alerts"},
		},
	}
	if got := buildRecommendations([]models.NodeCoverage{coverage}); len(got) != 0 {
		t.Fatalf("recommendations = %v, want none for unavailable signals", got)
	}
}
