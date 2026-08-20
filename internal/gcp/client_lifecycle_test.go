package gcp

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/api/option"
)

func TestAdapterCloseIsIdempotent(t *testing.T) {
	adapter := &gcpAdapter{}
	if err := adapter.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestNewInitializesHTTPAndGRPCClientsWithSharedTransportLimits(t *testing.T) {
	adapter, err := New(context.Background(), "valid-project",
		WithModules(map[string]bool{}),
		WithClientOptions(option.WithoutAuthentication(), option.WithEndpoint("localhost:1")),
	)
	if err != nil {
		t.Fatalf("New() with local no-auth endpoint: %v", err)
	}
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Errorf("Close() error: %v", err)
		}
	}()
	if adapter.crm == nil || adapter.bq == nil || adapter.gcs == nil || adapter.runSvc == nil || adapter.runRevisions == nil {
		t.Fatalf("always-on clients were not initialized: crm=%v bq=%v gcs=%v run=%v revisions=%v",
			adapter.crm != nil, adapter.bq != nil, adapter.gcs != nil, adapter.runSvc != nil, adapter.runRevisions != nil)
	}
}

func TestValidateAdapterSettings(t *testing.T) {
	newValid := func() *gcpAdapter {
		return &gcpAdapter{
			limiter: rate.NewLimiter(1, 1), callTimeout: time.Second, graphTimeout: time.Second,
			traceBackend: "trace", log: slog.Default(), enabledModules: map[string]bool{"gke": true},
		}
	}
	valid := newValid()
	if err := validateAdapterSettings(valid, "valid-project"); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*gcpAdapter)
	}{
		{"invalid rate", func(a *gcpAdapter) { a.limiter = rate.NewLimiter(0, 0) }},
		{"invalid timeout", func(a *gcpAdapter) { a.callTimeout = 0 }},
		{"invalid trace backend", func(a *gcpAdapter) { a.traceBackend = "typo" }},
		{"unknown module", func(a *gcpAdapter) { a.enabledModules = map[string]bool{"typo": true} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := newValid()
			test.mutate(candidate)
			if err := validateAdapterSettings(candidate, "valid-project"); err == nil {
				t.Fatal("invalid settings were accepted")
			}
		})
	}
	if err := validateAdapterSettings(valid, "bad"); err == nil {
		t.Fatal("invalid project ID was accepted")
	}
}

func TestCostClientsAreReusedByQueryProject(t *testing.T) {
	adapter, err := New(context.Background(), "valid-project",
		WithModules(map[string]bool{}),
		WithClientOptions(option.WithoutAuthentication(), option.WithEndpoint("localhost:1")),
		WithCostReasoningSources([]CostSourceConfig{
			{WorkloadProjectIDs: []string{"dev-project-123"}, CostAdapterConfig: CostAdapterConfig{QueryProjectID: "shared-finops-123", Dataset: "dev_billing"}},
			{WorkloadProjectIDs: []string{"preprod-project-123"}, CostAdapterConfig: CostAdapterConfig{QueryProjectID: "shared-finops-123", Dataset: "preprod_billing"}},
			{WorkloadProjectIDs: []string{"prod-project-123"}, CostAdapterConfig: CostAdapterConfig{QueryProjectID: "valid-project", Dataset: "prod_billing"}},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Errorf("Close() error: %v", err)
		}
	}()
	if len(adapter.costClients) != 2 || len(adapter.ownedCostClients) != 1 {
		t.Fatalf("cost clients=%d owned=%d", len(adapter.costClients), len(adapter.ownedCostClients))
	}
	if adapter.costSources["dev-project-123"].client != adapter.costSources["preprod-project-123"].client {
		t.Fatal("sources with the same query project did not reuse a client")
	}
	if adapter.costSources["prod-project-123"].client != adapter.bq {
		t.Fatal("startup-project BigQuery client was not reused")
	}
}
