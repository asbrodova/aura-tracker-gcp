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
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Cloud Run Services",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
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
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Get Cloud Run Service Details",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
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
		mcp.WithDescription(
			"Update the traffic split for a Cloud Run service. "+
				"SAFETY: requires two-step confirmation. "+
				"Step 1 — call with dry_run=true to receive a plan_id and preview the before/after traffic split. "+
				"Step 2 — call with confirm_plan_id=<plan_id> to execute (plan expires after 10 minutes).",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("region", mcp.Required(), mcp.Description("GCP region")),
		mcp.WithString("service_name", mcp.Required(), mcp.Description("Cloud Run service name")),
		mcp.WithBoolean("dry_run",
			mcp.Description("Step 1: return a plan_id and before/after preview without executing. Default: false."),
			mcp.DefaultBool(false),
		),
		mcp.WithString("confirm_plan_id",
			mcp.Description("Step 2: plan_id from a prior dry_run call. Providing this executes the traffic split change."),
		),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Update Cloud Run Traffic Split",
			ReadOnlyHint:    boolPtr(false),
			DestructiveHint: boolPtr(true),
			IdempotentHint:  boolPtr(false),
			OpenWorldHint:   boolPtr(true),
		}),
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
