package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/mark3labs/mcp-go/mcp"
)

// errHandlerPanic is returned to the client when a resource or prompt handler
// panics. Like the tool message below, it deliberately omits the panic value:
// panics raised deep in SDK response handling can carry project IDs and
// resource names, and a recovered panic bypasses the privacy filters.
var errHandlerPanic = errors.New("internal error: the request handler panicked; the server recovered and logged the details")

// The recover* wrappers sit outside the privacy and size filters, so a panic
// anywhere in a handler chain becomes an ordinary error for that one request
// instead of terminating the stdio or SSE server process. mcp-go's own
// server.WithRecovery is not used because it copies the panic value into the
// JSON-RPC error and covers tools only.

func recoverToolPanic(
	log *slog.Logger,
	name string,
	handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error),
) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (result *mcp.CallToolResult, err error) {
		defer func() {
			if r := recover(); r != nil {
				logHandlerPanic(ctx, log, "tool", name, r)
				result = mcp.NewToolResultError(fmt.Sprintf("internal error in %s: the server recovered and logged the details", name))
				err = nil
			}
		}()
		return handler(ctx, req)
	}
}

func recoverResourcePanic(
	log *slog.Logger,
	name string,
	handler func(context.Context, mcp.ReadResourceRequest) ([]mcp.ResourceContents, error),
) func(context.Context, mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	return func(ctx context.Context, req mcp.ReadResourceRequest) (contents []mcp.ResourceContents, err error) {
		defer func() {
			if r := recover(); r != nil {
				logHandlerPanic(ctx, log, "resource", name, r)
				contents, err = nil, errHandlerPanic
			}
		}()
		return handler(ctx, req)
	}
}

func recoverPromptPanic(
	log *slog.Logger,
	name string,
	handler func(context.Context, mcp.GetPromptRequest) (*mcp.GetPromptResult, error),
) func(context.Context, mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return func(ctx context.Context, req mcp.GetPromptRequest) (result *mcp.GetPromptResult, err error) {
		defer func() {
			if r := recover(); r != nil {
				logHandlerPanic(ctx, log, "prompt", name, r)
				result, err = nil, errHandlerPanic
			}
		}()
		return handler(ctx, req)
	}
}

func logHandlerPanic(ctx context.Context, log *slog.Logger, kind, name string, recovered any) {
	if log == nil {
		log = slog.Default()
	}
	log.ErrorContext(ctx, "recovered panic in MCP handler",
		"kind", kind,
		"name", name,
		"panic", fmt.Sprint(recovered),
		"stack", string(debug.Stack()),
	)
}
