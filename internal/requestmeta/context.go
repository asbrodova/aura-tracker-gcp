// Package requestmeta carries authenticated identity and correlation metadata
// across transport, MCP, safety, and adapter boundaries.
package requestmeta

import "context"

type contextKey int

const (
	principalKey contextKey = iota
	correlationIDKey
	sessionIDKey
)

// Principal is the authenticated caller of an MCP request.
type Principal struct {
	Subject  string
	Email    string
	Audience string
}

// Actor returns the most useful stable identifier for audit logging.
func (p Principal) Actor() string {
	if p.Email != "" {
		return p.Email
	}
	return p.Subject
}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey).(Principal)
	return principal, ok
}

func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey, id)
}

func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationIDKey).(string)
	return id
}

func WithSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sessionIDKey, id)
}

func SessionID(ctx context.Context) string {
	id, _ := ctx.Value(sessionIDKey).(string)
	return id
}
