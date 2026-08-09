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

// SupplyChainTools provides MCP tool definitions for GCP supply chain resources:
// Artifact Registry, Cloud Build, and Service Directory.
type SupplyChainTools struct {
	svc ports.SupplyChainService
	log *slog.Logger
}

func NewSupplyChainTools(svc ports.SupplyChainService, log *slog.Logger) *SupplyChainTools {
	return &SupplyChainTools{svc: svc, log: log}
}

func (t *SupplyChainTools) Name() string { return "supplychain" }

func (t *SupplyChainTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.ListArtifactRegistryRepos(),
		t.ListArtifactRegistryImages(),
		t.ListCloudBuildTriggers(),
		t.ListServiceDirectoryNamespaces(),
	}
}

func (t *SupplyChainTools) ListArtifactRegistryRepos() server.ServerTool {
	tool := mcp.NewTool("gcp_artifactregistry_list_repos",
		mcp.WithDescription(
			"List Artifact Registry repositories in a project. "+
				"Returns repository name, format (DOCKER, MAVEN, NPM, etc.), location, "+
				"description, creation time, and size in bytes. "+
				"Omit location to list repositories in all locations.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("location", mcp.Description("GCP location/region, or omit for all locations")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Artifact Registry Repositories",
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
			Location  string `json:"location"`
		}) (*mcp.CallToolResult, error) {
			resp, err := t.svc.ListArtifactRegistryRepos(ctx, models.ListArtifactRegistryReposRequest{
				ProjectID: args.ProjectID,
				Location:  args.Location,
			})
			if err != nil {
				return handleServiceError("gcp_artifactregistry_list_repos", err)
			}
			result, err := mcp.NewToolResultJSON(resp)
			if err != nil {
				return nil, fmt.Errorf("gcp_artifactregistry_list_repos: marshal: %w", err)
			}
			return result, nil
		}),
	}
}

func (t *SupplyChainTools) ListArtifactRegistryImages() server.ServerTool {
	tool := mcp.NewTool("gcp_artifactregistry_list_images",
		mcp.WithDescription(
			"List Docker images in an Artifact Registry repository. "+
				"Returns image name, URI, tags, build time, upload time, and size in bytes.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("location", mcp.Required(), mcp.Description("GCP location/region of the repository")),
		mcp.WithString("repository", mcp.Required(), mcp.Description("Repository name (short name, not full path)")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Artifact Registry Images",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool: tool,
		Handler: mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, args struct {
			ProjectID  string `json:"project_id"`
			Location   string `json:"location"`
			Repository string `json:"repository"`
		}) (*mcp.CallToolResult, error) {
			resp, err := t.svc.ListArtifactRegistryImages(ctx, models.ListArtifactRegistryImagesRequest{
				ProjectID:  args.ProjectID,
				Location:   args.Location,
				Repository: args.Repository,
			})
			if err != nil {
				return handleServiceError("gcp_artifactregistry_list_images", err)
			}
			result, err := mcp.NewToolResultJSON(resp)
			if err != nil {
				return nil, fmt.Errorf("gcp_artifactregistry_list_images: marshal: %w", err)
			}
			return result, nil
		}),
	}
}

func (t *SupplyChainTools) ListCloudBuildTriggers() server.ServerTool {
	tool := mcp.NewTool("gcp_cloudbuild_list_triggers",
		mcp.WithDescription(
			"List Cloud Build triggers in a project. "+
				"Returns trigger ID, name, description, event type (github, pubsub, webhook, manual), "+
				"enabled/disabled status, tags, creation time, and build config filename. "+
				"Omit region to list triggers across all regions.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("region", mcp.Description("GCP region, or omit for all regions")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Cloud Build Triggers",
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
			Region    string `json:"region"`
		}) (*mcp.CallToolResult, error) {
			resp, err := t.svc.ListCloudBuildTriggers(ctx, models.ListCloudBuildTriggersRequest{
				ProjectID: args.ProjectID,
				Region:    args.Region,
			})
			if err != nil {
				return handleServiceError("gcp_cloudbuild_list_triggers", err)
			}
			result, err := mcp.NewToolResultJSON(resp)
			if err != nil {
				return nil, fmt.Errorf("gcp_cloudbuild_list_triggers: marshal: %w", err)
			}
			return result, nil
		}),
	}
}

func (t *SupplyChainTools) ListServiceDirectoryNamespaces() server.ServerTool {
	tool := mcp.NewTool("gcp_servicedirectory_list",
		mcp.WithDescription(
			"List Service Directory namespaces and their services in a project. "+
				"Returns namespace name, location, and the services registered within each namespace. "+
				"Omit location to list namespaces across all locations.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("location", mcp.Description("GCP location/region, or omit for all locations")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Service Directory Namespaces",
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
			Location  string `json:"location"`
		}) (*mcp.CallToolResult, error) {
			resp, err := t.svc.ListServiceDirectoryNamespaces(ctx, models.ListServiceDirectoryNamespacesRequest{
				ProjectID: args.ProjectID,
				Location:  args.Location,
			})
			if err != nil {
				return handleServiceError("gcp_servicedirectory_list", err)
			}
			result, err := mcp.NewToolResultJSON(resp)
			if err != nil {
				return nil, fmt.Errorf("gcp_servicedirectory_list: marshal: %w", err)
			}
			return result, nil
		}),
	}
}
