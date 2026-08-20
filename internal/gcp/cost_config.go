package gcp

import (
	"fmt"
	"strings"

	"cloud.google.com/go/bigquery"
)

const defaultCostMaxBytesBilled = int64(5 * 1024 * 1024 * 1024)

// CostAdapterConfig identifies the Cloud Billing detailed export queried by
// the optional cost reasoning module. QueryProjectID owns the BigQuery jobs;
// ExportProjectID owns the export dataset.
type CostAdapterConfig struct {
	QueryProjectID  string
	ExportProjectID string
	Dataset         string
	Table           string
	MaxBytesBilled  int64
}

// CostSourceConfig is the compiled runtime mapping between workload projects
// and one Cloud Billing detailed-export source. WorkloadProjectIDs must contain
// real configured project IDs, never user-facing aliases.
type CostSourceConfig struct {
	WorkloadProjectIDs []string
	CostAdapterConfig
}

type costSource struct {
	config CostAdapterConfig
	client *bigquery.Client
}

func normalizeCostAdapterConfig(cfg CostAdapterConfig, defaultProjectID string) CostAdapterConfig {
	cfg.QueryProjectID = strings.TrimSpace(cfg.QueryProjectID)
	cfg.ExportProjectID = strings.TrimSpace(cfg.ExportProjectID)
	cfg.Dataset = strings.TrimSpace(cfg.Dataset)
	cfg.Table = strings.TrimSpace(cfg.Table)
	if cfg.QueryProjectID == "" {
		cfg.QueryProjectID = defaultProjectID
	}
	if cfg.ExportProjectID == "" {
		cfg.ExportProjectID = cfg.QueryProjectID
	}
	if cfg.MaxBytesBilled <= 0 {
		cfg.MaxBytesBilled = defaultCostMaxBytesBilled
	}
	return cfg
}

func validateCostAdapterConfig(cfg CostAdapterConfig) error {
	if !costProjectIDRE.MatchString(cfg.QueryProjectID) {
		return fmt.Errorf("invalid cost query project ID %q", cfg.QueryProjectID)
	}
	if !costProjectIDRE.MatchString(cfg.ExportProjectID) {
		return fmt.Errorf("invalid billing export project ID %q", cfg.ExportProjectID)
	}
	if len(cfg.Dataset) > 1024 || !costDatasetIDRE.MatchString(cfg.Dataset) {
		return fmt.Errorf("invalid billing export dataset ID %q", cfg.Dataset)
	}
	if cfg.Table != "" && (len(cfg.Table) > 1024 || !costTableIDRE.MatchString(cfg.Table) || !strings.HasPrefix(cfg.Table, detailedBillingTablePrefix)) {
		return fmt.Errorf("invalid detailed billing export table ID %q", cfg.Table)
	}
	return nil
}

// NormalizeCostReasoningSources validates compiled workload-project mappings
// and applies non-routing defaults. It returns an independent copy.
func NormalizeCostReasoningSources(sources []CostSourceConfig, defaultProjectID string) ([]CostSourceConfig, error) {
	return normalizeCostSourceConfigs(sources, defaultProjectID, false)
}

func normalizeCostSourceConfigs(sources []CostSourceConfig, defaultProjectID string, legacy bool) ([]CostSourceConfig, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("at least one cost source is required")
	}
	out := make([]CostSourceConfig, 0, len(sources))
	seen := make(map[string]struct{})
	for i, source := range sources {
		if len(source.WorkloadProjectIDs) == 0 {
			return nil, fmt.Errorf("source %d: at least one workload project is required", i+1)
		}
		cfg := source.CostAdapterConfig
		if !legacy && strings.TrimSpace(cfg.QueryProjectID) == "" {
			return nil, fmt.Errorf("source %d: query_project_id is required", i+1)
		}
		cfg = normalizeCostAdapterConfig(cfg, defaultProjectID)
		if err := validateCostAdapterConfig(cfg); err != nil {
			return nil, fmt.Errorf("source %d: %w", i+1, err)
		}
		projects := make([]string, 0, len(source.WorkloadProjectIDs))
		for _, rawProjectID := range source.WorkloadProjectIDs {
			projectID := strings.TrimSpace(rawProjectID)
			if !costProjectIDRE.MatchString(projectID) {
				return nil, fmt.Errorf("source %d: invalid workload project ID %q", i+1, projectID)
			}
			if _, exists := seen[projectID]; exists {
				return nil, fmt.Errorf("workload project %q is assigned to more than one cost source", projectID)
			}
			seen[projectID] = struct{}{}
			projects = append(projects, projectID)
		}
		out = append(out, CostSourceConfig{WorkloadProjectIDs: projects, CostAdapterConfig: cfg})
	}
	return out, nil
}
