package diagram

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeSVGAcceptsGraphvizShape(t *testing.T) {
	input := `<?xml version="1.0"?><!DOCTYPE svg><svg xmlns="http://www.w3.org/2000/svg" width="10"><g><title>n1</title><path d="M0 0"/><text>api</text></g></svg>`
	got, err := sanitizeSVG(input)
	if err != nil {
		t.Fatalf("sanitizeSVG() error = %v", err)
	}
	if !strings.HasPrefix(got, "<svg") || strings.Contains(got, "DOCTYPE") {
		t.Fatalf("unexpected sanitized SVG: %s", got)
	}
}

func TestSanitizeSVGRejectsActiveOrExternalContent(t *testing.T) {
	for name, input := range map[string]string{
		"script":   `<svg><script>alert(1)</script></svg>`,
		"handler":  `<svg><g onclick="alert(1)"/></svg>`,
		"external": `<svg><use href="https://evil.example/image.svg"/></svg>`,
		"image":    `<svg><image/></svg>`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := sanitizeSVG(input); err == nil {
				t.Fatalf("sanitizeSVG() accepted unsafe input: %s", input)
			}
		})
	}
}

func TestLimitedBufferRejectsOversizedOutput(t *testing.T) {
	buffer := &limitedBuffer{limit: 3}
	if _, err := buffer.Write([]byte("abcd")); err == nil || !strings.Contains(err.Error(), "2 MiB") {
		t.Fatalf("expected size error, got %v", err)
	}
}

func TestGraphvizSVGRendererRunsConfiguredExecutable(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-dot")
	contents := "#!/bin/sh\ncat >/dev/null\nprintf '%s' '<svg xmlns=\"http://www.w3.org/2000/svg\"><text>ok</text></svg>'\n"
	if err := os.WriteFile(script, []byte(contents), 0o700); err != nil {
		t.Fatalf("write fake dot: %v", err)
	}
	got, err := newGraphvizSVGRenderer(script).Render(context.Background(), "digraph { a -> b }")
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !strings.Contains(got, "<text>ok</text>") {
		t.Fatalf("unexpected SVG: %s", got)
	}
}
