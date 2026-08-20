package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/internal/config"
	"github.com/asbrodova/aura-tracker-gcp/internal/environments"
	gcpadapter "github.com/asbrodova/aura-tracker-gcp/internal/gcp"
)

func compileCostReasoningSourcesIfEnabled(cfg config.CostReasoningConfig, registry *environments.Registry, enabled bool) ([]gcpadapter.CostSourceConfig, error) {
	if !enabled {
		return nil, nil
	}
	return compileCostReasoningSources(cfg, registry)
}

func compileCostReasoningSources(cfg config.CostReasoningConfig, registry *environments.Registry) ([]gcpadapter.CostSourceConfig, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if registry == nil {
		return nil, errors.New("environment registry is required")
	}
	if cfg.Timezone != "" {
		if _, err := time.LoadLocation(cfg.Timezone); err != nil {
			return nil, fmt.Errorf("invalid timezone %q: %w", cfg.Timezone, err)
		}
	}
	if cfg.HistoryDays != 0 && (cfg.HistoryDays < 14 || cfg.HistoryDays > 366) {
		return nil, errors.New("history_days must be between 14 and 366")
	}
	if cfg.MaxBytesBilled < 0 {
		return nil, errors.New("max_bytes_billed must be positive")
	}

	legacyConfigured := hasLegacyCostFields(cfg)
	if len(cfg.Sources) > 0 && legacyConfigured {
		return nil, errors.New("cost_reasoning.sources cannot be combined with legacy query_project_id, export_project_id, dataset, or table fields")
	}

	if len(cfg.Sources) == 0 {
		if strings.TrimSpace(cfg.Dataset) == "" {
			return nil, errors.New("cost_reasoning.dataset or BILLING_EXPORT_DATASET is required when cost reasoning is enabled")
		}
		queryProjectID := strings.TrimSpace(cfg.QueryProjectID)
		if queryProjectID == "" {
			queryProjectID = registry.Default().ProjectID
		}
		exportProjectID := strings.TrimSpace(cfg.ExportProjectID)
		if exportProjectID == "" {
			exportProjectID = queryProjectID
		}
		projects := make([]string, 0, len(registry.Environments()))
		for _, environment := range registry.Environments() {
			projects = append(projects, environment.ProjectID)
		}
		return gcpadapter.NormalizeCostReasoningSources([]gcpadapter.CostSourceConfig{{
			WorkloadProjectIDs: projects,
			CostAdapterConfig: gcpadapter.CostAdapterConfig{
				QueryProjectID: queryProjectID, ExportProjectID: exportProjectID,
				Dataset: cfg.Dataset, Table: cfg.Table, MaxBytesBilled: cfg.MaxBytesBilled,
			},
		}}, registry.Default().ProjectID)
	}

	compiled := make([]gcpadapter.CostSourceConfig, 0, len(cfg.Sources))
	mapped := make(map[string]int)
	for sourceIndex, raw := range cfg.Sources {
		if len(raw.Environments) == 0 {
			return nil, fmt.Errorf("cost_reasoning.sources[%d].environments is required", sourceIndex)
		}
		if strings.TrimSpace(raw.QueryProjectID) == "" {
			return nil, fmt.Errorf("cost_reasoning.sources[%d].query_project_id is required", sourceIndex)
		}
		if strings.TrimSpace(raw.Dataset) == "" {
			return nil, fmt.Errorf("cost_reasoning.sources[%d].dataset is required", sourceIndex)
		}
		projects := make([]string, 0, len(raw.Environments))
		for _, selector := range raw.Environments {
			if strings.TrimSpace(selector) == "" {
				return nil, fmt.Errorf("cost_reasoning.sources[%d] contains an empty environment selector", sourceIndex)
			}
			environment, err := registry.Resolve(selector)
			if err != nil {
				return nil, fmt.Errorf("cost_reasoning.sources[%d]: unknown environment %q", sourceIndex, selector)
			}
			if previous, exists := mapped[environment.ProjectID]; exists {
				return nil, fmt.Errorf("environment %q is assigned to sources[%d] and sources[%d]", environment.DisplayName(), previous, sourceIndex)
			}
			mapped[environment.ProjectID] = sourceIndex
			projects = append(projects, environment.ProjectID)
		}
		exportProjectID := strings.TrimSpace(raw.ExportProjectID)
		if exportProjectID == "" {
			exportProjectID = strings.TrimSpace(raw.QueryProjectID)
		}
		compiled = append(compiled, gcpadapter.CostSourceConfig{
			WorkloadProjectIDs: projects,
			CostAdapterConfig: gcpadapter.CostAdapterConfig{
				QueryProjectID: raw.QueryProjectID, ExportProjectID: exportProjectID,
				Dataset: raw.Dataset, Table: raw.Table, MaxBytesBilled: cfg.MaxBytesBilled,
			},
		})
	}
	for _, environment := range registry.Environments() {
		if _, exists := mapped[environment.ProjectID]; !exists {
			return nil, fmt.Errorf("cost reasoning source is missing for environment %q", environment.DisplayName())
		}
	}
	return gcpadapter.NormalizeCostReasoningSources(compiled, registry.Default().ProjectID)
}

// costSourceProjectIDReplacements prevents query/export project IDs from
// escaping through BigQuery errors. Environment project aliases are applied by
// the MCP server first and therefore take precedence over this generic label.
func costSourceProjectIDReplacements(sources []gcpadapter.CostSourceConfig) map[string]string {
	replacements := make(map[string]string)
	for _, source := range sources {
		if projectID := strings.TrimSpace(source.QueryProjectID); projectID != "" {
			replacements[projectID] = "[COST_PROJECT_ID]"
		}
		if projectID := strings.TrimSpace(source.ExportProjectID); projectID != "" {
			replacements[projectID] = "[COST_PROJECT_ID]"
		}
	}
	return replacements
}

func hasLegacyCostFields(cfg config.CostReasoningConfig) bool {
	return strings.TrimSpace(cfg.QueryProjectID) != "" || strings.TrimSpace(cfg.ExportProjectID) != "" ||
		strings.TrimSpace(cfg.Dataset) != "" || strings.TrimSpace(cfg.Table) != ""
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}
