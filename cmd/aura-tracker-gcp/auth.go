package main

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"google.golang.org/api/idtoken"

	"github.com/asbrodova/aura-tracker-gcp/internal/requestmeta"
)

const (
	authModeRequired = "required"
	authModeDisabled = "disabled"
)

type identityTokenValidator interface {
	Validate(context.Context, string, string) (*idtoken.Payload, error)
}

type googleIdentityTokenValidator struct{}

func (googleIdentityTokenValidator) Validate(ctx context.Context, token, audience string) (*idtoken.Payload, error) {
	return idtoken.Validate(ctx, token, audience)
}

type sseAuthConfig struct {
	Mode          string
	Audience      string
	AllowedEmails map[string]struct{}
}

func loadSSEAuthConfig(baseURL string, getenv func(string) string) (sseAuthConfig, error) {
	mode := strings.ToLower(strings.TrimSpace(getenv("MCP_AUTH_MODE")))
	if mode == "" {
		mode = authModeRequired
	}
	if mode != authModeRequired && mode != authModeDisabled {
		return sseAuthConfig{}, fmt.Errorf("MCP_AUTH_MODE must be %q or %q", authModeRequired, authModeDisabled)
	}

	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil || parsedBaseURL.Scheme == "" || parsedBaseURL.Host == "" {
		return sseAuthConfig{}, fmt.Errorf("MCP_BASE_URL must be an absolute http(s) URL")
	}
	if parsedBaseURL.Scheme != "http" && parsedBaseURL.Scheme != "https" {
		return sseAuthConfig{}, fmt.Errorf("MCP_BASE_URL must use http or https")
	}
	if parsedBaseURL.Scheme == "http" && !isLoopbackHostname(parsedBaseURL.Hostname()) {
		return sseAuthConfig{}, fmt.Errorf("a non-loopback MCP_BASE_URL must use https")
	}
	if parsedBaseURL.User != nil || parsedBaseURL.RawQuery != "" || parsedBaseURL.Fragment != "" {
		return sseAuthConfig{}, fmt.Errorf("MCP_BASE_URL must not contain user info, a query, or a fragment")
	}
	if mode == authModeDisabled && !isLoopbackHostname(parsedBaseURL.Hostname()) {
		return sseAuthConfig{}, fmt.Errorf("MCP_AUTH_MODE=disabled is only allowed with a loopback MCP_BASE_URL")
	}

	audience := strings.TrimSpace(getenv("MCP_AUTH_AUDIENCE"))
	if audience == "" {
		audience = strings.TrimSuffix(baseURL, "/")
	}

	allowed := make(map[string]struct{})
	for _, email := range strings.Split(getenv("MCP_AUTH_ALLOWED_EMAILS"), ",") {
		if email = strings.ToLower(strings.TrimSpace(email)); email != "" {
			allowed[email] = struct{}{}
		}
	}
	return sseAuthConfig{Mode: mode, Audience: audience, AllowedEmails: allowed}, nil
}

func isLoopbackHostname(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func authenticatedMCPHandler(next http.Handler, validator identityTokenValidator, cfg sseAuthConfig, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cfg.Mode == authModeDisabled {
			next.ServeHTTP(w, r)
			return
		}

		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="aura-tracker-gcp"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		payload, err := validator.Validate(r.Context(), token, cfg.Audience)
		if err != nil {
			log.Warn("mcp authentication rejected", "err", err)
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			http.Error(w, "invalid bearer token", http.StatusUnauthorized)
			return
		}
		email, _ := payload.Claims["email"].(string)
		email = strings.ToLower(strings.TrimSpace(email))
		if len(cfg.AllowedEmails) > 0 {
			verified, _ := payload.Claims["email_verified"].(bool)
			if email == "" || !verified {
				log.Warn("mcp caller has no verified email", "subject", payload.Subject)
				http.Error(w, "caller does not have a verified email", http.StatusForbidden)
				return
			}
			if _, allowed := cfg.AllowedEmails[email]; !allowed {
				log.Warn("mcp caller is not allowlisted", "subject", payload.Subject, "email", email)
				http.Error(w, "caller is not authorized", http.StatusForbidden)
				return
			}
		}
		if strings.TrimSpace(payload.Subject) == "" {
			http.Error(w, "token subject is required", http.StatusUnauthorized)
			return
		}
		principal := requestmeta.Principal{Subject: payload.Subject, Email: email, Audience: payload.Audience}
		log.Info("authenticated mcp request", "actor", principal.Actor(), "path", r.URL.Path)
		requestContext := requestmeta.WithPrincipal(r.Context(), principal)
		if sessionID := r.URL.Query().Get("sessionId"); sessionID != "" {
			if !validSessionID(sessionID) {
				http.Error(w, "invalid session ID", http.StatusBadRequest)
				return
			}
			requestContext = requestmeta.WithSessionID(requestContext, sessionID)
		}
		next.ServeHTTP(w, r.WithContext(requestContext))
	})
}

func validSessionID(sessionID string) bool {
	if len(sessionID) == 0 || len(sessionID) > 256 {
		return false
	}
	for _, char := range sessionID {
		if char <= ' ' || char > '~' {
			return false
		}
	}
	return true
}

func bearerToken(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || subtle.ConstantTimeCompare([]byte(strings.ToLower(parts[0])), []byte("bearer")) != 1 || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}
