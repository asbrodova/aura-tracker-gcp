package tools

import (
	"context"
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
	svc ports.ArchGraphService
	log *slog.Logger
}

func NewArchGraphTools(svc ports.ArchGraphService, log *slog.Logger) *ArchGraphTools {
	return &ArchGraphTools{svc: svc, log: log}
}

func (t *ArchGraphTools) Name() string { return "archgraph" }

func (t *ArchGraphTools) GetTools() []server.ServerTool {
	return []server.ServerTool{t.ExportArchitectureGraph()}
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
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
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
