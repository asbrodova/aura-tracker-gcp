package diagram

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

const (
	// Graphviz is normally sub-second, but cold containers and race-instrumented
	// test runs can spend several seconds just starting the subprocess.
	svgRenderTimeout = 15 * time.Second
	maxSVGBytes      = 2 << 20
)

var errSVGTooLarge = errors.New("svg output exceeded 2 MiB")

type svgRenderer interface {
	Render(context.Context, string) (string, error)
}

type graphvizSVGRenderer struct {
	path string
}

func newGraphvizSVGRenderer(path string) *graphvizSVGRenderer {
	return &graphvizSVGRenderer{path: path}
}

func (r *graphvizSVGRenderer) Render(ctx context.Context, dot string) (string, error) {
	if strings.TrimSpace(r.path) == "" {
		return "", errSVGUnavailable
	}
	renderCtx, cancel := context.WithTimeout(ctx, svgRenderTimeout)
	defer cancel()

	cmd := exec.CommandContext(renderCtx, r.path, "-Tsvg") //nolint:gosec // executable is server configuration, never tool input
	cmd.Stdin = strings.NewReader(dot)
	var stdout limitedBuffer
	stdout.limit = maxSVGBytes
	var stderr limitedBuffer
	stderr.limit = 8 << 10
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if renderCtx.Err() != nil {
			return "", fmt.Errorf("graphviz timed out after %s", svgRenderTimeout)
		}
		if stdout.err != nil {
			return "", stdout.err
		}
		return "", fmt.Errorf("graphviz dot failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if stdout.err != nil {
		return "", stdout.err
	}
	return sanitizeSVG(stdout.String())
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
	err   error
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		b.err = errSVGTooLarge
		return 0, b.err
	}
	if len(p) > remaining {
		_, _ = b.Buffer.Write(p[:remaining])
		b.err = errSVGTooLarge
		return remaining, b.err
	}
	return b.Buffer.Write(p)
}

func sanitizeSVG(source string) (string, error) {
	start := strings.Index(strings.ToLower(source), "<svg")
	if start < 0 {
		return "", errors.New("graphviz returned no SVG root")
	}
	source = source[start:]
	decoder := xml.NewDecoder(strings.NewReader(source))
	rootSeen := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("invalid SVG XML: %w", err)
		}
		switch value := token.(type) {
		case xml.Directive:
			return "", errors.New("svg directives are not allowed")
		case xml.StartElement:
			name := strings.ToLower(value.Name.Local)
			if !rootSeen {
				if name != "svg" {
					return "", errors.New("first SVG element is not svg")
				}
				rootSeen = true
			}
			switch name {
			case "script", "foreignobject", "iframe", "image", "a":
				return "", fmt.Errorf("unsafe SVG element %q", name)
			}
			for _, attr := range value.Attr {
				attrName := strings.ToLower(attr.Name.Local)
				if strings.ToLower(attr.Name.Space) == "xmlns" || attrName == "xmlns" {
					continue
				}
				if strings.HasPrefix(attrName, "on") || attrName == "href" || attrName == "src" {
					return "", fmt.Errorf("unsafe SVG attribute %q", attrName)
				}
				attrValue := strings.ToLower(strings.TrimSpace(attr.Value))
				if strings.HasPrefix(attrValue, "javascript:") || strings.HasPrefix(attrValue, "vbscript:") || strings.HasPrefix(attrValue, "data:") || strings.HasPrefix(attrValue, "http:") || strings.HasPrefix(attrValue, "https:") {
					return "", fmt.Errorf("unsafe external SVG attribute value")
				}
			}
		}
	}
	if !rootSeen || !strings.Contains(strings.ToLower(source), "</svg>") {
		return "", errors.New("incomplete svg document")
	}
	return strings.TrimSpace(source), nil
}
