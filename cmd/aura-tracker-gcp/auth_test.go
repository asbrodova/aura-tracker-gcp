package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/api/idtoken"

	"github.com/asbrodova/aura-tracker-gcp/internal/requestmeta"
)

type stubIdentityValidator struct {
	payload *idtoken.Payload
	err     error
}

func (s stubIdentityValidator) Validate(context.Context, string, string) (*idtoken.Payload, error) {
	return s.payload, s.err
}

func TestAuthenticatedMCPHandlerRejectsMissingAndInvalidTokens(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	cfg := sseAuthConfig{Mode: authModeRequired, Audience: "https://service.example"}

	for name, header := range map[string]string{"missing": "", "malformed": "Basic abc"} {
		t.Run(name, func(t *testing.T) {
			called = false
			recorder := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://service.example/message", nil)
			req.Header.Set("Authorization", header)
			authenticatedMCPHandler(next, stubIdentityValidator{}, cfg, slog.Default()).ServeHTTP(recorder, req)
			if recorder.Code != http.StatusUnauthorized || called {
				t.Fatalf("status=%d called=%v", recorder.Code, called)
			}
		})
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://service.example/message", nil)
	req.Header.Set("Authorization", "Bearer invalid")
	authenticatedMCPHandler(next, stubIdentityValidator{err: errors.New("bad signature")}, cfg, slog.Default()).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized || called {
		t.Fatalf("status=%d called=%v", recorder.Code, called)
	}
}

func TestAuthenticatedMCPHandlerInjectsPrincipalAndEnforcesAllowlist(t *testing.T) {
	payload := &idtoken.Payload{
		Subject: "subject-123", Audience: "https://service.example",
		Claims: map[string]any{"email": "Caller@Example.com", "email_verified": true},
	}
	validator := stubIdentityValidator{payload: payload}
	cfg := sseAuthConfig{
		Mode: authModeRequired, Audience: payload.Audience,
		AllowedEmails: map[string]struct{}{"caller@example.com": {}},
	}
	var got requestmeta.Principal
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = requestmeta.PrincipalFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, payload.Audience+"/message", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	authenticatedMCPHandler(next, validator, cfg, slog.Default()).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent || got.Email != "caller@example.com" || got.Subject != payload.Subject {
		t.Fatalf("status=%d principal=%+v", recorder.Code, got)
	}

	cfg.AllowedEmails = map[string]struct{}{"different@example.com": {}}
	recorder = httptest.NewRecorder()
	authenticatedMCPHandler(next, validator, cfg, slog.Default()).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", recorder.Code)
	}

	payload.Claims["email_verified"] = false
	cfg.AllowedEmails = map[string]struct{}{"caller@example.com": {}}
	recorder = httptest.NewRecorder()
	authenticatedMCPHandler(next, validator, cfg, slog.Default()).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("unverified email status=%d, want 403", recorder.Code)
	}
}

func TestLoadSSEAuthConfigRestrictsDisabledModeToLoopback(t *testing.T) {
	getenv := func(name string) string {
		if name == "MCP_AUTH_MODE" {
			return "disabled"
		}
		return ""
	}
	if _, err := loadSSEAuthConfig("https://service.example", getenv); err == nil {
		t.Fatal("expected public disabled mode to fail")
	}
	cfg, err := loadSSEAuthConfig("http://localhost:8080", getenv)
	if err != nil || cfg.Mode != authModeDisabled {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func TestLoadSSEAuthConfigRequiresHTTPSOffLoopback(t *testing.T) {
	if _, err := loadSSEAuthConfig("http://service.example", func(string) string { return "" }); err == nil {
		t.Fatal("public HTTP base URL was accepted")
	}
	if _, err := loadSSEAuthConfig("https://service.example?token=bad", func(string) string { return "" }); err == nil {
		t.Fatal("base URL query was accepted")
	}
	if _, err := loadSSEAuthConfig("http://127.0.0.1:8080", func(string) string { return "" }); err != nil {
		t.Fatalf("loopback HTTP base URL rejected: %v", err)
	}
}

func TestLoadSSEAuthConfigRequiresExplicitPublicAuthorizationPolicy(t *testing.T) {
	if _, err := loadSSEAuthConfig("https://service.example", func(string) string { return "" }); err == nil ||
		!strings.Contains(err.Error(), "MCP_AUTH_ALLOWED_EMAILS") {
		t.Fatalf("public SSE without an authorization policy error = %v", err)
	}

	values := map[string]string{"MCP_AUTH_ALLOWED_EMAILS": "caller@example.com"}
	cfg, err := loadSSEAuthConfig("https://service.example", func(name string) string { return values[name] })
	if err != nil || len(cfg.AllowedEmails) != 1 || cfg.AllowAnyValidToken {
		t.Fatalf("allowlisted config = %+v, %v", cfg, err)
	}

	values = map[string]string{"MCP_AUTH_ALLOW_ANY_VALID_TOKEN": "true"}
	cfg, err = loadSSEAuthConfig("https://service.example", func(name string) string { return values[name] })
	if err != nil || !cfg.AllowAnyValidToken {
		t.Fatalf("explicit allow-any config = %+v, %v", cfg, err)
	}

	values["MCP_AUTH_ALLOW_ANY_VALID_TOKEN"] = "yes"
	if _, err := loadSSEAuthConfig("https://service.example", func(name string) string { return values[name] }); err == nil {
		t.Fatal("ambiguous MCP_AUTH_ALLOW_ANY_VALID_TOKEN value was accepted")
	}
}

func TestValidSessionID(t *testing.T) {
	if !validSessionID("session-1234") {
		t.Fatal("valid session ID rejected")
	}
	if validSessionID("contains space") || validSessionID(strings.Repeat("a", 257)) {
		t.Fatal("invalid session ID accepted")
	}
}
