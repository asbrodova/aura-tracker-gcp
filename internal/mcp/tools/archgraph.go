package tools

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

// ArchGraphTools provides the MCP tool definition and handler for the master
// architecture graph export.
type ArchGraphTools struct {
	svc       ports.ArchGraphService
	generator DiagramGenerator
	log       *slog.Logger
}

// DiagramGenerator is implemented by the pure internal/diagram application
// layer. Keeping the interface here prevents tool handlers from depending on a
// concrete renderer implementation.
type DiagramGenerator interface {
	Generate(context.Context, models.GenerateArchitectureDiagramRequest) (models.ArchitectureDiagramResponse, error)
}

func NewArchGraphTools(svc ports.ArchGraphService, generator DiagramGenerator, log *slog.Logger) *ArchGraphTools {
	return &ArchGraphTools{svc: svc, generator: generator, log: log}
}

func (t *ArchGraphTools) Name() string { return "archgraph" }

func (t *ArchGraphTools) GetTools() []server.ServerTool {
	return []server.ServerTool{t.ExportArchitectureGraph(), t.GenerateArchitectureDiagram()}
}

func (t *ArchGraphTools) GenerateArchitectureDiagram() server.ServerTool {
	tool := mcp.NewTool("gcp_generate_architecture_diagram",
		mcp.WithDescription(
			"Generate a display-ready architecture diagram from live GCP resources. Use this tool when the user asks to show, draw, visualize, or diagram their production architecture. "+
				"Defaults to a production-scoped Mermaid diagram; can return Graphviz DOT or SVG. Production is inferred only from env/environment/stage labels and exact prod/production GKE namespaces, then connected entrypoints and dependencies are included.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("environment", mcp.Description("Environment to diagram (default: production)."), mcp.DefaultString("production")),
		mcp.WithBoolean("whole_project", mcp.Description("Include every environment instead of selecting an environment (default: false).")),
		mcp.WithString("format", mcp.Description("Output format (default: mermaid). Graphviz returns DOT source."), mcp.Enum("mermaid", "graphviz", "svg"), mcp.DefaultString("mermaid")),
		mcp.WithArray("regions", mcp.Description("Optional GCP regions to include."), mcp.WithStringItems(), mcp.UniqueItems(true)),
		mcp.WithString("direction", mcp.Description("Diagram direction (default: LR)."), mcp.Enum("LR", "TB"), mcp.DefaultString("LR")),
		mcp.WithString("group_by", mcp.Description("Grouping strategy (default: auto)."), mcp.Enum("auto", "region", "cluster", "namespace", "none"), mcp.DefaultString("auto")),
		mcp.WithNumber("max_depth", mcp.Description("Relationship traversal depth from environment seeds (default: 2)."), mcp.Min(1), mcp.Max(10)),
		mcp.WithNumber("max_nodes", mcp.Description("Maximum nodes rendered (default: 80, hard maximum: 200)."), mcp.Min(1), mcp.Max(200)),
		mcp.WithNumber("min_confidence", mcp.Description("Minimum inferred-edge confidence from 0 to 1 (default: 0.65)."), mcp.Min(0), mcp.Max(1)),
		mcp.WithBoolean("include_external", mcp.Description("Include inferred external endpoints (default: false).")),
		mcp.WithNumber("lookback_hours", mcp.Description("Trace and mesh lookback window (default: 168, maximum: 720)."), mcp.Min(1), mcp.Max(720)),
		mcp.WithOutputSchema[models.ArchitectureDiagramResponse](),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Generate Architecture Diagram",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool: tool,
		Handler: mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, args models.GenerateArchitectureDiagramRequest) (*mcp.CallToolResult, error) {
			t.log.InfoContext(ctx, "gcp_generate_architecture_diagram",
				"project", args.ProjectID,
				"environment", args.Environment,
				"format", args.Format,
			)
			response, err := t.generator.Generate(ctx, args)
			if err != nil {
				var clientError interface{ ClientMessage() string }
				if errors.As(err, &clientError) {
					return mcp.NewToolResultError(clientError.ClientMessage()), nil
				}
				return handleServiceError("gcp_generate_architecture_diagram", err)
			}
			return architectureDiagramResult(response), nil
		}),
	}
}

func architectureDiagramResult(response models.ArchitectureDiagramResponse) *mcp.CallToolResult {
	if response.Format == models.DiagramFormatSVG && response.Source != "" {
		digest := sha256.Sum256([]byte(response.Source))
		resource := mcp.TextResourceContents{
			URI:      fmt.Sprintf("diagram://architecture/%x.svg", digest[:8]),
			MIMEType: "image/svg+xml",
			Text:     response.Source,
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent("Generated an SVG architecture diagram."),
				mcp.NewEmbeddedResource(resource),
			},
			StructuredContent: response,
		}
	}
	var text string
	if response.Source != "" {
		language := response.Format
		if language == models.DiagramFormatGraphviz {
			language = "dot"
		}
		text = "```" + language + "\n" + response.Source + "\n```"
	} else if len(response.Warnings) > 0 {
		text = response.Warnings[0]
	} else {
		text = "No architecture diagram was generated."
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{mcp.NewTextContent(text)},
		StructuredContent: response,
	}
}

func (t *ArchGraphTools) ExportArchitectureGraph() server.ServerTool {
	tool := mcp.NewTool("gcp_export_architecture_graph",
		mcp.WithDescription(
			"Export a full project-wide architecture graph fusing Phase 1 (serverless/event-driven) "+
				"and Phase 2 (GKE workloads, networking, data stores, supply chain, IAM, observability) resources. "+
				"Runs a 3-batch parallel fan-out across all GCP listers, infers service-to-service edges via "+
				"the resolution engine (K8s spec, IAM bindings, mesh telemetry, Cloud Trace spans), "+
				"and returns a single diagram-ready JSON with nodes, edges, and groups. "+
				"Results are cached for 5 minutes per project+region combination. "+
				"WARNING: full scans of large projects (5+ clusters, 50+ services) may take 40–60 seconds.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithNumber("max_nodes",
			mcp.Description("Hard cap on the number of nodes returned (0 = unlimited)."),
			mcp.Min(0),
		),
		mcp.WithNumber("lookback_hours",
			mcp.Description("Trace and mesh telemetry lookback window in hours (1–720, default 168)."),
			mcp.Min(1),
			mcp.Max(720),
		),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Export Architecture Graph",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool: tool,
		Handler: mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, args models.ExportArchitectureGraphRequest) (*mcp.CallToolResult, error) {
			t.log.InfoContext(ctx, "gcp_export_architecture_graph",
				"project", args.ProjectID,
				"max_nodes", args.MaxNodes,
				"lookback_hours", args.LookbackHours,
			)
			resp, err := t.svc.ExportArchitectureGraph(ctx, args)
			if err != nil {
				return handleServiceError("gcp_export_architecture_graph", err)
			}
			result, err := mcp.NewToolResultJSON(resp)
			if err != nil {
				return nil, fmt.Errorf("gcp_export_architecture_graph: marshal: %w", err)
			}
			return result, nil
		}),
	}
}
