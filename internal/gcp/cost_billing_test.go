package gcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"cloud.google.com/go/bigquery"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

func TestParseCostFactTimesRejectsOverlappingPeriods(t *testing.T) {
	t.Parallel()
	request := models.CollectCostFactsRequest{
		CurrentStart: "2026-08-01T00:00:00Z", CurrentEnd: "2026-08-08T00:00:00Z",
		BaselineStart: "2026-07-26T00:00:00Z", BaselineEnd: "2026-08-02T00:00:00Z",
		HistoryStart: "2026-05-01T00:00:00Z",
	}
	if _, _, _, _, _, err := parseCostFactTimes(request); err == nil {
		t.Fatal("parseCostFactTimes() accepted overlapping current and baseline periods")
	}
}

func TestNormalizeCostSourcesRejectsDuplicateWorkloadMapping(t *testing.T) {
	t.Parallel()
	_, err := NormalizeCostReasoningSources([]CostSourceConfig{
		{WorkloadProjectIDs: []string{"dev-project-123"}, CostAdapterConfig: CostAdapterConfig{QueryProjectID: "finops-project-123", Dataset: "dev_billing"}},
		{WorkloadProjectIDs: []string{"dev-project-123"}, CostAdapterConfig: CostAdapterConfig{QueryProjectID: "finops-project-456", Dataset: "other_billing"}},
	}, "dev-project-123")
	if err == nil || !strings.Contains(err.Error(), "more than one") {
		t.Fatalf("duplicate mapping error = %v", err)
	}
}

