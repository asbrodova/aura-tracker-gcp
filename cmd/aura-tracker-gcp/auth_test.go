package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcptransport "github.com/mark3labs/mcp-go/server"
	"google.golang.org/api/idtoken"

	"github.com/asbrodova/aura-tracker-gcp/internal/requestmeta"
)

type stubIdentityValidator struct {
	payload *idtoken.Payload
	err     error
}

type tokenIdentityValidator map[string]*idtoken.Payload

func testSessionBinding() *sseSessionBinding {
	binding := &sseSessionBinding{}
	copy(binding.key[:], []byte("fixed-test-key-material-32-bytes!!"))
	return binding
}

func (v tokenIdentityValidator) Validate(_ context.Context, token, _ string) (*idtoken.Payload, error) {
	payload, ok := v[token]
	if !ok {
		return nil, errors.New("unknown token")
	}
	return payload, nil
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
			authenticatedMCPHandler(next, stubIdentityValidator{}, cfg, slog.Default(), testSessionBinding()).ServeHTTP(recorder, req)
			if recorder.Code != http.StatusUnauthorized || called {
				t.Fatalf("status=%d called=%v", recorder.Code, called)
			}
		})
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://service.example/message", nil)
	req.Header.Set("Authorization", "Bearer invalid")
	authenticatedMCPHandler(next, stubIdentityValidator{err: errors.New("bad signature")}, cfg, slog.Default(), testSessionBinding()).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized || called {
		t.Fatalf("status=%d called=%v", recorder.Code, called)
	}
}

func TestAuthenticatedMCPHandlerInjectsPrincipalAndEnforcesAllowlist(t *testing.T) {
	payload := &idtoken.Payload{
		Issuer: googleIssuerHTTPS, Subject: "subject-123", Audience: "https://service.example",
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
	authenticatedMCPHandler(next, validator, cfg, slog.Default(), testSessionBinding()).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent || got.Email != "caller@example.com" || got.Subject != payload.Subject {
		t.Fatalf("status=%d principal=%+v", recorder.Code, got)
	}

	cfg.AllowedEmails = map[string]struct{}{"different@example.com": {}}
	recorder = httptest.NewRecorder()
	authenticatedMCPHandler(next, validator, cfg, slog.Default(), testSessionBinding()).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", recorder.Code)
	}

	payload.Claims["email_verified"] = false
	cfg.AllowedEmails = map[string]struct{}{"caller@example.com": {}}
	recorder = httptest.NewRecorder()
	authenticatedMCPHandler(next, validator, cfg, slog.Default(), testSessionBinding()).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("unverified email status=%d, want 403", recorder.Code)
	}
}

