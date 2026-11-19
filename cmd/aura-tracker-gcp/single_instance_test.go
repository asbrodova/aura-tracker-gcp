package main

import (
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
)

func envFrom(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestSSEStateNoticeOutsideCloudRun(t *testing.T) {
	level, message, attrs := sseStateNotice(envFrom(nil))
	if level != slog.LevelInfo || len(attrs) != 0 {
		t.Fatalf("level=%v attrs=%v, want an info notice without attributes", level, attrs)
	}
	if !strings.Contains(message, "exactly one instance") {
		t.Fatalf("message %q does not state the single-instance requirement", message)
	}
}

func TestSSEStateNoticeWarnsOnCloudRun(t *testing.T) {
	level, _, attrs := sseStateNotice(envFrom(map[string]string{"K_SERVICE": "aura", "K_REVISION": "aura-00007"}))
	if level != slog.LevelWarn {
		t.Fatalf("level = %v, want warn on Cloud Run", level)
	}
	for _, want := range []any{"aura", "aura-00007", sseCloudRunFlags} {
		if !slices.Contains(attrs, want) {
			t.Errorf("attrs %v are missing %v", attrs, want)
		}
	}
}

// The flags named in the startup warning must match the documented recipe.
func TestCloudRunRecipeUsesTheRequiredFlags(t *testing.T) {
	raw, err := os.ReadFile("../../deploy/cloud-run-sse.yaml.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	recipe := string(raw)
	for _, want := range []string{
		`autoscaling.knative.dev/maxScale: "1"`,
		`run.googleapis.com/sessionAffinity: "true"`,
		`timeoutSeconds: 3600`,
		`MUTATIONS_ENABLED`,
		`MCP_AUTH_MODE`,
	} {
		if !strings.Contains(recipe, want) {
			t.Errorf("deploy/cloud-run-sse.yaml.tmpl is missing %q", want)
		}
	}
	for _, flag := range strings.Fields(sseCloudRunFlags) {
		if !strings.HasPrefix(flag, "--") {
			t.Errorf("unexpected flag %q", flag)
		}
	}
}
