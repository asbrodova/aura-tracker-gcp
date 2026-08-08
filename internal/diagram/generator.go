// Package diagram scopes architecture graphs and renders them into portable
// diagram formats. It is deterministic and independent of GCP SDK types.
package diagram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

const (
	defaultEnvironment   = "production"
	defaultMaxDepth      = 2
	defaultMaxNodes      = 80
	hardMaxNodes         = 200
	defaultMinConfidence = 0.65
)

var errUnsupportedFormat = errors.New("unsupported diagram format")

var errSVGUnavailable = errors.New("svg rendering requires the Graphviz dot executable")

type userError struct {
	message string
}

func (e *userError) Error() string         { return e.message }
func (e *userError) ClientMessage() string { return e.message }

func userErrorf(format string, args ...any) error {
	return &userError{message: fmt.Sprintf(format, args...)}
}

// GraphSource is the narrow graph-export dependency required by Generator.
type GraphSource interface {
	ExportArchitectureGraph(context.Context, models.ExportArchitectureGraphRequest) (models.ServerlessGraph, error)
}

type Generator struct {
	source      GraphSource
	svgRenderer svgRenderer
	log         *slog.Logger
}

// New constructs a deterministic architecture diagram generator.
func New(source GraphSource, log *slog.Logger) *Generator {
	if log == nil {
		log = slog.Default()
	}
	generator := &Generator{source: source, log: log}
	path := strings.TrimSpace(os.Getenv("GRAPHVIZ_DOT_PATH"))
	if path == "" {
		path, _ = exec.LookPath("dot")
	}
	if path != "" {
		generator.svgRenderer = newGraphvizSVGRenderer(path)
	}
	return generator
}

// Generate discovers, scopes, and renders an architecture graph.
func (g *Generator) Generate(ctx context.Context, request models.GenerateArchitectureDiagramRequest) (models.ArchitectureDiagramResponse, error) {
	if g == nil || g.source == nil {
		return models.ArchitectureDiagramResponse{}, errors.New("diagram: graph source is required")
	}
	req, err := normalizeRequest(request)
	if err != nil {
		return models.ArchitectureDiagramResponse{}, err
	}

	graph, err := g.source.ExportArchitectureGraph(ctx, models.ExportArchitectureGraphRequest{
		ProjectID:       req.ProjectID,
		Regions:         req.Regions,
		IncludeExternal: req.IncludeExternal,
		LookbackHours:   req.LookbackHours,
	})
	if err != nil {
		return models.ArchitectureDiagramResponse{}, err
	}

	view := buildView(graph, req)
	response := models.ArchitectureDiagramResponse{
		Status:           models.DiagramStatusComplete,
		Format:           req.Format,
		MIMEType:         mimeType(req.Format),
		Scope:            view.scope,
		Stats:            view.stats,
		Warnings:         append([]string(nil), view.warnings...),
		CollectionErrors: append([]models.ToolError(nil), graph.Errors...),
		GeneratedAt:      graph.GeneratedAt,
	}
	if len(graph.Errors) > 0 {
		response.Status = models.DiagramStatusPartial
	}
	if view.needsScope {
		response.Status = models.DiagramStatusNeedsScope
		response.Warnings = append(response.Warnings, "No production resources were identified from env, environment, or stage labels, or exact prod/production namespaces.")
		return response, nil
	}
	if len(view.nodes) == 0 {
		response.Status = models.DiagramStatusEmpty
		response.Warnings = append(response.Warnings, "No resources matched the requested diagram scope.")
		return response, nil
	}

	switch req.Format {
	case models.DiagramFormatMermaid:
		response.Source = renderMermaid(view, req)
	case models.DiagramFormatGraphviz:
		response.Source = renderGraphviz(view, req)
	case models.DiagramFormatSVG:
		if g.svgRenderer == nil {
			return models.ArchitectureDiagramResponse{}, userErrorf("%v; install Graphviz, set GRAPHVIZ_DOT_PATH, or request Mermaid/Graphviz source", errSVGUnavailable)
		}
		response.Source, err = g.svgRenderer.Render(ctx, renderGraphviz(view, req))
		if err != nil {
			return models.ArchitectureDiagramResponse{}, fmt.Errorf("render SVG: %w", err)
		}
	default:
		return models.ArchitectureDiagramResponse{}, userErrorf("%v %q; choose mermaid, graphviz, or svg", errUnsupportedFormat, req.Format)
	}

	g.log.InfoContext(ctx, "architecture diagram generated",
		"format", req.Format,
		"environment", req.Environment,
		"whole_project", req.WholeProject,
		"nodes_discovered", response.Stats.NodesDiscovered,
		"nodes_rendered", response.Stats.NodesRendered,
		"edges_rendered", response.Stats.EdgesRendered,
		"partial_errors", len(response.CollectionErrors),
	)
	return response, nil
}

func normalizeRequest(req models.GenerateArchitectureDiagramRequest) (models.GenerateArchitectureDiagramRequest, error) {
	if req.Environment == "" {
		req.Environment = defaultEnvironment
	}
	req.Environment = strings.ToLower(strings.TrimSpace(req.Environment))
	if req.Format == "" {
		req.Format = models.DiagramFormatMermaid
	}
	req.Format = strings.ToLower(strings.TrimSpace(req.Format))
	if req.Direction == "" {
		req.Direction = "LR"
	}
	req.Direction = strings.ToUpper(strings.TrimSpace(req.Direction))
	if req.Direction != "LR" && req.Direction != "TB" {
		return req, userErrorf("direction must be LR or TB")
	}
	if req.GroupBy == "" {
		req.GroupBy = "auto"
	}
	req.GroupBy = strings.ToLower(strings.TrimSpace(req.GroupBy))
	switch req.GroupBy {
	case "auto", "region", "cluster", "namespace", "none":
	default:
		return req, userErrorf("group_by must be auto, region, cluster, namespace, or none")
	}
	if req.MaxDepth <= 0 {
		req.MaxDepth = defaultMaxDepth
	}
	if req.MaxDepth > 10 {
		return req, userErrorf("max_depth must be between 1 and 10")
	}
	if req.MaxNodes <= 0 {
		req.MaxNodes = defaultMaxNodes
	}
	if req.MaxNodes > hardMaxNodes {
		req.MaxNodes = hardMaxNodes
	}
	if req.MinConfidence <= 0 {
		req.MinConfidence = defaultMinConfidence
	}
	if req.MinConfidence > 1 {
		return req, userErrorf("min_confidence must be between 0 and 1")
	}
	if req.LookbackHours <= 0 {
		req.LookbackHours = 168
	}
	if req.LookbackHours > 720 {
		return req, userErrorf("lookback_hours must be between 1 and 720")
	}
	sort.Strings(req.Regions)
	return req, nil
}

func mimeType(format string) string {
	switch format {
	case models.DiagramFormatMermaid:
		return "text/x-mermaid"
	case models.DiagramFormatGraphviz:
		return "text/vnd.graphviz"
	case models.DiagramFormatSVG:
		return "image/svg+xml"
	default:
		return "text/plain"
	}
}
