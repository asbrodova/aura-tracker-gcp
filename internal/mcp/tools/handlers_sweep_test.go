package tools

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/internal/testutil"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

// serviceBackedTools builds every tool module that talks to ports.GCPService
// directly. Engine-backed modules (cost, drift, incidents, security, archgraph
// diagrams) take their own interfaces and have dedicated tests.
func serviceBackedTools(svc ports.GCPService) []server.ServerTool {
	log := slog.New(slog.DiscardHandler)
	iam := NewIAMTools(svc, log)
	iam.SetDefaultPermissions([]string{"run.services.list"})
	modules := []interface{ GetTools() []server.ServerTool }{
		NewGKETools(svc, log), NewGKEWorkloadTools(svc, log), NewGKEMeshTools(svc, log),
		NewNetworkingTools(svc, log), NewCloudRunTools(svc, log), NewFunctionsTools(svc, log),
		NewEventarcTools(svc, log), NewSchedulerTools(svc, log), NewWorkflowsTools(svc, log),
		NewTasksTools(svc, log), NewSecretManagerTools(svc, log), NewVPCAccessTools(svc, log),
		NewCloudSQLTools(svc, log), NewPubSubTools(svc, log), NewLoggingTools(svc, log),
		NewMonitoringTools(svc, log), iam, NewTopologyTools(svc, log), NewAuraTools(svc, log),
		NewStorageTools(svc, log), NewServerlessGraphTools(svc, log), NewDatastoreTools(svc, log),
		NewSupplyChainTools(svc, log), NewTaggingTools(svc, log), NewCoverageTools(svc, log),
		NewRecommenderExportTools(svc, log, "recommendations"),
	}
	var tools []server.ServerTool
	for _, module := range modules {
		tools = append(tools, module.GetTools()...)
	}
	return tools
}

// failingFake returns a FakeGCPService whose every method fails with err.
func failingFake(err error) *testutil.FakeGCPService {
	fake := &testutil.FakeGCPService{}
	fields := reflect.ValueOf(fake).Elem()
	for i := range fields.NumField() {
		field := fields.Field(i)
		if field.Kind() != reflect.Func {
			continue
		}
		fn := field.Type()
		field.Set(reflect.MakeFunc(fn, func([]reflect.Value) []reflect.Value {
			out := make([]reflect.Value, fn.NumOut())
			for j := range out {
				out[j] = reflect.Zero(fn.Out(j))
			}
			out[len(out)-1] = reflect.ValueOf(&err).Elem()
			return out
		}))
	}
	return fake
}

// sweepArguments fills project_id, dry_run and every required parameter with
// plausible values so handlers get past argument binding.
func sweepArguments(tool mcp.Tool) map[string]any {
	args := map[string]any{}
	names := append([]string{"project_id", "dry_run"}, tool.InputSchema.Required...)
	for _, name := range names {
		raw, ok := tool.InputSchema.Properties[name]
		if !ok {
			continue
		}
		property, _ := raw.(map[string]any)
		if values, ok := property["enum"].([]string); ok && len(values) > 0 {
			args[name] = values[0]
			continue
		}
		if values, ok := property["enum"].([]any); ok && len(values) > 0 {
			args[name] = values[0]
			continue
		}
		switch property["type"] {
		case "string":
			switch name {
			case "project_id", "project":
				args[name] = "test-project"
			case "region", "location":
				args[name] = "us-central1"
			default:
				args[name] = "sweep"
			}
		case "number", "integer":
			value := 1.0
			if minimum, ok := property["minimum"].(float64); ok && minimum > value {
				value = minimum
			}
			args[name] = value
		case "boolean":
			args[name] = name == "dry_run"
		case "array":
			args[name] = []any{}
		case "object":
			args[name] = map[string]any{}
		}
	}
	return args
}

func callSweep(t *testing.T, tool server.ServerTool) *mcp.CallToolResult {
	t.Helper()
	request := mcp.CallToolRequest{Params: mcp.CallToolParams{Name: tool.Tool.Name, Arguments: sweepArguments(tool.Tool)}}
	result, err := tool.Handler(context.Background(), request)
	if err != nil {
		t.Fatalf("%s returned a Go error (becomes a JSON-RPC internal error): %v", tool.Tool.Name, err)
	}
	if result == nil {
		t.Fatalf("%s returned a nil result", tool.Tool.Name)
	}
	return result
}

// TestServiceBackedToolHandlersHandleEmptyResponses runs every handler against
// a service that returns zero values: handlers must bind their arguments,
// tolerate empty responses and always return a result.
func TestServiceBackedToolHandlersHandleEmptyResponses(t *testing.T) {
	tools := serviceBackedTools(&testutil.FakeGCPService{})
	if len(tools) < 50 {
		t.Fatalf("only %d service-backed tools found; did a module stop registering tools?", len(tools))
	}
	for _, tool := range tools {
		t.Run(tool.Tool.Name, func(t *testing.T) {
			callSweep(t, tool)
		})
	}
}

// TestServiceBackedToolHandlersReportPermissionDenied checks that a
// PermissionDenied from the service reaches the client as a tool result
// (a structured error, or partial coverage for aggregating tools), never as
// a JSON-RPC internal error.
func TestServiceBackedToolHandlersReportPermissionDenied(t *testing.T) {
	denied := &ports.PermissionDeniedError{Op: "fake.Call", Err: errors.New("denied"), MissingPermissions: []string{"sweep.resources.list"}}
	for _, tool := range serviceBackedTools(failingFake(denied)) {
		t.Run(tool.Tool.Name, func(t *testing.T) {
			callSweep(t, tool)
		})
	}
}
