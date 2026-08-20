// Package config loads optional user-level defaults from ~/.aura-tracker.yaml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// RecommenderBQExportConfig controls the optional BigQuery export of active recommendations.
type RecommenderBQExportConfig struct {
	Enabled bool   `yaml:"enabled"`
	Dataset string `yaml:"dataset"` // BigQuery dataset name to write recommendations into
}

// CostReasoningSourceConfig maps one or more configured environments to a
// Cloud Billing detailed-export source.
type CostReasoningSourceConfig struct {
	Environments    []string `yaml:"environments" json:"environments"`
	QueryProjectID  string   `yaml:"query_project_id" json:"query_project_id"`
	ExportProjectID string   `yaml:"export_project_id" json:"export_project_id"`
	Dataset         string   `yaml:"dataset" json:"dataset"`
	Table           string   `yaml:"table" json:"table"`
}

// CostReasoningConfig controls the optional, read-only billing analysis tool.
// The export project can differ from the project being analysed.
type CostReasoningConfig struct {
	Enabled        bool                        `yaml:"enabled" json:"enabled"`
	Timezone       string                      `yaml:"timezone" json:"timezone"`
	HistoryDays    int                         `yaml:"history_days" json:"history_days"`
	MaxBytesBilled int64                       `yaml:"max_bytes_billed" json:"max_bytes_billed"`
	Sources        []CostReasoningSourceConfig `yaml:"sources" json:"sources"`

	// Deprecated: use Sources. These fields remain for one compatibility cycle.
	QueryProjectID  string `yaml:"query_project_id" json:"query_project_id"`
	ExportProjectID string `yaml:"export_project_id" json:"export_project_id"`
	Dataset         string `yaml:"dataset" json:"dataset"`
	Table           string `yaml:"table" json:"table"`
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
		// The user-level file is optional. Headless/container runtimes may have
		// neither HOME nor a passwd entry, which must not make environment-only
		// configuration fail at startup.
		return Config{}, nil
	}
	path := filepath.Join(home, ".aura-tracker.yaml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Config{}, nil
	}
	if errors.Is(err, fs.ErrPermission) {
		// An implicitly discovered optional config is not a hard dependency.
		// Explicit runtime configuration still comes from validated flags/env.
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}
