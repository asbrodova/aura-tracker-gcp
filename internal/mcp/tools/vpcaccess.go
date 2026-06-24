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

// VPCAccessTools provides MCP tool definitions and handlers for Serverless VPC Access.
type VPCAccessTools struct {
	svc ports.VPCAccessService
	log *slog.Logger
}

func NewVPCAccessTools(svc ports.VPCAccessService, log *slog.Logger) *VPCAccessTools {
	return &VPCAccessTools{svc: svc, log: log}
}

func (t *VPCAccessTools) Name() string { return "vpcaccess" }

func (t *VPCAccessTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.ListVPCConnectors(),
	}
}

func (t *VPCAccessTools) ListVPCConnectors() server.ServerTool {
	tool := mcp.NewTool("gcp_vpc_list_connectors",
		mcp.WithDescription("List Serverless VPC Access connectors in a GCP project. Use region='-' or omit for all regions."),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("region", mcp.Description("GCP region (e.g. us-central1). Omit or use '-' for all regions.")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List VPC Access Connectors",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listVPCConnectorsHandler),
	}
}

func (t *VPCAccessTools) listVPCConnectorsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListVPCConnectorsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_vpc_list_connectors", "project", args.ProjectID, "region", args.Region)
	resp, err := t.svc.ListVPCConnectors(ctx, args)
	if err != nil {
		return handleServiceError("gcp_vpc_list_connectors", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_vpc_list_connectors: marshal: %w", err)
	}
	return result, nil
}
