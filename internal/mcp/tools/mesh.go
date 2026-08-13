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

// GKEMeshTools provides an MCP tool for GKE service mesh topology.
// It queries Istio/ASM Cloud Monitoring metrics for caller→callee edges and
// falls back to Istio proxy access logs when metrics are unavailable.
type GKEMeshTools struct {
	svc ports.GKEMeshService
	log *slog.Logger
}

func NewGKEMeshTools(svc ports.GKEMeshService, log *slog.Logger) *GKEMeshTools {
	return &GKEMeshTools{svc: svc, log: log}
}

func (t *GKEMeshTools) Name() string { return "gke_mesh" }

func (t *GKEMeshTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.GetMeshTopology(),
	}
}

func (t *GKEMeshTools) GetMeshTopology() server.ServerTool {
	tool := mcp.NewTool("gcp_gke_get_mesh_topology",
		mcp.WithDescription(
			"Get service-to-service traffic edges for a GKE cluster from Anthos Service Mesh "+
				"(Istio) telemetry. Primary source: Cloud Monitoring istio.io/service/server/request_count "+
				"metric grouped by source and destination workload. Falls back to Istio proxy access "+
				"log entries when ASM metrics are unavailable. Returns caller/callee workload names, "+
				"namespace, and requests-per-minute. The 'backend' field in the response indicates "+
				"which data source was used ('istio_metrics', 'log_based', or 'none'). An empty edges "+
				"array with warnings means ASM is not installed or no traffic was observed.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("cluster_name", mcp.Required(), mcp.Description("GKE cluster name")),
		mcp.WithString("location", mcp.Required(), mcp.Description("GCP region or zone of the cluster")),
		mcp.WithNumber("lookback_hours", mcp.Description("Lookback window in hours (default 24, max 168)")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Get GKE Service Mesh Topology",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.getMeshTopologyHandler),
	}
}

func (t *GKEMeshTools) getMeshTopologyHandler(ctx context.Context, _ mcp.CallToolRequest, args models.GetGKEMeshTopologyRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_gke_get_mesh_topology",
		"project", args.ProjectID, "cluster", args.ClusterName,
		"location", args.Location, "lookback_hours", args.LookbackHours,
	)
	resp, err := t.svc.GetGKEMeshTopology(ctx, args)
	if err != nil {
		return handleServiceError("gcp_gke_get_mesh_topology", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_gke_get_mesh_topology: marshal: %w", err)
	}
	return result, nil
}
