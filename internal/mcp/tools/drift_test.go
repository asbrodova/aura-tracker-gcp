package tools

import (
	"context"
	"log/slog"
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

type fakeEnvironmentComparer struct{}

func (fakeEnvironmentComparer) Compare(context.Context, models.CompareEnvironmentsRequest) (models.CompareEnvironmentsResponse, error) {
	return models.CompareEnvironmentsResponse{}, nil
}

func TestDriftToolSchemaSupportsWholeAndComponentComparisons(t *testing.T) {
	tool := NewDriftTools(fakeEnvironmentComparer{}, slog.Default()).GetTools()[0].Tool
	for _, property := range []string{"environment_a", "environment_b", "components", "resource_names", "locations", "namespaces", "detail_level", "include_unchanged", "max_changes"} {
		if _, ok := tool.InputSchema.Properties[property]; !ok {
			t.Errorf("drift schema missing %q", property)
		}
	}
	required := make(map[string]bool)
	for _, property := range tool.InputSchema.Required {
		required[property] = true
	}
	if !required["environment_a"] || !required["environment_b"] {
		t.Fatalf("required fields = %#v", tool.InputSchema.Required)
	}
	if required["components"] {
		t.Fatal("components must be optional so omission means whole-environment comparison")
	}
}
