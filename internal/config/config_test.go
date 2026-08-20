package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeUserConfig(t *testing.T, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".aura-tracker.yaml"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsUnknownYAMLFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeUserConfig(t, "project_id: valid-project-123\nproject_typo: ignored-before\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "project_typo") {
		t.Fatalf("unknown field error = %v", err)
	}
}

func TestLoadAcceptsKnownFieldsAndMissingFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	missing, err := Load()
	if err != nil || missing.ProjectID != "" {
		t.Fatalf("missing config = %+v, %v", missing, err)
	}
	writeUserConfig(t, "project_id: valid-project-123\nenvironments:\n  - project_id: dev-project-123\n    alias: dev\n    default: true\n")
	cfg, err := Load()
	if err != nil || cfg.ProjectID != "valid-project-123" || len(cfg.Environments) != 1 || cfg.Environments[0].Alias != "dev" {
		t.Fatalf("loaded config = %+v, %v", cfg, err)
	}
}

func TestLoadWithoutHomeTreatsUserConfigAsOptional(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := Load(); err != nil {
		t.Fatalf("Load() without HOME returned %v", err)
	}
}

func TestLoadCostReasoningSourcesAndRejectsUnknownNestedFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeUserConfig(t, `environments:
  - project_id: dev-project-123
    alias: dev
    default: true
cost_reasoning:
  enabled: true
  sources:
    - environments: [dev]
      query_project_id: finops-project-123
      export_project_id: billing-project-123
      dataset: cloud_billing
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.CostReasoning.Sources) != 1 || cfg.CostReasoning.Sources[0].Environments[0] != "dev" {
		t.Fatalf("sources = %+v", cfg.CostReasoning.Sources)
	}

	t.Setenv("HOME", t.TempDir())
	writeUserConfig(t, `project_id: dev-project-123
cost_reasoning:
  sources:
    - environments: [dev]
      query_project_id: finops-project-123
      dataset: cloud_billing
      dataset_typo: rejected
`)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "dataset_typo") {
		t.Fatalf("unknown nested field error = %v", err)
	}
}
