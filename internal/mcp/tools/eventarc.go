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

// EventarcTools provides MCP tool definitions and handlers for Eventarc operations.
type EventarcTools struct {
	svc ports.EventarcService
	log *slog.Logger
}

func NewEventarcTools(svc ports.EventarcService, log *slog.Logger) *EventarcTools {
	return &EventarcTools{svc: svc, log: log}
}

func (t *EventarcTools) Name() string { return "eventarc" }

func (t *EventarcTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.ListTriggers(),
		t.GetTrigger(),
	}
}

func (t *EventarcTools) ListTriggers() server.ServerTool {
	tool := mcp.NewTool("gcp_eventarc_list_triggers",
		mcp.WithDescription(
			"List Eventarc triggers in a GCP project. "+
				"Each trigger shows its destination kind (cloud_run_service, cloud_function, workflow, etc.), "+
				"destination URN, Pub/Sub transport topic, event filters, and service account. "+
				"Use region='-' or omit for all regions.",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("region", mcp.Description("GCP region (e.g. us-central1). Omit or use '-' for all regions.")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Eventarc Triggers",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listTriggersHandler),
	}
}

func (t *EventarcTools) listTriggersHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListTriggersRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_eventarc_list_triggers", "project", args.ProjectID, "region", args.Region)
	resp, err := t.svc.ListTriggers(ctx, args)
	if err != nil {
		return handleServiceError("gcp_eventarc_list_triggers", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_eventarc_list_triggers: marshal: %w", err)
	}
	return result, nil
}

func (t *EventarcTools) GetTrigger() server.ServerTool {
	tool := mcp.NewTool("gcp_eventarc_get_trigger",
		mcp.WithDescription("Get detailed information about a specific Eventarc trigger including all event filters, destination, and transport configuration"),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("region", mcp.Required(), mcp.Description("GCP region")),
		mcp.WithString("trigger_name", mcp.Required(), mcp.Description("Eventarc trigger name")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Get Eventarc Trigger",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.getTriggerHandler),
	}
}

func (t *EventarcTools) getTriggerHandler(ctx context.Context, _ mcp.CallToolRequest, args models.GetTriggerRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_eventarc_get_trigger", "project", args.ProjectID, "region", args.Region, "trigger", args.TriggerName)
	resp, err := t.svc.GetTrigger(ctx, args)
	if err != nil {
		return handleServiceError("gcp_eventarc_get_trigger", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_eventarc_get_trigger: marshal: %w", err)
	}
	return result, nil
}
