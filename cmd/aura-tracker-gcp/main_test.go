package main

import (
	"strings"
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/internal/config"
	"github.com/asbrodova/aura-tracker-gcp/internal/environments"
	gcpadapter "github.com/asbrodova/aura-tracker-gcp/internal/gcp"
)

func TestLoadEnvironmentRegistryLegacyPrecedence(t *testing.T) {
	registry, err := loadEnvironmentRegistry(config.Config{ProjectID: "yaml-project"}, func(name string) string {
		if name == "GCP_PROJECT_ID" {
			return "env-project"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := registry.Default().ProjectID; got != "env-project" {
		t.Fatalf("default project = %q", got)
	}
}

func TestLoadEnvironmentRegistryYAMLMultiProject(t *testing.T) {
	registry, err := loadEnvironmentRegistry(config.Config{Environments: []config.EnvironmentConfig{
		{ProjectID: "dev-project", Alias: "dev", Default: true},
		{ProjectID: "prod-project", Alias: "prod"},
	}}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	prod, err := registry.Resolve("PROD")
	if err != nil || prod.ProjectID != "prod-project" {
		t.Fatalf("Resolve(PROD) = %+v, %v", prod, err)
	}
}

func TestLoadEnvironmentRegistryJSON(t *testing.T) {
	registry, err := loadEnvironmentRegistry(config.Config{}, func(name string) string {
		if name == "GCP_ENVIRONMENTS_JSON" {
			return `[{"project_id":"dev-project","alias":"dev","default":true},{"project_id":"prod-project","alias":"prod"}]`
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(registry.Environments()); got != 2 {
		t.Fatalf("environment count = %d", got)
	}
}

func TestLoadEnvironmentRegistryJSONRejectsUnknownFields(t *testing.T) {
	_, err := loadEnvironmentRegistry(config.Config{}, func(name string) string {
		if name == "GCP_ENVIRONMENTS_JSON" {
			return `[{"project_id":"dev-project","alias":"dev","default":true,"typo":true}]`
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadEnvironmentRegistryRejectsConflictingSources(t *testing.T) {
	_, err := loadEnvironmentRegistry(config.Config{Environments: []config.EnvironmentConfig{{ProjectID: "yaml", Alias: "dev"}}}, func(name string) string {
		if name == "GCP_PROJECT_ID" {
			return "env-project"
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadEnvironmentRegistryRequiresAProject(t *testing.T) {
	if _, err := loadEnvironmentRegistry(config.Config{}, func(string) string { return "" }); err == nil {
		t.Fatal("expected missing environment error")
	}
}

func TestParseModulesFlagValidatesNames(t *testing.T) {
	modules, err := parseModulesFlag("gke, monitoring")
	if err != nil {
		t.Fatal(err)
	}
	if !modules["gke"] || !modules["monitoring"] || len(modules) != 2 {
		t.Fatalf("modules = %#v", modules)
	}
	if _, err := parseModulesFlag("gke,typo"); err == nil {
		t.Fatal("unknown module was accepted")
	}
	if _, err := parseModulesFlag("gke,"); err == nil {
		t.Fatal("empty module was accepted")
	}
}

func TestReadBoolEnvIsStrict(t *testing.T) {
	getenv := func(string) string { return "yes" }
	if _, err := readBoolEnv("FEATURE", false, getenv); err == nil {
		t.Fatal("ambiguous boolean was accepted")
	}
	if value, err := readBoolEnv("FEATURE", true, func(string) string { return "" }); err != nil || !value {
		t.Fatalf("default boolean = %v, %v", value, err)
	}
}

func TestSecurityAuditConfigConversion(t *testing.T) {
	got := securityAuditConfig(config.SecurityAuditConfig{
		KubernetesAccess: "connect_gateway", FleetProjectID: "fleet", ClusterConcurrency: 3,
		PerClusterTimeoutSeconds: 15, MaxResourcesPerKind: 500,
		Suppressions: []config.SecurityAuditSuppressionConfig{{
			RuleID: "PUB-001", Resource: "//run.googleapis.com/*", Reason: "public API", Owner: "platform", ExpiresAt: "2026-12-01",
		}},
	})
	if got.KubernetesAccess != "connect_gateway" || got.FleetProjectID != "fleet" || got.ClusterConcurrency != 3 ||
		got.PerClusterTimeoutSeconds != 15 || got.MaxResourcesPerKind != 500 || len(got.Suppressions) != 1 ||
		got.Suppressions[0].Owner != "platform" || got.Suppressions[0].RuleID != "PUB-001" {
		t.Fatalf("conversion = %+v", got)
	}
}

func TestApplyCostReasoningEnvOverridesConfig(t *testing.T) {
	setCostEnv(t)
	t.Setenv("COST_REASONING_ENABLED", "true")
	t.Setenv("COST_QUERY_PROJECT_ID", "query-project")
	t.Setenv("BILLING_EXPORT_PROJECT_ID", "billing-project")
	t.Setenv("BILLING_EXPORT_DATASET", "cloud_billing")
	t.Setenv("BILLING_EXPORT_TABLE", "gcp_billing_export_resource_v1_ABC")
	t.Setenv("COST_REASONING_TIMEZONE", "Asia/Makassar")
	t.Setenv("COST_QUERY_MAX_BYTES", "1048576")
	t.Setenv("COST_REASONING_HISTORY_DAYS", "120")

	cfg := config.CostReasoningConfig{}
	if err := applyCostReasoningEnv(&cfg); err != nil {
		t.Fatalf("applyCostReasoningEnv() error = %v", err)
	}
	if !cfg.Enabled || cfg.QueryProjectID != "query-project" || cfg.ExportProjectID != "billing-project" ||
		cfg.Dataset != "cloud_billing" || cfg.Table != "gcp_billing_export_resource_v1_ABC" ||
		cfg.Timezone != "Asia/Makassar" || cfg.MaxBytesBilled != 1048576 || cfg.HistoryDays != 120 {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestApplyCostReasoningEnvRejectsInvalidLimits(t *testing.T) {
	setCostEnv(t)
	t.Setenv("COST_QUERY_MAX_BYTES", "unlimited")
	if err := applyCostReasoningEnv(&config.CostReasoningConfig{}); err == nil {
		t.Fatal("expected invalid max bytes error")
	}

	setCostEnv(t)
	t.Setenv("COST_REASONING_HISTORY_DAYS", "7")
	if err := applyCostReasoningEnv(&config.CostReasoningConfig{}); err == nil {
		t.Fatal("expected invalid history days error")
	}
}

func TestApplyCostReasoningEnvRejectsAmbiguousBoolean(t *testing.T) {
	setCostEnv(t)
	t.Setenv("COST_REASONING_ENABLED", "yes")
	if err := applyCostReasoningEnv(&config.CostReasoningConfig{}); err == nil {
		t.Fatal("expected strict boolean error")
	}
}

func setCostEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"COST_REASONING_ENABLED", "COST_QUERY_PROJECT_ID", "BILLING_EXPORT_PROJECT_ID",
		"BILLING_EXPORT_DATASET", "BILLING_EXPORT_TABLE", "COST_REASONING_TIMEZONE",
		"COST_QUERY_MAX_BYTES", "COST_REASONING_HISTORY_DAYS", "COST_REASONING_SOURCES_JSON",
	} {
		t.Setenv(name, "")
	}
}

func testEnvironmentRegistry(t *testing.T) *environments.Registry {
	t.Helper()
	registry, err := environments.NewRegistry([]environments.Environment{
		{ProjectID: "dev-project-123", Alias: "dev", Default: true},
		{ProjectID: "preprod-project-123", Alias: "preprod"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestCompileCostReasoningSourcesRoutesAliasesAndProjectIDs(t *testing.T) {
	registry := testEnvironmentRegistry(t)
	sources, err := compileCostReasoningSources(config.CostReasoningConfig{
		Enabled: true, MaxBytesBilled: 1234,
		Sources: []config.CostReasoningSourceConfig{
			{Environments: []string{"DEV"}, QueryProjectID: "finops-dev-123", Dataset: "dev_billing"},
			{Environments: []string{"preprod-project-123"}, QueryProjectID: "finops-preprod-123", ExportProjectID: "billing-preprod-123", Dataset: "preprod_billing"},
		},
	}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 || sources[0].WorkloadProjectIDs[0] != "dev-project-123" ||
		sources[0].ExportProjectID != "finops-dev-123" || sources[1].WorkloadProjectIDs[0] != "preprod-project-123" ||
		sources[1].Dataset != "preprod_billing" || sources[1].MaxBytesBilled != 1234 {
		t.Fatalf("compiled sources = %+v", sources)
	}
}

func TestCompileLegacyCostSourceCoversEveryEnvironment(t *testing.T) {
	sources, err := compileCostReasoningSources(config.CostReasoningConfig{
		Enabled: true, Dataset: "central_billing",
	}, testEnvironmentRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || len(sources[0].WorkloadProjectIDs) != 2 || sources[0].QueryProjectID != "dev-project-123" {
		t.Fatalf("legacy source = %+v", sources)
	}
}

func TestDisabledCostModuleSkipsInvalidSourceConfiguration(t *testing.T) {
	sources, err := compileCostReasoningSourcesIfEnabled(config.CostReasoningConfig{
		Enabled: true,
		// No dataset: this would fail compilation if the disabled module were validated.
	}, testEnvironmentRegistry(t), false)
	if err != nil || sources != nil {
		t.Fatalf("disabled module sources = %+v, error = %v", sources, err)
	}
}

func TestCostSourceProjectIDReplacementsMasksEveryDistinctBillingProject(t *testing.T) {
	replacements := costSourceProjectIDReplacements([]gcpadapter.CostSourceConfig{
		{
			WorkloadProjectIDs: []string{"dev-project-123"},
			CostAdapterConfig: gcpadapter.CostAdapterConfig{
				QueryProjectID: "query-dev-123", ExportProjectID: "billing-dev-123", Dataset: "dev_billing",
			},
		},
		{
			WorkloadProjectIDs: []string{"preprod-project-123"},
			CostAdapterConfig: gcpadapter.CostAdapterConfig{
				QueryProjectID: "shared-finops-123", ExportProjectID: "shared-finops-123", Dataset: "preprod_billing",
			},
		},
	})
	for _, projectID := range []string{"query-dev-123", "billing-dev-123", "shared-finops-123"} {
		if replacements[projectID] != "[COST_PROJECT_ID]" {
			t.Fatalf("replacement for %q = %q", projectID, replacements[projectID])
		}
	}
	if _, exists := replacements["dev-project-123"]; exists {
		t.Fatal("workload project should continue to use its environment alias")
	}
}

func TestCompileCostReasoningSourcesRejectsAmbiguousOrPartialMappings(t *testing.T) {
	registry := testEnvironmentRegistry(t)
	tests := []struct {
		name    string
		config  config.CostReasoningConfig
		message string
	}{
		{"unknown", config.CostReasoningConfig{Enabled: true, Sources: []config.CostReasoningSourceConfig{{Environments: []string{"qa"}, QueryProjectID: "finops-dev-123", Dataset: "billing"}}}, "unknown environment"},
		{"duplicate", config.CostReasoningConfig{Enabled: true, Sources: []config.CostReasoningSourceConfig{{Environments: []string{"dev"}, QueryProjectID: "finops-dev-123", Dataset: "billing"}, {Environments: []string{"DEV"}, QueryProjectID: "finops-dev-456", Dataset: "billing"}}}, "assigned to"},
		{"partial", config.CostReasoningConfig{Enabled: true, Sources: []config.CostReasoningSourceConfig{{Environments: []string{"dev"}, QueryProjectID: "finops-dev-123", Dataset: "billing"}}}, "missing for environment"},
		{"mixed legacy", config.CostReasoningConfig{Enabled: true, Dataset: "legacy", Sources: []config.CostReasoningSourceConfig{{Environments: []string{"dev", "preprod"}, QueryProjectID: "finops-dev-123", Dataset: "billing"}}}, "cannot be combined"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := compileCostReasoningSources(test.config, registry)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want %q", err, test.message)
			}
		})
	}
}

func TestApplyCostReasoningSourcesJSONIsStrictAndConflictsWithLegacy(t *testing.T) {
	setCostEnv(t)
	valid := `[{
		"environments":["dev","preprod"],
		"query_project_id":"finops-project-123",
		"dataset":"billing"
	}]`
	if err := applyCostReasoningEnvWithLookup(&config.CostReasoningConfig{}, func(name string) string {
		if name == "COST_REASONING_SOURCES_JSON" {
			return valid
		}
		return ""
	}); err != nil {
		t.Fatal(err)
	}

	for name, raw := range map[string]string{
		"unknown field": `[{"environments":["dev"],"query_project_id":"finops-project-123","dataset":"billing","typo":true}]`,
		"trailing JSON": valid + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := applyCostReasoningEnvWithLookup(&config.CostReasoningConfig{}, func(env string) string {
				if env == "COST_REASONING_SOURCES_JSON" {
					return raw
				}
				return ""
			}); err == nil {
				t.Fatal("invalid JSON accepted")
			}
		})
	}

	err := applyCostReasoningEnvWithLookup(&config.CostReasoningConfig{}, func(name string) string {
		switch name {
		case "COST_REASONING_SOURCES_JSON":
			return valid
		case "BILLING_EXPORT_DATASET":
			return "legacy"
		default:
			return ""
		}
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("conflict error = %v", err)
	}
}
