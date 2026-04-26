package anonymize

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func toolReturning(result *mcp.CallToolResult, err error) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("test_tool"),
		Handler: func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return result, err
		},
	}
}

func callTool(t *testing.T, tool server.ServerTool) (*mcp.CallToolResult, error) {
	t.Helper()
	return tool.Handler(context.Background(), mcp.CallToolRequest{})
}

func TestWrapHandlerNoop(t *testing.T) {
	r := textResult("hello 192.168.1.1")
	wrapped := WrapHandler(toolReturning(r, nil), NoopAnonymizer{})
	got, err := callTool(t, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	// NoopAnonymizer must not change content.
	if resultText(got) != "hello 192.168.1.1" {
		t.Errorf("noop changed content: %q", resultText(got))
	}
}

func TestWrapHandlerLocalScrubber(t *testing.T) {
	s, err := NewLocalScrubber(Config{})
	if err != nil {
		t.Fatal(err)
	}
	r := textResult(`{"endpoint":"10.0.0.1"}`)
	wrapped := WrapHandler(toolReturning(r, nil), s)
	got, callErr := callTool(t, wrapped)
	if callErr != nil {
		t.Fatal(callErr)
	}
	if strings.Contains(resultText(got), "10.0.0.1") {
		t.Error("IP not masked by WrapHandler")
	}
}

// Go-level errors must propagate unchanged; scrubber must NOT be called.
func TestWrapHandlerPropagatesGoError(t *testing.T) {
	sentinel := errors.New("rpc error")
	wrapped := WrapHandler(toolReturning(nil, sentinel), NoopAnonymizer{})
	_, err := callTool(t, wrapped)
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}

// A failing scrubber must return a generic error result, never the raw data.
func TestWrapHandlerScrubFailure(t *testing.T) {
	var failingAnon failAnonymizer
	r := textResult("secret data 10.0.0.1")
	wrapped := WrapHandler(toolReturning(r, nil), failingAnon)
	got, err := callTool(t, wrapped)
	if err != nil {
		t.Fatalf("expected tool result, not Go error: %v", err)
	}
	if !got.IsError {
		t.Error("expected IsError=true when scrub fails")
	}
	if strings.Contains(resultText(got), "secret data") {
		t.Error("raw content leaked after scrub failure")
	}
}

// failAnonymizer always returns an error from Scrub.
type failAnonymizer struct{}

func (failAnonymizer) Scrub(_ context.Context, _ *mcp.CallToolResult) (*mcp.CallToolResult, error) {
	return nil, errors.New("simulated scrub failure")
}
