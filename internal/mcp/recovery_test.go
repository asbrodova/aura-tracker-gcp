package mcp

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	protocol "github.com/mark3labs/mcp-go/mcp"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

const panicDetail = "nil pointer in projects/private-dev-123/locations/x"

// panicSvc panics inside one tool path and one resource path, the way a nil
// SDK client or an unexpected API response shape would.
type panicSvc struct {
	*mockSvc
}

func (p *panicSvc) ListClusters(context.Context, models.ListClustersRequest) (models.ListClustersResponse, error) {
	panic(panicDetail)
}

func (p *panicSvc) ListServices(context.Context, models.ListServicesRequest) (models.ListServicesResponse, error) {
	panic(panicDetail)
}

func TestToolPanicBecomesToolErrorWithoutLeakingDetails(t *testing.T) {
	s := New(&panicSvc{mockSvc: &mockSvc{}}, slog.Default(), "test",
		WithModules(map[string]bool{ModuleGKE: true}),
		WithEnvironments(testEnvironmentRegistry(t)),
	)

	response := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"gcp_gke_list_clusters","arguments":{"project_id":"dev","location":"-"}}}`)
	if !strings.Contains(response, `"isError":true`) {
		t.Fatalf("panic was not returned as a tool error: %s", response)
	}
	if !strings.Contains(response, "internal error in gcp_gke_list_clusters") {
		t.Fatalf("tool error does not name the tool: %s", response)
	}
	for _, leaked := range []string{"private-dev-123", "nil pointer"} {
		if strings.Contains(response, leaked) {
			t.Fatalf("tool error leaks panic detail %q: %s", leaked, response)
		}
	}

	// The server must keep serving after the panic.
	if list := handleJSON(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`); !strings.Contains(list, "gcp_gke_list_clusters") {
		t.Fatalf("tools/list after recovered panic = %s", list)
	}
}

func TestResourcePanicBecomesErrorWithoutLeakingDetails(t *testing.T) {
	s := New(&panicSvc{mockSvc: &mockSvc{}}, slog.Default(), "test",
		WithEnvironments(testEnvironmentRegistry(t)),
	)

	response := handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"gcp://dev/cloudrun/services"}}`)
	if !strings.Contains(response, `"error"`) || !strings.Contains(response, "handler panicked") {
		t.Fatalf("resource panic was not returned as an error: %s", response)
	}
	for _, leaked := range []string{"private-dev-123", "nil pointer"} {
		if strings.Contains(response, leaked) {
			t.Fatalf("resource error leaks panic detail %q: %s", leaked, response)
		}
	}
}

func TestPromptPanicBecomesError(t *testing.T) {
	handler := recoverPromptPanic(slog.Default(), "test-prompt", func(context.Context, protocol.GetPromptRequest) (*protocol.GetPromptResult, error) {
		panic(panicDetail)
	})
	result, err := handler(context.Background(), protocol.GetPromptRequest{})
	if result != nil || !errors.Is(err, errHandlerPanic) {
		t.Fatalf("handler() = (%v, %v), want (nil, errHandlerPanic)", result, err)
	}
}

func TestRecoveryWrappersPassThroughNormalResults(t *testing.T) {
	want := errors.New("ordinary failure")
	resource := recoverResourcePanic(nil, "test-resource", func(context.Context, protocol.ReadResourceRequest) ([]protocol.ResourceContents, error) {
		return nil, want
	})
	if _, err := resource(context.Background(), protocol.ReadResourceRequest{}); !errors.Is(err, want) {
		t.Fatalf("resource error = %v, want %v", err, want)
	}

	tool := recoverToolPanic(nil, "test-tool", func(context.Context, protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		return protocol.NewToolResultText("ok"), nil
	})
	result, err := tool(context.Background(), protocol.CallToolRequest{})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("tool() = (%+v, %v), want a successful result", result, err)
	}
}
