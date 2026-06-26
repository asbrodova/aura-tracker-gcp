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

// TaggingTools provides MCP tool definitions and handlers for GCP resource tags.
type TaggingTools struct {
	svc ports.TaggingService
	log *slog.Logger
}

func NewTaggingTools(svc ports.TaggingService, log *slog.Logger) *TaggingTools {
	return &TaggingTools{svc: svc, log: log}
}

func (t *TaggingTools) Name() string { return "tagging" }

func (t *TaggingTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.ListTaggedResources(),
	}
}

func (t *TaggingTools) ListTaggedResources() server.ServerTool {
	tool := mcp.NewTool("gcp_tag_list_resources",
		mcp.WithDescription(
			"List GCP resources bound to a specific tag key (and optional value) using the Cloud Resource Manager v3 TagBindings API. "+
				"Returns each resource's full name, the bound tag value ID, and the namespaced tag name. "+
				"tag_key should be the short key name (e.g. \"env\") as it appears in the namespaced tag name. "+
				"When tag_value is omitted, all values under the key are returned.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("tag_key", mcp.Required(), mcp.Description("Tag key short name (e.g. \"env\"); matched against the namespaced tag name")),
		mcp.WithString("tag_value", mcp.Description("Tag value short name (e.g. \"prod\"); omit to return all values under the key")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Tagged Resources",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool: tool,
		Handler: mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, args models.ListTaggedResourcesRequest) (*mcp.CallToolResult, error) {
			t.log.InfoContext(ctx, "gcp_tag_list_resources", "project", args.ProjectID, "key", args.TagKey, "value", args.TagValue)
			resp, err := t.svc.ListTaggedResources(ctx, args)
			if err != nil {
				return handleServiceError("gcp_tag_list_resources", err)
			}
			result, err := mcp.NewToolResultJSON(resp)
			if err != nil {
				return nil, fmt.Errorf("gcp_tag_list_resources: marshal: %w", err)
			}
			return result, nil
		}),
	}
}
