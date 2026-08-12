// Package config loads optional user-level defaults from ~/.aura-tracker.yaml.
package config

import (
	"bytes"
	"fmt"
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

// SecurityAuditSuppressionConfig describes one time-bounded accepted risk.
// Resource supports * and ? wildcards and is matched against the canonical GCP
// resource name returned in the finding.
type SecurityAuditSuppressionConfig struct {
	RuleID    string `yaml:"rule_id"`
	Resource  string `yaml:"resource"`
	Reason    string `yaml:"reason"`
	Owner     string `yaml:"owner"`
	ExpiresAt string `yaml:"expires_at"`
}

type SecurityAuditConfig struct {
	KubernetesAccess         string                           `yaml:"kubernetes_access"`
	FleetProjectID           string                           `yaml:"fleet_project_id"`
	ClusterConcurrency       int                              `yaml:"cluster_concurrency"`
	PerClusterTimeoutSeconds int                              `yaml:"per_cluster_timeout_seconds"`
	MaxResourcesPerKind      int                              `yaml:"max_resources_per_kind"`
	Suppressions             []SecurityAuditSuppressionConfig `yaml:"suppressions"`
}

// EnvironmentConfig maps a GCP project to an optional chat-safe alias.
// Default is implicit for a single environment and required on exactly one
// entry when multiple environments are configured.
type EnvironmentConfig struct {
	ProjectID string `yaml:"project_id" json:"project_id"`
	Alias     string `yaml:"alias" json:"alias"`
	Default   bool   `yaml:"default" json:"default"`
}

// Config holds user-level defaults read from ~/.aura-tracker.yaml.
type Config struct {
	ProjectID         string                    `yaml:"project_id"`
	Environments      []EnvironmentConfig       `yaml:"environments"`
	RecommenderExport RecommenderBQExportConfig `yaml:"recommender_export"`
	CostReasoning     CostReasoningConfig       `yaml:"cost_reasoning"`
	SecurityAudit     SecurityAuditConfig       `yaml:"security_audit"`
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
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", filepath.Join(home, ".aura-tracker.yaml"), err)
	}
	return cfg, nil
}
