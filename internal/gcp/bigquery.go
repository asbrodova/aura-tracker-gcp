package gcp

import (
	"context"

	"cloud.google.com/go/bigquery"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

const (
	maxDatasets = 100
	maxTables   = 200
	maxFields   = 500
)

func (a *gcpAdapter) ListDatasets(ctx context.Context, req models.ListDatasetsRequest) (models.ListDatasetsResponse, error) {
	if err := a.rateWait(ctx, "bigquery.ListDatasets"); err != nil {
		return models.ListDatasetsResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	it := a.bq.DatasetsInProject(ctx, req.ProjectID)
	var datasets []models.DatasetSummary
	for len(datasets) < maxDatasets {
		ds, err := it.Next()
		if err != nil {
			if isIteratorDone(err) {
				break
			}
			return models.ListDatasetsResponse{}, wrapGCPError("bigquery.ListDatasets", err)
		}
		meta, err := ds.Metadata(ctx)
		if err != nil {
			continue
		}
		datasets = append(datasets, models.DatasetSummary{
			ID:       ds.DatasetID,
			Location: meta.Location,
			Labels:   meta.Labels,
		})
	}
	if datasets == nil {
		datasets = []models.DatasetSummary{}
	}
	return models.ListDatasetsResponse{ProjectID: req.ProjectID, Datasets: datasets}, nil
}

func (a *gcpAdapter) ListTables(ctx context.Context, req models.ListTablesRequest) (models.ListTablesResponse, error) {
	if err := a.rateWait(ctx, "bigquery.ListTables"); err != nil {
		return models.ListTablesResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	it := a.bq.DatasetInProject(req.ProjectID, req.DatasetID).Tables(ctx)
	var tables []models.TableSummary
	for len(tables) < maxTables {
		t, err := it.Next()
		if err != nil {
			if isIteratorDone(err) {
				break
			}
			return models.ListTablesResponse{}, wrapGCPError("bigquery.ListTables", err)
		}
		meta, err := t.Metadata(ctx)
		if err != nil {
			continue
		}
		partField := ""
		if meta.TimePartitioning != nil {
			partField = meta.TimePartitioning.Field
		}
		tables = append(tables, models.TableSummary{
			ID:             t.TableID,
			Type:           string(meta.Type),
			NumRows:        meta.NumRows,
			SizeGB:         float64(meta.NumBytes) / 1e9,
			PartitionField: partField,
		})
	}
	if tables == nil {
		tables = []models.TableSummary{}
	}
	return models.ListTablesResponse{ProjectID: req.ProjectID, DatasetID: req.DatasetID, Tables: tables}, nil
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
