package gcp

import (
	"context"
	"errors"
	"fmt"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

const (
	defaultDatasetPageSize = 100
	defaultTablePageSize   = 200
	maxFields              = 500
)

func (a *gcpAdapter) ListDatasets(ctx context.Context, req models.ListDatasetsRequest) (models.ListDatasetsResponse, error) {
	if err := a.rateWait(ctx, "bigquery.ListDatasets"); err != nil {
		return models.ListDatasetsResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()
	pageSize, err := bigQueryInventoryPageSize(req.PageSize, defaultDatasetPageSize)
	if err != nil {
		return models.ListDatasetsResponse{}, fmt.Errorf("bigquery.ListDatasets: %w", err)
	}

	it := a.bq.DatasetsInProject(ctx, req.ProjectID)
	var handles []*bigquery.Dataset
	nextPageToken, err := iterator.NewPager(it, pageSize, req.PageToken).NextPage(&handles)
	if err != nil {
		return models.ListDatasetsResponse{}, wrapGCPError("bigquery.ListDatasets", err)
	}
	datasets := make([]models.DatasetSummary, 0, len(handles))
	var metadataErrors []models.ToolError
	for _, ds := range handles {
		meta, err := ds.Metadata(ctx)
		if err != nil {
			metadataErrors = append(metadataErrors, bigQueryMetadataToolError("bigquery.datasets.get", ds.DatasetID, err))
			continue
		}
		datasets = append(datasets, models.DatasetSummary{
			ID:       ds.DatasetID,
			Location: meta.Location,
			Labels:   meta.Labels,
		})
	}
	sortToolErrors(metadataErrors)
	return models.ListDatasetsResponse{
		ProjectID: req.ProjectID, Datasets: datasets, NextPageToken: nextPageToken,
		Truncated: nextPageToken != "", Errors: metadataErrors,
	}, nil
}

func (a *gcpAdapter) ListTables(ctx context.Context, req models.ListTablesRequest) (models.ListTablesResponse, error) {
	if err := a.rateWait(ctx, "bigquery.ListTables"); err != nil {
		return models.ListTablesResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()
	if req.DatasetID == "" {
		return models.ListTablesResponse{}, fmt.Errorf("bigquery.ListTables: dataset_id is required")
	}
	pageSize, err := bigQueryInventoryPageSize(req.PageSize, defaultTablePageSize)
	if err != nil {
		return models.ListTablesResponse{}, fmt.Errorf("bigquery.ListTables: %w", err)
	}

	it := a.bq.DatasetInProject(req.ProjectID, req.DatasetID).Tables(ctx)
	var handles []*bigquery.Table
	nextPageToken, err := iterator.NewPager(it, pageSize, req.PageToken).NextPage(&handles)
	if err != nil {
		return models.ListTablesResponse{}, wrapGCPError("bigquery.ListTables", err)
	}
	tables := make([]models.TableSummary, 0, len(handles))
	var metadataErrors []models.ToolError
	for _, table := range handles {
		meta, err := table.Metadata(ctx)
		if err != nil {
			metadataErrors = append(metadataErrors, bigQueryMetadataToolError("bigquery.tables.get", table.TableID, err))
			continue
		}
		partField := ""
		if meta.TimePartitioning != nil {
			partField = meta.TimePartitioning.Field
		}
		tables = append(tables, models.TableSummary{
			ID:             table.TableID,
			Type:           string(meta.Type),
			NumRows:        meta.NumRows,
			SizeGB:         float64(meta.NumBytes) / 1e9,
			PartitionField: partField,
		})
	}
	sortToolErrors(metadataErrors)
	return models.ListTablesResponse{
		ProjectID: req.ProjectID, DatasetID: req.DatasetID, Tables: tables,
		NextPageToken: nextPageToken, Truncated: nextPageToken != "", Errors: metadataErrors,
	}, nil
}

func bigQueryInventoryPageSize(requested, defaultValue int) (int, error) {
	if requested == 0 {
		return defaultValue, nil
	}
	return inventoryPageSize(requested)
}

func bigQueryMetadataToolError(api, resource string, err error) models.ToolError {
	wrapped := wrapGCPError(api, err)
	result := models.ToolError{FailingAPI: api, Message: fmt.Sprintf("%s: %v", resource, wrapped)}
	var denied *PermissionDeniedError
	if errors.As(wrapped, &denied) {
		result.MissingIAMPermissions = denied.MissingPermissions
	}
	var retriable *RetriableError
	result.Retriable = errors.As(wrapped, &retriable)
	return result
}

func (a *gcpAdapter) GetTableSchema(ctx context.Context, req models.GetTableSchemaRequest) (models.TableSchemaResponse, error) {
	if err := a.rateWait(ctx, "bigquery.GetTableSchema"); err != nil {
		return models.TableSchemaResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	meta, err := a.bq.DatasetInProject(req.ProjectID, req.DatasetID).Table(req.TableID).Metadata(ctx)
	if err != nil {
		return models.TableSchemaResponse{}, wrapGCPError("bigquery.GetTableSchema", err)
	}

	fields := convertSchema(meta.Schema)
	truncated := false
	if len(fields) > maxFields {
		fields = fields[:maxFields]
		truncated = true
	}
	return models.TableSchemaResponse{
		ProjectID: req.ProjectID,
		DatasetID: req.DatasetID,
		TableID:   req.TableID,
		Fields:    fields,
		Truncated: truncated,
	}, nil
}

func convertSchema(schema bigquery.Schema) []models.FieldSchema {
	fields := make([]models.FieldSchema, 0, len(schema))
	for _, f := range schema {
		mode := "NULLABLE"
		if f.Required {
			mode = "REQUIRED"
		} else if f.Repeated {
			mode = "REPEATED"
		}
		fs := models.FieldSchema{
			Name:        f.Name,
			Type:        string(f.Type),
			Mode:        mode,
			Description: f.Description,
		}
		if len(f.Schema) > 0 {
			fs.Fields = convertSchema(f.Schema)
		}
		fields = append(fields, fs)
	}
	return fields
}
