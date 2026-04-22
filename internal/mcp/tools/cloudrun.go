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

// CloudRunTools provides MCP tool definitions and handlers for Cloud Run operations.
type CloudRunTools struct {
	svc ports.GCPService
	log *slog.Logger
}

func NewCloudRunTools(svc ports.GCPService, log *slog.Logger) *CloudRunTools {
	return &CloudRunTools{svc: svc, log: log}
}

func (t *CloudRunTools) ListServices() server.ServerTool {
	tool := mcp.NewTool("gcp_cloudrun_list_services",
		mcp.WithDescription("List all Cloud Run services in a GCP project and region"),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("region", mcp.Required(), mcp.Description("GCP region, e.g. us-central1")),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listServicesHandler),
	}
}

func (t *CloudRunTools) listServicesHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListServicesRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_cloudrun_list_services", "project", args.ProjectID, "region", args.Region)
	resp, err := t.svc.ListServices(ctx, args)
	if err != nil {
		return handleServiceError("gcp_cloudrun_list_services", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_cloudrun_list_services: marshal: %w", err)
	}
	return result, nil
}

func (t *CloudRunTools) GetServiceDetails() server.ServerTool {
	tool := mcp.NewTool("gcp_cloudrun_get_service_details",
		mcp.WithDescription("Get detailed information about a specific Cloud Run service including traffic splits and latest revision"),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("region", mcp.Required(), mcp.Description("GCP region")),
		mcp.WithString("service_name", mcp.Required(), mcp.Description("Cloud Run service name")),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.getServiceDetailsHandler),
	}
}

func (t *CloudRunTools) getServiceDetailsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.GetServiceDetailsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_cloudrun_get_service_details", "project", args.ProjectID, "service", args.ServiceName)
	resp, err := t.svc.GetServiceDetails(ctx, args)
	if err != nil {
		return handleServiceError("gcp_cloudrun_get_service_details", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_cloudrun_get_service_details: marshal: %w", err)
	}
	return result, nil
}

func (t *CloudRunTools) UpdateTraffic() server.ServerTool {
	tool := mcp.NewTool("gcp_cloudrun_update_traffic",
		mcp.WithDescription("Update the traffic split for a Cloud Run service. Supports dry-run for safe previewing."),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("region", mcp.Required(), mcp.Description("GCP region")),
		mcp.WithString("service_name", mcp.Required(), mcp.Description("Cloud Run service name")),
		mcp.WithBoolean("dry_run",
			mcp.Description("If true, describe what would happen without executing. Default: false."),
			mcp.DefaultBool(false),
		),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.updateTrafficHandler),
	}
}

func (t *CloudRunTools) updateTrafficHandler(ctx context.Context, _ mcp.CallToolRequest, args models.UpdateTrafficRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_cloudrun_update_traffic",
		"project", args.ProjectID,
		"service", args.ServiceName,
		"dry_run", args.DryRun,
	)
	resp, err := t.svc.UpdateTraffic(ctx, args)
	if err != nil {
		return handleServiceError("gcp_cloudrun_update_traffic", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_cloudrun_update_traffic: marshal: %w", err)
	}
	return result, nil
}
