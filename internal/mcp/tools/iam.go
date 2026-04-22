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

// IAMTools provides MCP tool definitions and handlers for IAM operations.
type IAMTools struct {
	svc ports.GCPService
	log *slog.Logger
}

func NewIAMTools(svc ports.GCPService, log *slog.Logger) *IAMTools {
	return &IAMTools{svc: svc, log: log}
}

func (t *IAMTools) TestPermissions() server.ServerTool {
	tool := mcp.NewTool("gcp_iam_test_permissions",
		mcp.WithDescription("Test which IAM permissions the caller service account has on a GCP project. Use this before attempting mutations to avoid permission errors."),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID to test permissions against")),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.testPermissionsHandler),
	}
}

func (t *IAMTools) testPermissionsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.TestPermissionsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_iam_test_permissions",
		"project", args.ProjectID,
		"permission_count", len(args.Permissions),
	)
	if len(args.Permissions) == 0 {
		args.Permissions = defaultPermissionsToCheck()
	}
	resp, err := t.svc.TestPermissions(ctx, args)
	if err != nil {
		return handleServiceError("gcp_iam_test_permissions", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_iam_test_permissions: marshal: %w", err)
	}
	return result, nil
}

// defaultPermissionsToCheck returns a useful set of GCP permissions to probe
// when the caller does not specify an explicit list.
func defaultPermissionsToCheck() []string {
	return []string{
		"container.clusters.list",
		"container.clusters.get",
		"container.clusters.update",
		"run.services.list",
		"run.services.get",
		"run.services.update",
		"pubsub.topics.list",
		"pubsub.topics.get",
		"logging.logEntries.list",
		"monitoring.timeSeries.list",
		"resourcemanager.projects.getIamPolicy",
	}
}
