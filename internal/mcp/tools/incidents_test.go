package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

type fakeDiagnoser struct {
	response models.DiagnoseIncidentResponse
	err      error
	captured models.DiagnoseIncidentRequest
}

func (f *fakeDiagnoser) Diagnose(_ context.Context, req models.DiagnoseIncidentRequest) (models.DiagnoseIncidentResponse, error) {
	f.captured = req
	return f.response, f.err
}

func TestIncidentToolDefinition(t *testing.T) {
	t.Parallel()
	module := NewIncidentTools(&fakeDiagnoser{}, nil)
	tools := module.GetTools()
	if len(tools) != 1 {
		t.Fatalf("tool count = %d, want 1", len(tools))
	}
	tool := tools[0].Tool
	if tool.Name != "gcp_incident_diagnose" {
		t.Fatalf("tool name = %q", tool.Name)
	}
	for _, property := range []string{"project_id", "environment", "service_name", "region", "lookback_minutes", "baseline_minutes", "max_services", "max_dependencies", "detail_level", "include_platform_health"} {
		if _, ok := tool.InputSchema.Properties[property]; !ok {
			t.Errorf("input schema missing %q", property)
		}
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("incident diagnosis must be annotated read-only")
	}
}

func TestIncidentToolHandlerPropagatesRequest(t *testing.T) {
	t.Parallel()
	diagnoser := &fakeDiagnoser{response: models.DiagnoseIncidentResponse{Status: "degraded", Summary: "test"}}
	module := NewIncidentTools(diagnoser, nil)
	request := models.DiagnoseIncidentRequest{ProjectID: "prod", ServiceName: "checkout", Region: "us-central1", IncludePlatformHealth: true}
	result, err := module.diagnoseHandler(context.Background(), mcp.CallToolRequest{}, request)
	if err != nil {
		t.Fatalf("diagnoseHandler() error = %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("unexpected result: %+v", result)
	}
	if diagnoser.captured != request {
		t.Fatalf("captured request = %+v, want %+v", diagnoser.captured, request)
	}
}

func TestIncidentToolHandlerReturnsToolError(t *testing.T) {
	t.Parallel()
	module := NewIncidentTools(&fakeDiagnoser{err: errors.New("collector failed")}, nil)
	result, err := module.diagnoseHandler(context.Background(), mcp.CallToolRequest{}, models.DiagnoseIncidentRequest{ProjectID: "prod"})
	if err == nil || result != nil {
		t.Fatalf("expected wrapped Go error, result=%+v err=%v", result, err)
	}
}
