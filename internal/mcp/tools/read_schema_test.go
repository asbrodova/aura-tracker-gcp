package tools

import (
	"log/slog"
	"testing"
)

func TestMetricsSchemaExposesExplicitInterval(t *testing.T) {
	tool := NewMonitoringTools(&mockGCPService{}, slog.Default()).GetTools()[0].Tool
	for _, property := range []string{"lookback_minutes", "start_time", "end_time"} {
		if _, ok := tool.InputSchema.Properties[property]; !ok {
			t.Errorf("metrics input schema missing %q", property)
		}
	}
}

func TestIAMPermissionsSchemaExposesPermissionList(t *testing.T) {
	tool := NewIAMTools(&mockGCPService{}, slog.Default()).GetTools()[0].Tool
	if _, ok := tool.InputSchema.Properties["permissions"]; !ok {
		t.Fatal("IAM test-permissions schema is missing permissions")
	}
}
