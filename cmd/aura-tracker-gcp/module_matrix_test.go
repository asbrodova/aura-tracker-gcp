package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"google.golang.org/api/option"

	gcpadapter "github.com/asbrodova/aura-tracker-gcp/internal/gcp"
	mcpserver "github.com/asbrodova/aura-tracker-gcp/internal/mcp"
	"github.com/asbrodova/aura-tracker-gcp/internal/safety"
)

// matrixEndpoint sends every SDK call to a closed local port, so the matrix
// never reaches Google: gRPC clients fail with Unavailable, REST clients fail
// on the unsupported scheme. The "passthrough" scheme keeps it a valid gRPC
// target and a parseable URL for clients that parse their endpoint.
const matrixEndpoint = "passthrough:///127.0.0.1:1"

// Text that only appears when a panic was recovered (internal/mcp/recovery.go
// and internal/safego). A tool error of any other kind is fine here.
var recoveredPanicMarkers = []string{
	"the server recovered and logged the details",
	"a background task panicked",
}

// TestModuleMatrixEveryToolRunsWithOnlyItsModule starts the real adapter,
// safety decorator and MCP server with exactly one module enabled, the way
// --modules=<name> does, and calls every tool that module registers once.
// Each call must return (a result or an error) without panicking. This is the
// configuration that crashed the server with --modules=serverlessgraph.
func TestModuleMatrixEveryToolRunsWithOnlyItsModule(t *testing.T) {
	if testing.Short() {
		t.Skip("module matrix builds one adapter per module")
	}
	discard := slog.New(slog.DiscardHandler)
	for _, module := range mcpserver.AllModules {
		t.Run(module, func(t *testing.T) {
			t.Parallel()
			enabled := map[string]bool{module: true}
			adapter, err := gcpadapter.New(context.Background(), "matrix-project",
				gcpadapter.WithModules(enabled),
				gcpadapter.WithClientOptions(option.WithoutAuthentication(), option.WithEndpoint(matrixEndpoint)),
				gcpadapter.WithCallTimeout(300*time.Millisecond),
				gcpadapter.WithGraphTimeout(2*time.Second),
				gcpadapter.WithRateLimit(1000, 1000),
				gcpadapter.WithLogger(discard),
			)
			if err != nil {
				t.Fatalf("gcp adapter with --modules=%s: %v", module, err)
			}
			t.Cleanup(func() { _ = adapter.Close() })
			s := mcpserver.New(safety.NewSafetyDecorator(adapter, discard), discard, "matrix", mcpserver.WithModules(enabled), mcpserver.WithMutations())

			tools := matrixListTools(t, s)
			if len(tools) == 0 {
				t.Logf("--modules=%s registers no tools without extra configuration", module)
			}
			for i, tool := range tools {
				args, err := json.Marshal(matrixArguments(tool.InputSchema))
				if err != nil {
					t.Fatal(err)
				}
				request := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, i+1, tool.Name, args)
				response, ok := matrixCall(s, request, 30*time.Second)
				if !ok {
					t.Errorf("--modules=%s: %s did not return within 30s", module, tool.Name)
					continue
				}
				for _, marker := range recoveredPanicMarkers {
					if strings.Contains(response, marker) {
						t.Errorf("--modules=%s: %s panicked (stack in the test log): %s", module, tool.Name, response)
					}
				}
			}
		})
	}
}

type matrixTool struct {
	Name        string            `json:"name"`
	InputSchema matrixInputSchema `json:"inputSchema"`
}

type matrixInputSchema struct {
	Properties map[string]map[string]any `json:"properties"`
	Required   []string                  `json:"required"`
}

func matrixListTools(t *testing.T, s *server.MCPServer) []matrixTool {
	t.Helper()
	raw, err := json.Marshal(s.HandleMessage(context.Background(), json.RawMessage(`{"jsonrpc":"2.0","id":0,"method":"tools/list","params":{}}`)))
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Result struct {
			Tools []matrixTool `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatalf("tools/list: %v\n%s", err, raw)
	}
	return response.Result.Tools
}

func matrixCall(s *server.MCPServer, request string, limit time.Duration) (string, bool) {
	done := make(chan string, 1)
	go func() {
		raw, err := json.Marshal(s.HandleMessage(context.Background(), json.RawMessage(request)))
		if err != nil {
			done <- err.Error()
			return
		}
		done <- string(raw)
	}()
	select {
	case response := <-done:
		return response, true
	case <-time.After(limit):
		return "", false
	}
}

// matrixArguments fills the required parameters (plus project_id and
// dry_run) with plausible values so calls get past argument validation and
// into the adapter where possible.
func matrixArguments(schema matrixInputSchema) map[string]any {
	args := map[string]any{}
	names := append([]string{"project_id", "dry_run"}, schema.Required...)
	for _, name := range names {
		property, ok := schema.Properties[name]
		if !ok {
			continue
		}
		if values, ok := property["enum"].([]any); ok && len(values) > 0 {
			args[name] = values[0]
			continue
		}
		switch property["type"] {
		case "string":
			args[name] = matrixString(name)
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

func matrixString(name string) string {
	switch {
	case name == "project_id" || name == "project" || strings.HasPrefix(name, "environment_"):
		return "matrix-project"
	case name == "region" || name == "location":
		return "us-central1"
	case strings.Contains(name, "zone"):
		return "us-central1-a"
	default:
		return "matrix"
	}
}
