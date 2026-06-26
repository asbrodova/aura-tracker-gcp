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

// StorageTools provides MCP tool definitions and handlers for Cloud Storage operations.
type StorageTools struct {
	svc ports.StorageService
	log *slog.Logger
}

func NewStorageTools(svc ports.StorageService, log *slog.Logger) *StorageTools {
	return &StorageTools{svc: svc, log: log}
}

func (t *StorageTools) Name() string { return "storage" }

func (t *StorageTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.ListBuckets(),
		t.GetBucketMetadata(),
		t.ListBucketObjects(),
	}
}

func (t *StorageTools) ListBuckets() server.ServerTool {
	tool := mcp.NewTool("gcp_storage_list_buckets",
		mcp.WithDescription("List all Cloud Storage buckets in a GCP project with location, storage class, labels, and creation time."),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List GCS Buckets",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listBucketsHandler),
	}
}

func (t *StorageTools) listBucketsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListBucketsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_storage_list_buckets", "project", args.ProjectID)
	resp, err := t.svc.ListBuckets(ctx, args)
	if err != nil {
		return handleServiceError("gcp_storage_list_buckets", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_storage_list_buckets: marshal: %w", err)
	}
	return result, nil
}

func (t *StorageTools) GetBucketMetadata() server.ServerTool {
	tool := mcp.NewTool("gcp_storage_get_bucket_metadata",
		mcp.WithDescription("Get detailed metadata for a Cloud Storage bucket: versioning, uniform bucket-level access, public access prevention, and lifecycle rule count."),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("bucket_name", mcp.Required(), mcp.Description("Globally unique bucket name")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Get GCS Bucket Metadata",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.getBucketMetadataHandler),
	}
}

func (t *StorageTools) getBucketMetadataHandler(ctx context.Context, _ mcp.CallToolRequest, args models.GetBucketMetadataRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_storage_get_bucket_metadata", "project", args.ProjectID, "bucket", args.BucketName)
	resp, err := t.svc.GetBucketMetadata(ctx, args)
	if err != nil {
		return handleServiceError("gcp_storage_get_bucket_metadata", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_storage_get_bucket_metadata: marshal: %w", err)
	}
	return result, nil
}

func (t *StorageTools) ListBucketObjects() server.ServerTool {
	tool := mcp.NewTool("gcp_storage_list_bucket_objects",
		mcp.WithDescription("List objects in a GCS bucket. Supports optional prefix filtering and a configurable result limit (max 1000)."),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("bucket_name", mcp.Required(), mcp.Description("Bucket name")),
		mcp.WithString("prefix", mcp.Description("Filter objects by name prefix (e.g. \"images/\")")),
		mcp.WithNumber("max_results", mcp.Description("Maximum number of objects to return (default 1000, max 1000)")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List GCS Bucket Objects",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listBucketObjectsHandler),
	}
}

func (t *StorageTools) listBucketObjectsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListBucketObjectsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_storage_list_bucket_objects", "project", args.ProjectID, "bucket", args.BucketName, "prefix", args.Prefix)
	resp, err := t.svc.ListBucketObjects(ctx, args)
	if err != nil {
		return handleServiceError("gcp_storage_list_bucket_objects", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_storage_list_bucket_objects: marshal: %w", err)
	}
	return result, nil
}
