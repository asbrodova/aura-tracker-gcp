package middleware

import (
	"context"
	"regexp"
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/internal/requestmeta"
)

func TestCorrelationIDDefaultsToUnknown(t *testing.T) {
	if got := CorrelationID(context.Background()); got != "unknown" {
		t.Fatalf("CorrelationID() = %q, want unknown", got)
	}
}

func TestWithCorrelationIDSetsShortHexID(t *testing.T) {
	ctx := WithCorrelationID(context.Background())
	id := CorrelationID(ctx)
	if !regexp.MustCompile(`^[0-9a-f]{8}$`).MatchString(id) {
		t.Fatalf("correlation ID = %q, want 8 hex characters", id)
	}
	if requestmeta.CorrelationID(ctx) != id {
		t.Fatal("correlation ID is not stored in requestmeta")
	}
}
