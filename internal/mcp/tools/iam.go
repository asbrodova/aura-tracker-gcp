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
	svc ports.IAMService
	log *slog.Logger
}

func NewIAMTools(svc ports.IAMService, log *slog.Logger) *IAMTools {
	return &IAMTools{svc: svc, log: log}
}

func (t *IAMTools) Name() string { return "iam" }

func (t *IAMTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.TestPermissions(),
		t.GetResourceIAMBindings(),
		t.ListServiceAccounts(),
	}
}

func (t *IAMTools) TestPermissions() server.ServerTool {
	tool := mcp.NewTool("gcp_iam_test_permissions",
		mcp.WithDescription("Test which IAM permissions the service account holds on the project. Call before mutations to pre-check access."),
		mcp.WithString("project_id", mcp.Description("GCP project ID to test permissions against. Omit to use the server default.")),
		mcp.WithArray("permissions",
			mcp.Description("Permissions to test. Omit to check the server's standard read and mutation permission set."),
			mcp.WithStringItems(),
			mcp.UniqueItems(true),
			mcp.MaxItems(100),
		),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Test IAM Permissions",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
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

func (t *IAMTools) GetResourceIAMBindings() server.ServerTool {
	tool := mcp.NewTool("gcp_iam_get_resource_bindings",
		mcp.WithDescription(
			"Get IAM bindings for a GCP resource identified by its URN. "+
				"Supported URN kinds: storage_bucket, project. "+
				"URN format: urn:gcp:{kind}:{region}:{project_id}:{resource-name}. "+
				"Returns the list of role→members bindings on that resource.",
		),
		mcp.WithString("project_id", mcp.Description("Configured GCP project that must own the URN resource. Omit to use the server default.")),
		mcp.WithString("urn", mcp.Required(), mcp.Description("Resource URN, e.g. urn:gcp:storage_bucket:-:my-project:my-bucket")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Get Resource IAM Bindings",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool: tool,
		Handler: mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, args struct {
			ProjectID string `json:"project_id"`
			URN       string `json:"urn"`
		}) (*mcp.CallToolResult, error) {
			resp, err := t.svc.GetResourceIAMBindings(ctx, models.GetResourceIAMBindingsRequest{ProjectID: args.ProjectID, URN: args.URN})
			if err != nil {
				return handleServiceError("gcp_iam_get_resource_bindings", err)
			}
			result, err := mcp.NewToolResultJSON(resp)
			if err != nil {
				return nil, fmt.Errorf("gcp_iam_get_resource_bindings: marshal: %w", err)
			}
			return result, nil
		}),
	}
}

func (t *IAMTools) ListServiceAccounts() server.ServerTool {
	tool := mcp.NewTool("gcp_iam_list_service_accounts",
		mcp.WithDescription(
			"List IAM service accounts in a project. "+
				"Returns each account's name, email, display name, description, "+
				"disabled status, and unique numeric ID.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithNumber("page_size", mcp.Description("Maximum service accounts to return (1–1000). Default: 250."), mcp.Min(1), mcp.Max(1000)),
		mcp.WithString("page_token", mcp.Description("Opaque next_page_token from a previous call.")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Service Accounts",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool: tool,
		Handler: mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, args struct {
			ProjectID string `json:"project_id"`
			PageSize  int    `json:"page_size,omitempty"`
			PageToken string `json:"page_token,omitempty"`
		}) (*mcp.CallToolResult, error) {
			resp, err := t.svc.ListServiceAccounts(ctx, models.ListServiceAccountsRequest{
				ProjectID: args.ProjectID, PageSize: args.PageSize, PageToken: args.PageToken,
			})
			if err != nil {
				return handleServiceError("gcp_iam_list_service_accounts", err)
			}
			result, err := mcp.NewToolResultJSON(resp)
			if err != nil {
				return nil, fmt.Errorf("gcp_iam_list_service_accounts: marshal: %w", err)
			}
			return result, nil
		}),
	}
}

// defaultPermissionsToCheck returns a useful set of GCP permissions to probe
// when the caller does not specify an explicit list.
func defaultPermissionsToCheck() []string {
	return []string{
		// Phase 1 — core services
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
		// Phase 2 — GKE workloads
		"container.namespaces.get",
		// Phase 2 — networking
		"compute.forwardingRules.list",
		"compute.networks.list",
		"compute.subnetworks.list",
		"compute.networkEndpointGroups.list",
		// Phase 2 — data stores
		"spanner.instances.list",
		"alloydb.clusters.list",
		"datastore.databases.list",
		"redis.instances.list",
		// Phase 2 — supply chain
		"artifactregistry.repositories.list",
		"cloudbuild.builds.list",
		"servicedirectory.namespaces.list",
		// Phase 2 — IAM
		"iam.serviceAccounts.list",
		// Phase 2 — monitoring completions
		"monitoring.alertPolicies.list",
		"monitoring.uptimeCheckConfigs.list",
		// Phase 2 — tagging
		"resourcemanager.tagBindings.list",
		// Phase 2 — traces
		"cloudtrace.traces.list",
	}
}
