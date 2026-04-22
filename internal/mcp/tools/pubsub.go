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

// PubSubTools provides MCP tool definitions and handlers for Pub/Sub operations.
type PubSubTools struct {
	svc ports.GCPService
	log *slog.Logger
}

func NewPubSubTools(svc ports.GCPService, log *slog.Logger) *PubSubTools {
	return &PubSubTools{svc: svc, log: log}
}

func (t *PubSubTools) ListTopics() server.ServerTool {
	tool := mcp.NewTool("gcp_pubsub_list_topics",
		mcp.WithDescription("List all Pub/Sub topics in a GCP project with their subscription counts"),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listTopicsHandler),
	}
}

func (t *PubSubTools) listTopicsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListTopicsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_pubsub_list_topics", "project", args.ProjectID)
	resp, err := t.svc.ListTopics(ctx, args)
	if err != nil {
		return handleServiceError("gcp_pubsub_list_topics", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_pubsub_list_topics: marshal: %w", err)
	}
	return result, nil
}

func (t *PubSubTools) InspectTopicHealth() server.ServerTool {
	tool := mcp.NewTool("gcp_pubsub_inspect_topic_health",
		mcp.WithDescription("Inspect a Pub/Sub topic for subscription lag, unacked messages, and health issues"),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("topic_name", mcp.Required(), mcp.Description("Pub/Sub topic short name (not the full resource path)")),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.inspectTopicHealthHandler),
	}
}

func (t *PubSubTools) inspectTopicHealthHandler(ctx context.Context, _ mcp.CallToolRequest, args models.InspectTopicHealthRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_pubsub_inspect_topic_health", "project", args.ProjectID, "topic", args.TopicName)
	resp, err := t.svc.InspectTopicHealth(ctx, args)
	if err != nil {
		return handleServiceError("gcp_pubsub_inspect_topic_health", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_pubsub_inspect_topic_health: marshal: %w", err)
	}
	return result, nil
}
