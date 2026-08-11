package tools

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

type EnvironmentComparer interface {
	Compare(context.Context, models.CompareEnvironmentsRequest) (models.CompareEnvironmentsResponse, error)
}

type DriftTools struct {
	comparer EnvironmentComparer
	log      *slog.Logger
}

func NewDriftTools(comparer EnvironmentComparer, log *slog.Logger) *DriftTools {
	if log == nil {
		log = slog.Default()
	}
	return &DriftTools{comparer: comparer, log: log}
}

func (t *DriftTools) Name() string { return "drift" }

func (t *DriftTools) GetTools() []server.ServerTool {
	return []server.ServerTool{t.CompareEnvironments()}
}

func (t *DriftTools) CompareEnvironments() server.ServerTool {
	components := []string{
		"bigquery", "cloudrun", "cloudsql", "datastores", "eventarc", "functions",
		"gke", "gke_workloads", "iam", "monitoring", "networking", "pubsub",
		"scheduler", "secretmanager", "storage", "supplychain", "tasks", "vpcaccess", "workflows",
	}
	tool := mcp.NewTool("gcp_compare_environments",
		mcp.WithDescription("Find configuration drift, diffs, differences, and environment parity between two configured GCP environments. Compares the whole environments when components is omitted, or only requested components. Neither environment is authoritative; results always name the exact environment where a resource or field is missing. Present the summary first, group missing resources under the exact aliases (for example, 'Missing in dev'), show changed fields with both alias-specific values, and put coverage gaps last."),
		mcp.WithString("environment_a", mcp.Required(), mcp.Description("First configured environment alias or project ID. No environment is treated as authoritative.")),
		mcp.WithString("environment_b", mcp.Required(), mcp.Description("Second configured environment alias or project ID. Must resolve to a different configured environment.")),
		mcp.WithArray("components", mcp.Description("Optional components to compare. Omit for the whole environment across every supported component."), mcp.WithStringItems(mcp.Enum(components...)), mcp.UniqueItems(true), mcp.MaxItems(len(components))),
		mcp.WithArray("resource_names", mcp.Description("Optional exact resource names to include across selected components."), mcp.WithStringItems(mcp.MinLength(1), mcp.MaxLength(256)), mcp.UniqueItems(true), mcp.MaxItems(100)),
		mcp.WithArray("locations", mcp.Description("Optional GCP regions or zones to include."), mcp.WithStringItems(mcp.MinLength(1), mcp.MaxLength(128)), mcp.UniqueItems(true), mcp.MaxItems(100)),
		mcp.WithArray("namespaces", mcp.Description("Optional Kubernetes namespaces when gke_workloads is selected."), mcp.WithStringItems(mcp.MinLength(1), mcp.MaxLength(253)), mcp.UniqueItems(true), mcp.MaxItems(100)),
		mcp.WithString("detail_level", mcp.Description("Output detail: summary, standard, or detailed (default: standard)."), mcp.Enum("summary", "standard", "detailed"), mcp.DefaultString("standard")),
		mcp.WithBoolean("include_unchanged", mcp.Description("Include equivalent resources in the resource list (default: false)."), mcp.DefaultBool(false)),
		mcp.WithNumber("max_changes", mcp.Description("Maximum resource results returned, 1-1000 (default: 250)."), mcp.Min(1), mcp.Max(1000)),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{Title: "Compare GCP Environments", ReadOnlyHint: boolPtr(true), DestructiveHint: boolPtr(false), IdempotentHint: boolPtr(true), OpenWorldHint: boolPtr(true)}),
	)
	return server.ServerTool{Tool: tool, Handler: mcp.NewTypedToolHandler(t.compareHandler)}
}

func (t *DriftTools) compareHandler(ctx context.Context, _ mcp.CallToolRequest, args models.CompareEnvironmentsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_compare_environments", "environment_a", args.EnvironmentA, "environment_b", args.EnvironmentB, "components", args.Components)
	response, err := t.comparer.Compare(ctx, args)
	if err != nil {
		return handleServiceError("gcp_compare_environments", err)
	}
	result, err := mcp.NewToolResultJSON(response)
	if err != nil {
		return nil, fmt.Errorf("gcp_compare_environments: marshal: %w", err)
	}
	return result, nil
}
