// Package config loads optional user-level defaults from ~/.aura-tracker.yaml.
package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// RecommenderBQExportConfig controls the optional BigQuery export of active recommendations.
type RecommenderBQExportConfig struct {
	Enabled bool   `yaml:"enabled"`
	Dataset string `yaml:"dataset"` // BigQuery dataset name to write recommendations into
}

// CostReasoningConfig controls the optional, read-only billing analysis tool.
// The export project can differ from the project being analysed.
type CostReasoningConfig struct {
	Enabled         bool   `yaml:"enabled"`
	QueryProjectID  string `yaml:"query_project_id"`
	ExportProjectID string `yaml:"export_project_id"`
	Dataset         string `yaml:"dataset"`
	Table           string `yaml:"table"`
	Timezone        string `yaml:"timezone"`
	HistoryDays     int    `yaml:"history_days"`
	MaxBytesBilled  int64  `yaml:"max_bytes_billed"`
}

// Config holds user-level defaults read from ~/.aura-tracker.yaml.
type Config struct {
	ProjectID         string                    `yaml:"project_id"`
	RecommenderExport RecommenderBQExportConfig `yaml:"recommender_export"`
	CostReasoning     CostReasoningConfig       `yaml:"cost_reasoning"`
}

// Load reads ~/.aura-tracker.yaml. A missing file is not an error; it returns
// a zero Config so callers always fall back to environment variables.
func Load() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(filepath.Join(home, ".aura-tracker.yaml"))
	if os.IsNotExist(err) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	return cfg, yaml.Unmarshal(data, &cfg)
}
