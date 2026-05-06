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

// DatastoreTools provides MCP tool definitions and handlers for managed data store services.
type DatastoreTools struct {
	svc ports.GCPService
	log *slog.Logger
}

func NewDatastoreTools(svc ports.GCPService, log *slog.Logger) *DatastoreTools {
	return &DatastoreTools{svc: svc, log: log}
}

func (t *DatastoreTools) Name() string { return "datastores" }

func (t *DatastoreTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.ListSpannerInstances(),
		t.ListAlloyDBClusters(),
		t.ListFirestoreDatabases(),
		t.ListMemorystoreInstances(),
	}
}

func (t *DatastoreTools) ListSpannerInstances() server.ServerTool {
	tool := mcp.NewTool("gcp_spanner_list_instances",
		mcp.WithDescription("List Cloud Spanner instances in a GCP project including state, node count, processing units, and edition."),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Spanner Instances",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listSpannerInstancesHandler),
	}
}

func (t *DatastoreTools) listSpannerInstancesHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListSpannerInstancesRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_spanner_list_instances", "project", args.ProjectID)
	resp, err := t.svc.ListSpannerInstances(ctx, args)
	if err != nil {
		return handleServiceError("gcp_spanner_list_instances", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_spanner_list_instances: marshal: %w", err)
	}
	return result, nil
}

func (t *DatastoreTools) ListAlloyDBClusters() server.ServerTool {
	tool := mcp.NewTool("gcp_alloydb_list_clusters",
		mcp.WithDescription("List AlloyDB clusters in a GCP project including state, PostgreSQL version, and cluster type (primary/secondary)."),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("location", mcp.Description("GCP region (e.g. us-central1). Omit to list across all regions.")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List AlloyDB Clusters",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listAlloyDBClustersHandler),
	}
}

func (t *DatastoreTools) listAlloyDBClustersHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListAlloyDBClustersRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_alloydb_list_clusters", "project", args.ProjectID, "location", args.Location)
	resp, err := t.svc.ListAlloyDBClusters(ctx, args)
	if err != nil {
		return handleServiceError("gcp_alloydb_list_clusters", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_alloydb_list_clusters: marshal: %w", err)
	}
	return result, nil
}

func (t *DatastoreTools) ListFirestoreDatabases() server.ServerTool {
	tool := mcp.NewTool("gcp_firestore_list_databases",
		mcp.WithDescription("List Firestore databases in a GCP project including type (Native/Datastore), location, concurrency mode, and edition."),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Firestore Databases",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listFirestoreDatabasesHandler),
	}
}

func (t *DatastoreTools) listFirestoreDatabasesHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListFirestoreDatabasesRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_firestore_list_databases", "project", args.ProjectID)
	resp, err := t.svc.ListFirestoreDatabases(ctx, args)
	if err != nil {
		return handleServiceError("gcp_firestore_list_databases", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_firestore_list_databases: marshal: %w", err)
	}
	return result, nil
}

func (t *DatastoreTools) ListMemorystoreInstances() server.ServerTool {
	tool := mcp.NewTool("gcp_memorystore_list_instances",
		mcp.WithDescription("List Memorystore for Redis instances in a GCP project including tier, memory size, Redis version, host endpoint, and state."),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("location", mcp.Description("GCP region (e.g. us-central1). Omit to list across all regions.")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Memorystore Instances",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listMemorystoreInstancesHandler),
	}
}

func (t *DatastoreTools) listMemorystoreInstancesHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListMemorystoreInstancesRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_memorystore_list_instances", "project", args.ProjectID, "location", args.Location)
	resp, err := t.svc.ListMemorystoreInstances(ctx, args)
	if err != nil {
		return handleServiceError("gcp_memorystore_list_instances", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_memorystore_list_instances: marshal: %w", err)
	}
	return result, nil
}