func TestCollectCostFactsDoesNotFallbackForUnmappedProject(t *testing.T) {
	t.Parallel()
	adapter := &gcpAdapter{enableCostReasoning: true, costSources: map[string]*costSource{}}
	_, err := adapter.CollectCostFacts(context.Background(), models.CollectCostFactsRequest{ProjectID: "preprod-project-123"})
	var missing *ports.CostSourceNotConfiguredError
	if !errors.As(err, &missing) || missing.ProjectID != "preprod-project-123" {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestCostSourceSelectionKeepsExportDatasetsIsolated(t *testing.T) {
	t.Parallel()
	adapter := &gcpAdapter{costSources: map[string]*costSource{
		"dev-project-123":     {config: CostAdapterConfig{ExportProjectID: "billing-dev-123", Dataset: "dev_billing"}},
		"preprod-project-123": {config: CostAdapterConfig{ExportProjectID: "billing-preprod-123", Dataset: "preprod_billing"}},
	}}
	dev, err := adapter.costSourceForProject("dev-project-123")
	if err != nil {
		t.Fatal(err)
	}
	preprod, err := adapter.costSourceForProject("preprod-project-123")
	if err != nil {
		t.Fatal(err)
	}
	tableID := "gcp_billing_export_resource_v1_ABC"
	if got := costTableReference(dev, tableID); got != "`billing-dev-123.dev_billing."+tableID+"`" {
		t.Fatalf("dev table reference = %q", got)
	}
	if got := costTableReference(preprod, tableID); got != "`billing-preprod-123.preprod_billing."+tableID+"`" {
		t.Fatalf("preprod table reference = %q", got)
	}
}

func TestCostPartitionPredicateSupportsIngestionAndFieldPartitioning(t *testing.T) {
	t.Parallel()
	if got := costPartitionPredicate(&bigquery.TableMetadata{TimePartitioning: &bigquery.TimePartitioning{}}); got != "AND _PARTITIONTIME >= TIMESTAMP(@partition_start)" {
		t.Fatalf("ingestion predicate = %q", got)
	}
	if got := costPartitionPredicate(&bigquery.TableMetadata{TimePartitioning: &bigquery.TimePartitioning{Field: "export_time"}}); got != "AND DATE(`export_time`) >= DATE(@partition_start)" {
		t.Fatalf("field predicate = %q", got)
	}
	if got := costPartitionPredicate(&bigquery.TableMetadata{TimePartitioning: &bigquery.TimePartitioning{Field: "unsafe`field"}}); got != "" {
		t.Fatalf("unsafe field predicate = %q", got)
	}
}

func TestParseCostFactTimesAcceptsAdjacentPeriods(t *testing.T) {
	t.Parallel()
	request := models.CollectCostFactsRequest{
		CurrentStart: "2026-08-01T00:00:00Z", CurrentEnd: "2026-08-08T00:00:00Z",
		BaselineStart: "2026-07-25T00:00:00Z", BaselineEnd: "2026-08-01T00:00:00Z",
		HistoryStart: "2026-05-01T00:00:00Z",
	}
	currentStart, _, _, baselineEnd, _, err := parseCostFactTimes(request)
	if err != nil {
		t.Fatalf("parseCostFactTimes() error = %v", err)
	}
	if !currentStart.Equal(baselineEnd) {
		t.Fatalf("periods are not adjacent: current=%v baseline_end=%v", currentStart, baselineEnd)
	}
}

func TestCostQueriesUseParametersAndAggregatedResults(t *testing.T) {
	t.Parallel()
	table := "`billing-project.billing.gcp_billing_export_resource_v1_ABC123`"
	contributor := buildCostContributorSQL(table, "AND _PARTITIONTIME >= TIMESTAMP(@partition_start)")
	history := buildCostHistorySQL(table, "AND _PARTITIONTIME >= TIMESTAMP(@partition_start)")
	for name, query := range map[string]string{"contributor": contributor, "history": history} {
		for _, expected := range []string{table, "@project_id", "@current_end", "@partition_start"} {
			if !strings.Contains(query, expected) {
				t.Errorf("%s query missing %q", name, expected)
			}
		}
	}
	if !strings.Contains(contributor, "ROW_NUMBER() OVER") || !strings.Contains(contributor, "@max_rows") {
		t.Error("contributor query must rank and cap aggregates in BigQuery")
	}
	if !strings.Contains(contributor, "ABS(current_net - baseline_net)") || !strings.Contains(contributor, "change_rank") {
		t.Error("contributor query must retain large decreases as well as top increases")
	}
	if !strings.Contains(contributor, "COALESCE(cost_type, 'regular') = 'regular'") || !strings.Contains(history, "COALESCE(cost_type, 'regular') = 'regular'") {
		t.Error("cost queries must exclude tax and adjustment rows from net usage cost")
	}
	if strings.Contains(contributor, "prod-project") || strings.Contains(history, "prod-project") {
		t.Error("queries must not interpolate workload project IDs")
	}
	if !strings.Contains(history, "COALESCE(FORMAT_TIMESTAMP") {
		t.Error("history query must return non-null metadata for empty exports")
	}
	if !strings.Contains(history, "QUALIFY ROW_NUMBER()") || !strings.Contains(history, "<= @max_rows") {
		t.Error("history query must bound first-seen resource rows in BigQuery")
	}
}

func TestNormalizeCostAdapterConfig(t *testing.T) {
	t.Parallel()
	cfg := normalizeCostAdapterConfig(CostAdapterConfig{Dataset: " billing ", Table: " table "}, "workload-project")
	if cfg.QueryProjectID != "workload-project" || cfg.ExportProjectID != "workload-project" {
		t.Fatalf("project defaults = %q / %q", cfg.QueryProjectID, cfg.ExportProjectID)
	}
	if cfg.Dataset != "billing" || cfg.Table != "table" {
		t.Fatalf("normalized strings = %+v", cfg)
	}
	if cfg.MaxBytesBilled != defaultCostMaxBytesBilled {
		t.Fatalf("defaults = %+v", cfg)
	}
	if cfg.MaxBytesBilled <= 0 {
		t.Fatal("normalized limits must be positive")
	}
}

func TestValidateCostAdapterConfig(t *testing.T) {
	t.Parallel()
	valid := normalizeCostAdapterConfig(CostAdapterConfig{
		QueryProjectID: "query-project-1", ExportProjectID: "billing-project-1", Dataset: "cloud_billing",
		Table: "gcp_billing_export_resource_v1_ABC123",
	}, "unused-project")
	if err := validateCostAdapterConfig(valid); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	invalid := valid
	invalid.Table = "gcp_billing_export_v1_standard"
	if err := validateCostAdapterConfig(invalid); err == nil {
		t.Fatal("standard export table must not be accepted as a detailed export")
	}
}

func TestCostIdentifierValidation(t *testing.T) {
	t.Parallel()
	validProjects := []string{"prod-12345", "billing-export-1"}
	for _, project := range validProjects {
		if !costProjectIDRE.MatchString(project) {
			t.Errorf("valid project ID rejected: %q", project)
		}
	}
	for _, invalid := range []string{"x", "Prod Project", "project;drop"} {
		if costProjectIDRE.MatchString(invalid) {
			t.Errorf("invalid project ID accepted: %q", invalid)
		}
	}
	for _, dataset := range []string{"cloud_billing", "123_billing", "_hidden"} {
		if !costDatasetIDRE.MatchString(dataset) {
			t.Errorf("valid dataset ID rejected: %q", dataset)
		}
	}
}
