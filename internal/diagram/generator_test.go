package diagram

import (
	"context"
	"strings"
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

type fakeGraphSource struct {
	graph models.ServerlessGraph
	err   error
	req   models.ExportArchitectureGraphRequest
}

type fakeSVGRenderer struct {
	source string
	err    error
}

func (f fakeSVGRenderer) Render(_ context.Context, _ string) (string, error) {
	return f.source, f.err
}

func (f *fakeGraphSource) ExportArchitectureGraph(_ context.Context, req models.ExportArchitectureGraphRequest) (models.ServerlessGraph, error) {
	f.req = req
	return f.graph, f.err
}

func TestGeneratorDefaultsToProductionMermaid(t *testing.T) {
	source := &fakeGraphSource{graph: productionGraph()}
	generator := New(source, nil)

	got, err := generator.Generate(context.Background(), models.GenerateArchitectureDiagramRequest{ProjectID: "project"})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got.Status != models.DiagramStatusComplete {
		t.Fatalf("status = %q, want complete", got.Status)
	}
	if got.Format != models.DiagramFormatMermaid || !strings.HasPrefix(got.Source, "flowchart LR") {
		t.Fatalf("unexpected Mermaid response: %+v", got)
	}
	for _, want := range []string{"orders-api", "orders-db", "nightly"} {
		if !strings.Contains(got.Source, want) {
			t.Errorf("diagram missing %q:\n%s", want, got.Source)
		}
	}
	if strings.Contains(got.Source, "staging-api") {
		t.Fatalf("diagram included unrelated staging node:\n%s", got.Source)
	}
	if got.Scope.SeedCount != 1 || got.Scope.DependencyCount != 1 || got.Scope.EntrypointCount != 1 {
		t.Fatalf("unexpected scope counts: %+v", got.Scope)
	}
	if source.req.MaxNodes != 0 {
		t.Fatalf("generator must request an uncapped source graph, got max_nodes=%d", source.req.MaxNodes)
	}
}

func TestGeneratorReportsNeedsScope(t *testing.T) {
	source := &fakeGraphSource{graph: models.ServerlessGraph{Nodes: []models.GraphNode{{
		ID: "dev", Kind: models.KindCloudRunService, Name: "dev-api", Labels: map[string]string{"env": "development"},
	}}}}

	got, err := New(source, nil).Generate(context.Background(), models.GenerateArchitectureDiagramRequest{})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got.Status != models.DiagramStatusNeedsScope {
		t.Fatalf("status = %q, want needs_scope", got.Status)
	}
	if len(got.Scope.EnvironmentCandidates) != 1 || got.Scope.EnvironmentCandidates[0] != "development" {
		t.Fatalf("unexpected candidates: %+v", got.Scope.EnvironmentCandidates)
	}
	if got.Source != "" {
		t.Fatalf("needs_scope should not render a source: %q", got.Source)
	}
}

func TestGeneratorMarksCrossEnvironmentDependency(t *testing.T) {
	prod := models.GraphNode{ID: "prod", Kind: models.KindCloudRunService, Name: "prod-api", Labels: map[string]string{"env": "prod"}}
	stage := models.GraphNode{ID: "stage", Kind: models.KindCloudSQLInstance, Name: "stage-db", Labels: map[string]string{"environment": "staging"}}
	source := &fakeGraphSource{graph: models.ServerlessGraph{
		Nodes: []models.GraphNode{prod, stage},
		Edges: []models.GraphEdge{{Source: prod.ID, Target: stage.ID, Type: models.EdgeReadsFromDB, Confidence: 0.95}},
	}}

	got, err := New(source, nil).Generate(context.Background(), models.GenerateArchitectureDiagramRequest{})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got.Scope.CrossEnvironmentCount != 1 || !strings.Contains(got.Source, "cross-environment") {
		t.Fatalf("cross-environment dependency was not surfaced: %+v\n%s", got.Scope, got.Source)
	}
	if len(got.Warnings) == 0 {
		t.Fatal("expected cross-environment warning")
	}
}

func TestGraphvizIsDeterministicAndEscaped(t *testing.T) {
	graph := productionGraph()
	graph.Nodes[0].Name = "orders\"; URL=\"https://evil.example\n%%{init: bad}%%"
	source := &fakeGraphSource{graph: graph}
	request := models.GenerateArchitectureDiagramRequest{WholeProject: true, Format: models.DiagramFormatGraphviz}

	first, err := New(source, nil).Generate(context.Background(), request)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	second, err := New(source, nil).Generate(context.Background(), request)
	if err != nil {
		t.Fatalf("Generate() second error = %v", err)
	}
	if first.Source != second.Source {
		t.Fatal("Graphviz output is not deterministic")
	}
	if !strings.HasPrefix(first.Source, "digraph architecture") {
		t.Fatalf("unexpected Graphviz source:\n%s", first.Source)
	}
	if strings.Contains(first.Source, `URL="https://evil.example"`) {
		t.Fatalf("resource label escaped into a DOT URL attribute:\n%s", first.Source)
	}
}

func TestGeneratorCapsLargeViews(t *testing.T) {
	graph := models.ServerlessGraph{}
	for i := 0; i < 5; i++ {
		graph.Nodes = append(graph.Nodes, models.GraphNode{
			ID: string(rune('a' + i)), Kind: models.KindCloudRunService, Name: string(rune('a' + i)),
			Labels: map[string]string{"env": "production"},
		})
	}
	got, err := New(&fakeGraphSource{graph: graph}, nil).Generate(context.Background(), models.GenerateArchitectureDiagramRequest{MaxNodes: 3})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got.Stats.NodesRendered != 3 || got.Stats.NodesOmitted != 2 || len(got.Warnings) == 0 {
		t.Fatalf("unexpected capped response: %+v", got)
	}
}

func TestGeneratorReturnsSVGFromConfiguredRenderer(t *testing.T) {
	source := &fakeGraphSource{graph: productionGraph()}
	generator := New(source, nil)
	generator.svgRenderer = fakeSVGRenderer{source: `<svg xmlns="http://www.w3.org/2000/svg"></svg>`}

	got, err := generator.Generate(context.Background(), models.GenerateArchitectureDiagramRequest{Format: models.DiagramFormatSVG})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got.MIMEType != "image/svg+xml" || !strings.HasPrefix(got.Source, "<svg") {
		t.Fatalf("unexpected SVG response: %+v", got)
	}
}

func TestGeneratorReportsUnavailableSVGRenderer(t *testing.T) {
	t.Setenv("GRAPHVIZ_DOT_PATH", "")
	generator := &Generator{source: &fakeGraphSource{graph: productionGraph()}}
	_, err := generator.Generate(context.Background(), models.GenerateArchitectureDiagramRequest{Format: models.DiagramFormatSVG})
	if err == nil || !strings.Contains(err.Error(), "Graphviz") {
		t.Fatalf("expected Graphviz availability error, got %v", err)
	}
}

func productionGraph() models.ServerlessGraph {
	service := models.GraphNode{
		ID: "service", Kind: models.KindCloudRunService, Name: "orders-api", Region: "us-central1",
		Labels: map[string]string{"environment": "production"},
	}
	database := models.GraphNode{ID: "database", Kind: models.KindCloudSQLInstance, Name: "orders-db", Region: "us-central1"}
	scheduler := models.GraphNode{ID: "scheduler", Kind: models.KindSchedulerJob, Name: "nightly", Region: "us-central1"}
	staging := models.GraphNode{
		ID: "staging", Kind: models.KindCloudRunService, Name: "staging-api", Region: "us-central1",
		Labels: map[string]string{"environment": "staging"},
	}
	return models.ServerlessGraph{
		ProjectID: "project",
		Nodes:     []models.GraphNode{service, database, scheduler, staging},
		Edges: []models.GraphEdge{
			{Source: service.ID, Target: database.ID, Type: models.EdgeReadsFromDB, Confidence: 0.95},
			{Source: scheduler.ID, Target: service.ID, Type: models.EdgeTriggers, Confidence: 0.95},
		},
	}
}
