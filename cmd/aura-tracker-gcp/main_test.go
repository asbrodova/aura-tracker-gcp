package main

import (
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/internal/config"
)

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
		"COST_QUERY_MAX_BYTES", "COST_REASONING_HISTORY_DAYS",
	} {
		t.Setenv(name, "")
	}
}
