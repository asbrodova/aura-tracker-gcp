package gcp

import (
	"log/slog"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestAdapterCloseIsIdempotent(t *testing.T) {
	adapter := &gcpAdapter{}
	if err := adapter.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestValidateAdapterSettings(t *testing.T) {
	newValid := func() *gcpAdapter {
		return &gcpAdapter{
			limiter: rate.NewLimiter(1, 1), callTimeout: time.Second, graphTimeout: time.Second,
			traceBackend: "trace", log: slog.Default(), enabledModules: map[string]bool{"gke": true},
		}
	}
	valid := newValid()
	if err := validateAdapterSettings(valid, "valid-project"); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*gcpAdapter)
	}{
		{"invalid rate", func(a *gcpAdapter) { a.limiter = rate.NewLimiter(0, 0) }},
		{"invalid timeout", func(a *gcpAdapter) { a.callTimeout = 0 }},
		{"invalid trace backend", func(a *gcpAdapter) { a.traceBackend = "typo" }},
		{"unknown module", func(a *gcpAdapter) { a.enabledModules = map[string]bool{"typo": true} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := newValid()
			test.mutate(candidate)
			if err := validateAdapterSettings(candidate, "valid-project"); err == nil {
				t.Fatal("invalid settings were accepted")
			}
		})
	}
	if err := validateAdapterSettings(valid, "bad"); err == nil {
		t.Fatal("invalid project ID was accepted")
	}
}
