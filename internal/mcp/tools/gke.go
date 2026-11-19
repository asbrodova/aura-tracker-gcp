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
	svc          ports.GKEService
	log          *slog.Logger
	skipApproval bool
}

// DisableMutationApproval makes the mutation tool execute without asking the
// user. Only for SAFETY_ENABLED=false (local stdio), where svc is the raw
// adapter without the safety decorator.
func (t *GKETools) DisableMutationApproval() { t.skipApproval = true }

func NewGKETools(svc ports.GKEService, log *slog.Logger) *GKETools {
	return &GKETools{svc: svc, log: log}
}

func (t *GKETools) Name() string { return "gke" }

func (t *GKETools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.ListClusters(),
		t.GetClusterDetails(),
		t.GetClusterBottlenecks(),
		t.ScaleDeployment(),
	}
}

func (t *GKETools) ListClusters() server.ServerTool {
	tool := mcp.NewTool("gcp_gke_list_clusters",
		mcp.WithDescription("List all GKE clusters in a GCP project and location"),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("location", mcp.Required(), mcp.Description("GCP region, zone, or '-' for all locations")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List GKE Clusters",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
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
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("location", mcp.Required(), mcp.Description("GCP region or zone")),
		mcp.WithString("cluster_name", mcp.Required(), mcp.Description("GKE cluster name")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Get GKE Cluster Details",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
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
		mcp.WithDescription("Identify GKE cluster bottlenecks from CPU/memory metrics and error logs; returns severity rating."),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("location", mcp.Required(), mcp.Description("GCP region or zone")),
		mcp.WithString("cluster_name", mcp.Required(), mcp.Description("GKE cluster name")),
		mcp.WithNumber("lookback_minutes",
			mcp.Description("How far back to fetch metrics and logs (1–1440). Default: 30."),
			mcp.Min(1),
			mcp.Max(1440),
		),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Analyse GKE Cluster Bottlenecks",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
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
		mcp.WithDescription(
			"Resize a GKE node pool to node_count nodes per zone. "+
				"Despite the tool name, this does not change Kubernetes Deployment replica counts. "+
				"Mutation: the server previews the change and asks the user to approve it in the MCP client; "+
				"nothing changes unless the user accepts. dry_run=true only previews. "+
				"Same count → no_change_needed=true.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("location", mcp.Required(), mcp.Description("GCP region or zone")),
		mcp.WithString("cluster_name", mcp.Required(), mcp.Description("GKE cluster name")),
		mcp.WithString("node_pool_name", mcp.Required(), mcp.Description("Node pool name to resize")),
		mcp.WithNumber("node_count", mcp.Required(), mcp.Description("Desired node count per zone"), mcp.Min(0), mcp.Max(1000)),
		mcp.WithBoolean("dry_run",
			mcp.Description("Only preview the change; never asks for approval and never executes. Default: false."),
			mcp.DefaultBool(false),
		),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Scale GKE Node Pool",
			ReadOnlyHint:    boolPtr(false),
			DestructiveHint: boolPtr(true),
			IdempotentHint:  boolPtr(false),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.scaleDeploymentHandler),
	}
}

func (t *GKETools) scaleDeploymentHandler(ctx context.Context, req mcp.CallToolRequest, args models.ScaleDeploymentRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_gke_scale_deployment",
		"project", args.ProjectID,
		"cluster", args.ClusterName,
		"node_pool", args.NodePoolName,
		"node_count", args.NodeCount,
		"dry_run", args.DryRun,
	)
	return runMutation(ctx, req, mutation[models.ScaleDeploymentResponse]{
		tool:          "gcp_gke_scale_deployment",
		previewOnly:   args.DryRun,
		confirmPlanID: args.ConfirmPlanID,
		skipApproval:  t.skipApproval,
		preview: func(ctx context.Context) (models.ScaleDeploymentResponse, error) {
			r := args
			r.DryRun, r.ConfirmPlanID = true, ""
			return t.svc.ScaleDeployment(ctx, r)
		},
		confirm: func(ctx context.Context, planID string) (models.ScaleDeploymentResponse, error) {
			return t.svc.ScaleDeployment(ctx, models.ScaleDeploymentRequest{ConfirmPlanID: planID})
		},
		execute: func(ctx context.Context) (models.ScaleDeploymentResponse, error) {
			r := args
			r.DryRun, r.ConfirmPlanID = false, ""
			return t.svc.ScaleDeployment(ctx, r)
		},
		plan: func(r models.ScaleDeploymentResponse) (string, bool) { return r.PlanID, r.NoChangeNeeded },
		strip: func(r *models.ScaleDeploymentResponse) {
			r.PlanID, r.ExpiresIn = "", ""
			r.Description = "PREVIEW ONLY: " + scalePreviewText(args, *r)
		},
		prompt: func(r models.ScaleDeploymentResponse) string {
			return "Approve this GKE change? " + scalePreviewText(args, r)
		},
	})
}

// scalePreviewText describes a node-pool resize for the user approving it.
// Project IDs in it are replaced by environment aliases before it leaves the
// server.
func scalePreviewText(args models.ScaleDeploymentRequest, r models.ScaleDeploymentResponse) string {
	text := fmt.Sprintf("Resize node pool %q in cluster %q (%s, project %s) from %d to %d nodes per zone",
		args.NodePoolName, args.ClusterName, args.Location, args.ProjectID, r.PreviousCount, r.RequestedCount)
	if r.Zones > 1 {
		text += fmt.Sprintf(" across %d zones (%d → %d nodes in total)", r.Zones, r.CurrentTotalNodes, r.RequestedCount*int32(r.Zones))
	}
	text += "."
	for _, warning := range r.Warnings {
		text += " Warning: " + warning
	}
	return text
}
