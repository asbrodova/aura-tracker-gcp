package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/internal/environments"
	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// DatasetList returns a static resource listing all BigQuery datasets in the project.
// URI: gcp://{project}/bigquery/datasets
func (r *BigQueryResources) DatasetList(environment environments.Environment) server.ServerResource {
	uri := fmt.Sprintf("gcp://%s/bigquery/datasets", environment.DisplayName())
	return server.ServerResource{
		Resource: mcp.NewResource(uri, "BigQuery Datasets",
			mcp.WithResourceDescription("All BigQuery datasets in the project — start here to discover what data is available before writing SQL"),
			mcp.WithMIMEType("application/json"),
		),
		Handler: func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			r.log.InfoContext(ctx, "resource read", "uri", uri)
			resp, err := r.svc.ListDatasets(ctx, models.ListDatasetsRequest{ProjectID: environment.ProjectID})
			if err != nil {
				return nil, fmt.Errorf("bigquery datasets: %w", err)
			}
			data, _ := json.Marshal(resp)
			return []mcp.ResourceContents{mcp.TextResourceContents{
				URI: uri, MIMEType: "application/json", Text: string(data),
			}}, nil
		},
	}
}

// TableListTemplate returns a resource template for listing tables in a dataset.
// URI template: gcp://{project}/bigquery/{dataset}/tables
func (r *BigQueryResources) TableListTemplate() server.ServerResourceTemplate {
	return server.ServerResourceTemplate{
		Template: mcp.NewResourceTemplate(
			"gcp://{project}/bigquery/{dataset}/tables",
			"BigQuery Tables",
			mcp.WithTemplateDescription("Tables in a BigQuery dataset with row counts and sizes — read before fetching individual schemas"),
			mcp.WithTemplateMIMEType("application/json"),
		),
		Handler: func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			selector, dataset, err := parseBQTablesURI(req.Params.URI)
			if err != nil {
				return nil, err
			}
			environment, err := resolveEnvironment(r.environments, selector, r.placeholder)
			if err != nil {
				return nil, err
			}
			r.log.InfoContext(ctx, "resource read", "uri", req.Params.URI)
			resp, err := r.svc.ListTables(ctx, models.ListTablesRequest{ProjectID: environment.ProjectID, DatasetID: dataset})
			if err != nil {
				return nil, fmt.Errorf("bigquery tables: %w", err)
			}
			data, _ := json.Marshal(resp)
			return []mcp.ResourceContents{mcp.TextResourceContents{
				URI: req.Params.URI, MIMEType: "application/json", Text: string(data),
			}}, nil
		},
	}
}

// TableSchemaTemplate returns a resource template for reading a table's field definitions.
// URI template: gcp://{project}/bigquery/{dataset}/{table}/schema
func (r *BigQueryResources) TableSchemaTemplate() server.ServerResourceTemplate {
	return server.ServerResourceTemplate{
		Template: mcp.NewResourceTemplate(
			"gcp://{project}/bigquery/{dataset}/{table}/schema",
			"BigQuery Table Schema",
			mcp.WithTemplateDescription("Field definitions for a BigQuery table — read this before writing SQL queries to understand column names and types"),
			mcp.WithTemplateMIMEType("application/json"),
		),
		Handler: func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			selector, dataset, table, err := parseBQSchemaURI(req.Params.URI)
			if err != nil {
				return nil, err
			}
			environment, err := resolveEnvironment(r.environments, selector, r.placeholder)
			if err != nil {
				return nil, err
			}
			r.log.InfoContext(ctx, "resource read", "uri", req.Params.URI)
			resp, err := r.svc.GetTableSchema(ctx, models.GetTableSchemaRequest{
				ProjectID: environment.ProjectID, DatasetID: dataset, TableID: table,
			})
			if err != nil {
				return nil, fmt.Errorf("bigquery schema: %w", err)
			}
			data, _ := json.Marshal(resp)
			return []mcp.ResourceContents{mcp.TextResourceContents{
				URI: req.Params.URI, MIMEType: "application/json", Text: string(data),
			}}, nil
		},
	}
}

// parseBQTablesURI parses "gcp://project/bigquery/dataset/tables" → project, dataset.
func parseBQTablesURI(uri string) (project, dataset string, err error) {
	path := strings.TrimPrefix(uri, "gcp://")
	parts := strings.SplitN(path, "/", 5)
	if len(parts) < 4 || parts[1] != "bigquery" || parts[3] != "tables" {
		return "", "", fmt.Errorf("invalid BigQuery tables URI: %q", uri)
	}
	return parts[0], parts[2], nil
}

// parseBQSchemaURI parses "gcp://project/bigquery/dataset/table/schema" → project, dataset, table.
func parseBQSchemaURI(uri string) (project, dataset, table string, err error) {
	path := strings.TrimPrefix(uri, "gcp://")
	parts := strings.SplitN(path, "/", 6)
	if len(parts) < 5 || parts[1] != "bigquery" || parts[4] != "schema" {
		return "", "", "", fmt.Errorf("invalid BigQuery schema URI: %q", uri)
	}
	return parts[0], parts[2], parts[3], nil
}
