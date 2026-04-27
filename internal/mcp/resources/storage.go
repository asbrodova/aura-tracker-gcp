package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// BucketList returns a static resource listing all GCS buckets in the project.
// URI: gcp://{project}/storage/buckets
func (r *StorageResources) BucketList(project string) server.ServerResource {
	uri := fmt.Sprintf("gcp://%s/storage/buckets", project)
	return server.ServerResource{
		Resource: mcp.NewResource(uri, "Cloud Storage Buckets",
			mcp.WithResourceDescription("All GCS buckets in the project — discover bucket names before reading metadata"),
			mcp.WithMIMEType("application/json"),
		),
		Handler: func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			r.log.InfoContext(ctx, "resource read", "uri", uri)
			resp, err := r.svc.ListBuckets(ctx, models.ListBucketsRequest{ProjectID: project})
			if err != nil {
				return nil, fmt.Errorf("storage buckets: %w", err)
			}
			data, _ := json.Marshal(resp)
			return []mcp.ResourceContents{mcp.TextResourceContents{
				URI: uri, MIMEType: "application/json", Text: string(data),
			}}, nil
		},
	}
}

// BucketMetadataTemplate returns a resource template for bucket metadata.
// URI template: gcp://{project}/storage/{bucket}
func (r *StorageResources) BucketMetadataTemplate() server.ServerResourceTemplate {
	return server.ServerResourceTemplate{
		Template: mcp.NewResourceTemplate(
			"gcp://{project}/storage/{bucket}",
			"Cloud Storage Bucket Metadata",
			mcp.WithTemplateDescription("Metadata for a GCS bucket: versioning, lifecycle rule count, uniform access, and public access prevention"),
			mcp.WithTemplateMIMEType("application/json"),
		),
		Handler: func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			project, bucket, err := parseStorageBucketURI(req.Params.URI)
			if err != nil {
				return nil, err
			}
			r.log.InfoContext(ctx, "resource read", "uri", req.Params.URI)
			resp, err := r.svc.GetBucketMetadata(ctx, models.GetBucketMetadataRequest{
				ProjectID:  project,
				BucketName: bucket,
			})
			if err != nil {
				return nil, fmt.Errorf("storage bucket: %w", err)
			}
			data, _ := json.Marshal(resp)
			return []mcp.ResourceContents{mcp.TextResourceContents{
				URI: req.Params.URI, MIMEType: "application/json", Text: string(data),
			}}, nil
		},
	}
}

// ObjectListTemplate returns a resource template for bucket object listing.
// URI template: gcp://{project}/storage/{bucket}/objects
// Returns bucket config summary with a note — full object listing requires GCS client tools.
func (r *StorageResources) ObjectListTemplate() server.ServerResourceTemplate {
	return server.ServerResourceTemplate{
		Template: mcp.NewResourceTemplate(
			"gcp://{project}/storage/{bucket}/objects",
			"Cloud Storage Objects",
			mcp.WithTemplateDescription("Bucket config summary for object browsing — includes versioning and lifecycle info"),
			mcp.WithTemplateMIMEType("application/json"),
		),
		Handler: func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			rawURI := strings.TrimSuffix(req.Params.URI, "/objects")
			project, bucket, err := parseStorageBucketURI(rawURI)
			if err != nil {
				return nil, err
			}
			r.log.InfoContext(ctx, "resource read", "uri", req.Params.URI)
			resp, err := r.svc.GetBucketMetadata(ctx, models.GetBucketMetadataRequest{
				ProjectID:  project,
				BucketName: bucket,
			})
			if err != nil {
				return nil, fmt.Errorf("storage objects: %w", err)
			}
			result := map[string]any{
				"bucket":             bucket,
				"project":            project,
				"versioning_enabled": resp.VersioningEnabled,
				"lifecycle_rules":    resp.LifecycleRuleCount,
				"note":               "Per-object listing is not available via MCP resources. Use gsutil ls gs://" + bucket + " or the Cloud Storage API directly.",
			}
			data, _ := json.Marshal(result)
			return []mcp.ResourceContents{mcp.TextResourceContents{
				URI: req.Params.URI, MIMEType: "application/json", Text: string(data),
			}}, nil
		},
	}
}

// parseStorageBucketURI parses "gcp://project/storage/bucket" → project, bucket.
func parseStorageBucketURI(uri string) (project, bucket string, err error) {
	path := strings.TrimPrefix(uri, "gcp://")
	parts := strings.SplitN(path, "/", 4)
	if len(parts) < 3 || parts[1] != "storage" {
		return "", "", fmt.Errorf("invalid Cloud Storage bucket URI: %q", uri)
	}
	return parts[0], parts[2], nil
}
