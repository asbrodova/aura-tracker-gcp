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

// SecretManagerTools provides MCP tool definitions and handlers for Secret Manager operations.
// Only secret metadata is returned — secret values are never read or exposed.
type SecretManagerTools struct {
	svc ports.GCPService
	log *slog.Logger
}

func NewSecretManagerTools(svc ports.GCPService, log *slog.Logger) *SecretManagerTools {
	return &SecretManagerTools{svc: svc, log: log}
}

func (t *SecretManagerTools) Name() string { return "secretmanager" }

func (t *SecretManagerTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.ListSecrets(),
	}
}

func (t *SecretManagerTools) ListSecrets() server.ServerTool {
	tool := mcp.NewTool("gcp_secretmanager_list",
		mcp.WithDescription(
			"List Secret Manager secrets in a GCP project. "+
				"Returns metadata only (name, labels, create time, replication policy). "+
				"Secret values are NEVER read or returned. "+
				"Set include_references=true to see which Cloud Run services reference each secret via secretKeyRef.",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("filter", mcp.Description("Optional server-side filter expression (e.g. 'labels.env=prod')")),
		mcp.WithBoolean("include_references",
			mcp.Description("If true, scan Cloud Run service specs to show which services reference each secret. Default: false."),
			mcp.DefaultBool(false),
		),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Secret Manager Secrets",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listSecretsHandler),
	}
}

func (t *SecretManagerTools) listSecretsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListSecretsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_secretmanager_list", "project", args.ProjectID, "include_references", args.IncludeReferences)
	resp, err := t.svc.ListSecrets(ctx, args)
	if err != nil {
		return handleServiceError("gcp_secretmanager_list", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_secretmanager_list: marshal: %w", err)
	}
	return result, nil
}
