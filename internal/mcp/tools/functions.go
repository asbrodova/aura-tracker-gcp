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

// FunctionsTools provides MCP tool definitions and handlers for Cloud Functions operations.
type FunctionsTools struct {
	svc ports.FunctionsService
	log *slog.Logger
}

func NewFunctionsTools(svc ports.FunctionsService, log *slog.Logger) *FunctionsTools {
	return &FunctionsTools{svc: svc, log: log}
}

func (t *FunctionsTools) Name() string { return "functions" }

func (t *FunctionsTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.ListFunctions(),
		t.GetFunctionDetails(),
	}
}

func (t *FunctionsTools) ListFunctions() server.ServerTool {
	tool := mcp.NewTool("gcp_functions_list",
		mcp.WithDescription(
			"List Cloud Functions (Gen 1 and/or Gen 2) in a GCP project. "+
				"Gen 2 functions run on Cloud Run and are listed separately from gcp_cloudrun_list_services. "+
				"Use generation='1', '2', or 'both' (default). Use region='-' or omit for all regions.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("region", mcp.Description("GCP region (e.g. us-central1). Omit or use '-' for all regions.")),
		mcp.WithString("generation",
			mcp.Description("Which generation to list: '1' (Gen 1 only), '2' (Gen 2 only), or 'both' (default)."),
			mcp.DefaultString("both"),
		),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Cloud Functions",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listFunctionsHandler),
	}
}

func (t *FunctionsTools) listFunctionsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListFunctionsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_functions_list", "project", args.ProjectID, "region", args.Region, "generation", args.Generation)
	resp, err := t.svc.ListFunctions(ctx, args)
	if err != nil {
		return handleServiceError("gcp_functions_list", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_functions_list: marshal: %w", err)
	}
	return result, nil
}

func (t *FunctionsTools) GetFunctionDetails() server.ServerTool {
	tool := mcp.NewTool("gcp_functions_get_details",
		mcp.WithDescription(
			"Get detailed information about a specific Cloud Function (Gen 1 or Gen 2) including runtime, entry point, memory, timeout, service account, and VPC connector. "+
				"For Gen 2 functions, also returns the underlying Cloud Run service name.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("region", mcp.Required(), mcp.Description("GCP region")),
		mcp.WithString("function_name", mcp.Required(), mcp.Description("Cloud Function name")),
		mcp.WithNumber("generation",
			mcp.Description("Generation to look up: 1 or 2. Omit for auto-detect (tries Gen 2 first)."),
		),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Get Cloud Function Details",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.getFunctionDetailsHandler),
	}
}

func (t *FunctionsTools) getFunctionDetailsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.GetFunctionDetailsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_functions_get_details", "project", args.ProjectID, "region", args.Region, "function", args.FunctionName)
	resp, err := t.svc.GetFunctionDetails(ctx, args)
	if err != nil {
		return handleServiceError("gcp_functions_get_details", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_functions_get_details: marshal: %w", err)
	}
	return result, nil
}
