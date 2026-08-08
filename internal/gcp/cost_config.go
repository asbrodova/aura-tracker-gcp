package gcp

import (
	"fmt"
	"strings"
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
