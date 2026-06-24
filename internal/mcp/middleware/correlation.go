// Package middleware provides cross-cutting concerns for MCP tool handlers:
// request correlation IDs and structured logging helpers.
package middleware

import (
	"context"
	"fmt"
	"math/rand/v2"
)

type contextKeyCorrelationID struct{}

// WithCorrelationID returns a derived context carrying a new short correlation ID.
// Call once per MCP tool invocation to bind all log lines for that call.
func WithCorrelationID(ctx context.Context) context.Context {
	id := fmt.Sprintf("%08x", rand.Uint32())
	return context.WithValue(ctx, contextKeyCorrelationID{}, id)
}

// CorrelationID extracts the correlation ID injected by WithCorrelationID.
// Returns "unknown" when no ID is present (e.g., in unit tests that don't inject one).
func CorrelationID(ctx context.Context) string {
	if id, ok := ctx.Value(contextKeyCorrelationID{}).(string); ok {
		return id
	}
	return "unknown"
}
