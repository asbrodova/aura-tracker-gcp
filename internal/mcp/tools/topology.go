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

// TopologyTools provides MCP tool definitions and handlers for service topology discovery.
type TopologyTools struct {
	svc ports.GCPService
	log *slog.Logger
}

func NewTopologyTools(svc ports.GCPService, log *slog.Logger) *TopologyTools {
	return &TopologyTools{svc: svc, log: log}
}

func (t *TopologyTools) GetServiceTopology() server.ServerTool {
	tool := mcp.NewTool("gcp_get_service_topology",
		mcp.WithDescription(
			"Infer the dependency graph of a Cloud Run service by scanning its Cloud SQL annotations, "+
				"VPC connector, environment variables, Secret Manager references, and Pub/Sub push subscriptions. "+
				"Returns both a structured node/edge graph and flat human-readable relationship statements. "+
				"Use depth=2 to also resolve dependencies-of-dependencies.",
		),
		mcp.WithString("project", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("region", mcp.Required(), mcp.Description("Cloud Run region, e.g. us-central1")),
		mcp.WithString("service_name", mcp.Required(), mcp.Description("Cloud Run service name")),
		mcp.WithNumber("depth",
			mcp.Description("Discovery depth: 1 = direct dependencies only, 2 = deps-of-deps (max 2). Default: 1."),
			mcp.Min(1),
			mcp.Max(2),
		),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.getServiceTopologyHandler),
	}
}

func (t *TopologyTools) getServiceTopologyHandler(ctx context.Context, _ mcp.CallToolRequest, args models.GetServiceTopologyRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_get_service_topology",
		"project", args.Project,
		"region", args.Region,
		"service", args.ServiceName,
		"depth", args.Depth,
	)
	resp, err := t.svc.GetServiceTopology(ctx, args)
	if err != nil {
		return handleServiceError("gcp_get_service_topology", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_get_service_topology: marshal: %w", err)
	}
	return result, nil
}