func TestAuthenticatedMCPHandlerRejectsUntrustedIssuer(t *testing.T) {
	payload := &idtoken.Payload{
		Issuer: "https://untrusted.example", Subject: "subject-123", Audience: "https://service.example",
		Claims: map[string]any{"email": "caller@example.com", "email_verified": true},
	}
	called := false
	handler := authenticatedMCPHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}), stubIdentityValidator{payload: payload}, sseAuthConfig{
		Mode: authModeRequired, Audience: payload.Audience, AllowAnyValidToken: true,
	}, slog.Default(), testSessionBinding())
	recorder := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, payload.Audience+"/sse", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized || called {
		t.Fatalf("untrusted issuer status=%d called=%v", recorder.Code, called)
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

func TestLoadSSEAuthConfigSeparatesOriginAndStaticBasePath(t *testing.T) {
	getenv := func(name string) string {
		if name == "MCP_AUTH_ALLOW_ANY_VALID_TOKEN" {
			return "true"
		}
		return ""
	}
	cfg, err := loadSSEAuthConfig("https://service.example/mcp/", getenv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Origin != "https://service.example" || cfg.BasePath != "/mcp" || cfg.Audience != "https://service.example/mcp" {
		t.Fatalf("unexpected path config: %+v", cfg)
	}
	for _, invalid := range []string{
		"https://service.example/mcp/../admin",
		"https://service.example/./mcp",
		"https://service.example/mcp//nested",
		"https://service.example/mcp%2Fnested",
		"https://service.example?",
		"https://service.example#",
		"https://user@service.example/mcp",
	} {
		if _, err := loadSSEAuthConfig(invalid, getenv); err == nil {
			t.Errorf("unsafe base path %q was accepted", invalid)
		}
	}
}

func TestSSEStaticBasePathMountsAdvertisedAndHandledEndpoints(t *testing.T) {
	core := mcptransport.NewMCPServer("test", "test")
	sse := mcptransport.NewSSEServer(core,
		mcptransport.WithBaseURL("https://service.example"),
		mcptransport.WithStaticBasePath("/mcp"),
	)
	if got := sse.CompleteSsePath(); got != "/mcp/sse" {
		t.Fatalf("SSE path = %q", got)
	}
	if got := sse.CompleteMessagePath(); got != "/mcp/message" {
		t.Fatalf("message path = %q", got)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "https://service.example/mcp/sse", nil)
	if got := sse.GetMessageEndpointForClient(req, "session-123"); got != "https://service.example/mcp/message?sessionId=session-123" {
		t.Fatalf("advertised endpoint = %q", got)
	}

	recorder := httptest.NewRecorder()
	message := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "https://service.example/mcp/message?sessionId=missing", strings.NewReader(`{}`))
	sse.ServeHTTP(recorder, message)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "Invalid session ID") {
		t.Fatalf("prefixed message route status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	message = httptest.NewRequestWithContext(context.Background(), http.MethodPost, "https://service.example/message?sessionId=missing", strings.NewReader(`{}`))
	sse.ServeHTTP(recorder, message)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unmounted root message route status=%d, want 404", recorder.Code)
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

func TestSSESessionBindingUsesImmutableSubject(t *testing.T) {
	binding := testSessionBinding()
	owner := requestmeta.WithPrincipal(context.Background(), requestmeta.Principal{Issuer: googleIssuerHTTPS, Subject: "subject-owner", Email: "owner@example.com", Audience: "https://service.example"})
	sessionID, err := binding.NewSessionID(owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !validSessionID(sessionID) || !binding.Authorize(owner, sessionID) {
		t.Fatalf("generated session was not authorized: %q", sessionID)
	}
	sameSubjectNewEmail := requestmeta.WithPrincipal(context.Background(), requestmeta.Principal{Issuer: googleIssuerHTTPS, Subject: "subject-owner", Email: "renamed@example.com", Audience: "https://service.example"})
	if !binding.Authorize(sameSubjectNewEmail, sessionID) {
		t.Fatal("same immutable subject was rejected after email change")
	}
	sameEmailOtherSubject := requestmeta.WithPrincipal(context.Background(), requestmeta.Principal{Issuer: googleIssuerHTTPS, Subject: "subject-attacker", Email: "owner@example.com", Audience: "https://service.example"})
	if binding.Authorize(sameEmailOtherSubject, sessionID) {
		t.Fatal("different subject was authorized based on matching email")
	}
	differentAudience := requestmeta.WithPrincipal(context.Background(), requestmeta.Principal{Issuer: googleIssuerHTTPS, Subject: "subject-owner", Audience: "https://different.example"})
	if binding.Authorize(differentAudience, sessionID) {
		t.Fatal("session ID was authorized for a different token audience")
	}
	differentIssuer := requestmeta.WithPrincipal(context.Background(), requestmeta.Principal{Issuer: "https://untrusted.example", Subject: "subject-owner", Audience: "https://service.example"})
	if binding.Authorize(differentIssuer, sessionID) {
		t.Fatal("session ID was authorized for a different token issuer")
	}
	replacement := "A"
	if strings.HasSuffix(sessionID, replacement) {
		replacement = "B"
	}
	tampered := sessionID[:len(sessionID)-1] + replacement
	if binding.Authorize(owner, tampered) {
		t.Fatal("tampered session ID was authorized")
	}
}

func TestCanonicalGoogleIdentityIssuerTreatsDocumentedAliasesAsOneIssuer(t *testing.T) {
	for _, issuer := range []string{googleIssuerHTTP, googleIssuerHTTPS} {
		canonical, ok := canonicalGoogleIdentityIssuer(issuer)
		if !ok || canonical != googleIssuerHTTPS {
			t.Fatalf("canonical issuer for %q = %q, %v", issuer, canonical, ok)
		}
	}
	if _, ok := canonicalGoogleIdentityIssuer("https://untrusted.example"); ok {
		t.Fatal("untrusted issuer was accepted")
	}
}

func TestAuthenticatedMCPHandlerRejectsCrossPrincipalSessionUse(t *testing.T) {
	binding := testSessionBinding()
	ownerPayload := &idtoken.Payload{Issuer: googleIssuerHTTPS, Subject: "subject-owner", Audience: "https://service.example", Claims: map[string]any{"email": "owner@example.com", "email_verified": true}}
	attackerPayload := &idtoken.Payload{Issuer: googleIssuerHTTPS, Subject: "subject-attacker", Audience: ownerPayload.Audience, Claims: map[string]any{"email": "attacker@example.com", "email_verified": true}}
	ownerContext := requestmeta.WithPrincipal(context.Background(), requestmeta.Principal{Issuer: ownerPayload.Issuer, Subject: ownerPayload.Subject, Email: "owner@example.com", Audience: ownerPayload.Audience})
	sessionID, err := binding.NewSessionID(ownerContext, nil)
	if err != nil {
		t.Fatal(err)
	}
	aliasIssuerPayload := &idtoken.Payload{Issuer: googleIssuerHTTP, Subject: ownerPayload.Subject, Audience: ownerPayload.Audience, Claims: map[string]any{"email": "renamed@example.com", "email_verified": true}}
	validator := tokenIdentityValidator{"owner-token": ownerPayload, "alias-issuer-token": aliasIssuerPayload, "attacker-token": attackerPayload}
	cfg := sseAuthConfig{Mode: authModeRequired, Audience: ownerPayload.Audience, AllowAnyValidToken: true}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if got := requestmeta.SessionID(r.Context()); got != sessionID {
			t.Errorf("context session ID = %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	handler := authenticatedMCPHandler(next, validator, cfg, slog.Default(), binding)

	request := func(token string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "https://service.example/message?sessionId="+sessionID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		handler.ServeHTTP(recorder, req)
		return recorder
	}
	if recorder := request("attacker-token"); recorder.Code != http.StatusForbidden || called {
		t.Fatalf("cross-principal request status=%d called=%v", recorder.Code, called)
	}
	if recorder := request("owner-token"); recorder.Code != http.StatusNoContent || !called {
		t.Fatalf("owner request status=%d called=%v", recorder.Code, called)
	}
	called = false
	if recorder := request("alias-issuer-token"); recorder.Code != http.StatusNoContent || !called {
		t.Fatalf("owner issuer alias request status=%d called=%v", recorder.Code, called)
	}
}

func TestAuthenticatedMCPHandlerRejectsDuplicateSessionID(t *testing.T) {
	binding := testSessionBinding()
	payload := &idtoken.Payload{Issuer: googleIssuerHTTPS, Subject: "subject-owner", Audience: "https://service.example", Claims: map[string]any{}}
	ctx := requestmeta.WithPrincipal(context.Background(), requestmeta.Principal{Issuer: payload.Issuer, Subject: payload.Subject, Audience: payload.Audience})
	sessionID, err := binding.NewSessionID(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	handler := authenticatedMCPHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}), stubIdentityValidator{payload: payload}, sseAuthConfig{
		Mode: authModeRequired, Audience: payload.Audience, AllowAnyValidToken: true,
	}, slog.Default(), binding)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "https://service.example/message?sessionId="+sessionID+"&sessionId="+sessionID, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest || called {
		t.Fatalf("duplicate session status=%d called=%v", recorder.Code, called)
	}
}
