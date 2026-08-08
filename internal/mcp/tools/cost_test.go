package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

type fakeCostExplainer struct {
	response models.ExplainCostResponse
	err      error
	captured models.ExplainCostRequest
}

func (f *fakeCostExplainer) Explain(_ context.Context, request models.ExplainCostRequest) (models.ExplainCostResponse, error) {
	f.captured = request
	return f.response, f.err
}

func TestCostToolDefinition(t *testing.T) {
	t.Parallel()
	registered := NewCostTools(&fakeCostExplainer{}, nil).GetTools()
	if len(registered) != 1 {
		t.Fatalf("tool count = %d, want 1", len(registered))
	}
	tool := registered[0].Tool
	if tool.Name != "gcp_cost_explain" {
		t.Fatalf("tool name = %q", tool.Name)
	}
	for _, property := range []string{"project_id", "period", "comparison", "start_date", "end_date", "timezone", "detail_level", "max_results", "include_idle", "include_traffic"} {
		if _, ok := tool.InputSchema.Properties[property]; !ok {
			t.Errorf("input schema missing %q", property)
		}
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("cost explanation must be annotated read-only")
	}
	if tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
		t.Error("cost explanation must be annotated non-destructive")
	}
}

func TestCostToolHandlerPropagatesRequest(t *testing.T) {
	t.Parallel()
	explainer := &fakeCostExplainer{response: models.ExplainCostResponse{Status: "complete", Summary: "cost increased"}}
	module := NewCostTools(explainer, nil)
	idle, traffic := false, true
	request := models.ExplainCostRequest{ProjectID: "prod-project", Period: "last_30_complete_days", MaxResults: 7, IncludeIdle: &idle, IncludeTraffic: &traffic}
	result, err := module.explainHandler(context.Background(), mcp.CallToolRequest{}, request)
	if err != nil {
		t.Fatalf("explainHandler() error = %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("unexpected result: %+v", result)
	}
	if explainer.captured.ProjectID != request.ProjectID || explainer.captured.Period != request.Period ||
		explainer.captured.MaxResults != request.MaxResults || explainer.captured.IncludeIdle == nil || *explainer.captured.IncludeIdle {
		t.Fatalf("captured request = %+v", explainer.captured)
	}
}

func TestCostToolHandlerReturnsToolError(t *testing.T) {
	t.Parallel()
	module := NewCostTools(&fakeCostExplainer{err: errors.New("billing export unavailable")}, nil)
	result, err := module.explainHandler(context.Background(), mcp.CallToolRequest{}, models.ExplainCostRequest{ProjectID: "prod-project"})
	if err == nil || result != nil {
		t.Fatalf("expected wrapped Go error, result=%+v err=%v", result, err)
	}
}
