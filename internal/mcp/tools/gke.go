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

// GKETools provides MCP tool definitions and handlers for GKE operations.
type GKETools struct {
	svc ports.GCPService
	log *slog.Logger
}

func NewGKETools(svc ports.GCPService, log *slog.Logger) *GKETools {
	return &GKETools{svc: svc, log: log}
}

func (t *GKETools) ListClusters() server.ServerTool {
	tool := mcp.NewTool("gcp_gke_list_clusters",
		mcp.WithDescription("List all GKE clusters in a GCP project and location"),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("location", mcp.Required(), mcp.Description("GCP region, zone, or '-' for all locations")),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listClustersHandler),
	}
}

func (t *GKETools) listClustersHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListClustersRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_gke_list_clusters", "project", args.ProjectID, "location", args.Location)
	resp, err := t.svc.ListClusters(ctx, args)
	if err != nil {
		return handleServiceError("gcp_gke_list_clusters", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_gke_list_clusters: marshal: %w", err)
	}
	return result, nil
}

func (t *GKETools) GetClusterDetails() server.ServerTool {
	tool := mcp.NewTool("gcp_gke_get_cluster_details",
		mcp.WithDescription("Get detailed information about a specific GKE cluster including node pools and configuration"),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("location", mcp.Required(), mcp.Description("GCP region or zone")),
		mcp.WithString("cluster_name", mcp.Required(), mcp.Description("GKE cluster name")),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.getClusterDetailsHandler),
	}
}

func (t *GKETools) getClusterDetailsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.GetClusterDetailsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_gke_get_cluster_details", "project", args.ProjectID, "cluster", args.ClusterName)
	resp, err := t.svc.GetClusterDetails(ctx, args)
	if err != nil {
		return handleServiceError("gcp_gke_get_cluster_details", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_gke_get_cluster_details: marshal: %w", err)
	}
	return result, nil
}

func (t *GKETools) GetClusterBottlenecks() server.ServerTool {
	tool := mcp.NewTool("gcp_gke_get_cluster_bottlenecks",
		mcp.WithDescription("Aggregate CPU/memory metrics and recent error logs to identify cluster bottlenecks and compute a severity rating"),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("location", mcp.Required(), mcp.Description("GCP region or zone")),
		mcp.WithString("cluster_name", mcp.Required(), mcp.Description("GKE cluster name")),
		mcp.WithNumber("lookback_minutes",
			mcp.Description("How far back to fetch metrics and logs (1–1440). Default: 30."),
			mcp.Min(1),
			mcp.Max(1440),
		),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.getClusterBottlenecksHandler),
	}
}

func (t *GKETools) getClusterBottlenecksHandler(ctx context.Context, _ mcp.CallToolRequest, args models.GetClusterBottlenecksRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_gke_get_cluster_bottlenecks",
		"project", args.ProjectID,
		"cluster", args.ClusterName,
		"lookback_minutes", args.LookbackMinutes,
	)
	report, err := t.svc.GetClusterBottlenecks(ctx, args)
	if err != nil {
		return handleServiceError("gcp_gke_get_cluster_bottlenecks", err)
	}
	result, err := mcp.NewToolResultJSON(report)
	if err != nil {
		return nil, fmt.Errorf("gcp_gke_get_cluster_bottlenecks: marshal: %w", err)
	}
	return result, nil
}

func (t *GKETools) ScaleDeployment() server.ServerTool {
	tool := mcp.NewTool("gcp_gke_scale_deployment",
		mcp.WithDescription("Scale a GKE node pool to the requested node count. Supports dry-run for safe previewing. Idempotent: scaling to the current count returns no-change."),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("location", mcp.Required(), mcp.Description("GCP region or zone")),
		mcp.WithString("cluster_name", mcp.Required(), mcp.Description("GKE cluster name")),
		mcp.WithString("node_pool_name", mcp.Required(), mcp.Description("Node pool name to resize")),
		mcp.WithNumber("node_count", mcp.Required(), mcp.Description("Desired node count"), mcp.Min(0), mcp.Max(1000)),
		mcp.WithBoolean("dry_run",
			mcp.Description("If true, describe what would happen without executing. Default: false."),
			mcp.DefaultBool(false),
		),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.scaleDeploymentHandler),
	}
}

func (t *GKETools) scaleDeploymentHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ScaleDeploymentRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_gke_scale_deployment",
		"project", args.ProjectID,
		"cluster", args.ClusterName,
		"node_pool", args.NodePoolName,
		"node_count", args.NodeCount,
		"dry_run", args.DryRun,
	)
	resp, err := t.svc.ScaleDeployment(ctx, args)
	if err != nil {
		return handleServiceError("gcp_gke_scale_deployment", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_gke_scale_deployment: marshal: %w", err)
	}
	return result, nil
}
