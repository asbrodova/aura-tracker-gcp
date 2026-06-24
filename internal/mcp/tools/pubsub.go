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
	svc ports.PubSubService
	log *slog.Logger
}

func NewPubSubTools(svc ports.PubSubService, log *slog.Logger) *PubSubTools {
	return &PubSubTools{svc: svc, log: log}
}

func (t *PubSubTools) Name() string { return "pubsub" }

func (t *PubSubTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.ListTopics(),
		t.InspectTopicHealth(),
		t.ListSubscriptions(),
	}
}

func (t *PubSubTools) ListTopics() server.ServerTool {
	tool := mcp.NewTool("gcp_pubsub_list_topics",
		mcp.WithDescription("List all Pub/Sub topics in a GCP project with their subscription counts"),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Pub/Sub Topics",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
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
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Inspect Pub/Sub Topic Health",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
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

func (t *PubSubTools) ListSubscriptions() server.ServerTool {
	tool := mcp.NewTool("gcp_pubsub_list_subscriptions",
		mcp.WithDescription(
			"List Pub/Sub subscriptions in a GCP project. "+
				"Shows push endpoint, dead-letter topic, and filter for each subscription. "+
				"Optionally filter by topic_name to list only subscriptions for a specific topic.",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("topic_name", mcp.Description("Optional: filter to subscriptions on this topic (bare name, not full resource path)")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Pub/Sub Subscriptions",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listSubscriptionsHandler),
	}
}

func (t *PubSubTools) listSubscriptionsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListSubscriptionsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_pubsub_list_subscriptions", "project", args.ProjectID, "topic", args.TopicName)
	resp, err := t.svc.ListSubscriptions(ctx, args)
	if err != nil {
		return handleServiceError("gcp_pubsub_list_subscriptions", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_pubsub_list_subscriptions: marshal: %w", err)
	}
	return result, nil
}
