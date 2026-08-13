package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"

	"google.golang.org/api/idtoken"

	"github.com/asbrodova/aura-tracker-gcp/internal/requestmeta"
)

const (
	authModeRequired  = "required"
	authModeDisabled  = "disabled"
	googleIssuerHTTP  = "accounts.google.com"
	googleIssuerHTTPS = "https://accounts.google.com"
)

type identityTokenValidator interface {
	Validate(context.Context, string, string) (*idtoken.Payload, error)
}

type googleIdentityTokenValidator struct{}

func (googleIdentityTokenValidator) Validate(ctx context.Context, token, audience string) (*idtoken.Payload, error) {
	return idtoken.Validate(ctx, token, audience)
}

type sseAuthConfig struct {
	Mode               string
	Audience           string
	Origin             string
	BasePath           string
	AllowedEmails      map[string]struct{}
	AllowAnyValidToken bool
}

func loadSSEAuthConfig(baseURL string, getenv func(string) string) (sseAuthConfig, error) {
	baseURL = strings.TrimSpace(baseURL)
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
	if parsedBaseURL.User != nil || parsedBaseURL.ForceQuery || parsedBaseURL.RawQuery != "" || strings.Contains(baseURL, "#") {
		return sseAuthConfig{}, fmt.Errorf("MCP_BASE_URL must not contain user info, a query, or a fragment")
	}
	if parsedBaseURL.RawPath != "" || strings.Contains(baseURL, "%") || hasUnsafeURLPathCharacter(parsedBaseURL.Path) {
		return sseAuthConfig{}, fmt.Errorf("MCP_BASE_URL must use an unescaped URL path without backslashes or control characters")
	}
	basePath := parsedBaseURL.Path
	if basePath == "" {
		basePath = "/"
	}
	cleanPath := path.Clean(basePath)
	if strings.TrimSuffix(basePath, "/") != cleanPath && basePath != cleanPath {
		return sseAuthConfig{}, fmt.Errorf("MCP_BASE_URL path must not contain duplicate, dot, or parent segments")
	}
	basePath = cleanPath
	if mode == authModeDisabled && !isLoopbackHostname(parsedBaseURL.Hostname()) {
		return sseAuthConfig{}, fmt.Errorf("MCP_AUTH_MODE=disabled is only allowed with a loopback MCP_BASE_URL")
	}

	audience := strings.TrimSpace(getenv("MCP_AUTH_AUDIENCE"))
	if audience == "" {
		audience = parsedBaseURL.Scheme + "://" + parsedBaseURL.Host
		if basePath != "/" {
			audience += basePath
		}
	}

	allowed := make(map[string]struct{})
	for _, email := range strings.Split(getenv("MCP_AUTH_ALLOWED_EMAILS"), ",") {
		if email = strings.ToLower(strings.TrimSpace(email)); email != "" {
			allowed[email] = struct{}{}
		}
	}
	allowAny := false
	if raw := strings.ToLower(strings.TrimSpace(getenv("MCP_AUTH_ALLOW_ANY_VALID_TOKEN"))); raw != "" {
		switch raw {
		case "true":
			allowAny = true
		case "false":
		default:
			return sseAuthConfig{}, fmt.Errorf("MCP_AUTH_ALLOW_ANY_VALID_TOKEN must be 'true' or 'false'")
		}
	}
	if mode == authModeRequired && !isLoopbackHostname(parsedBaseURL.Hostname()) && len(allowed) == 0 && !allowAny {
		return sseAuthConfig{}, fmt.Errorf("public SSE requires MCP_AUTH_ALLOWED_EMAILS or explicit MCP_AUTH_ALLOW_ANY_VALID_TOKEN=true")
	}
	return sseAuthConfig{
		Mode: mode, Audience: audience,
		Origin: parsedBaseURL.Scheme + "://" + parsedBaseURL.Host, BasePath: basePath,
		AllowedEmails: allowed, AllowAnyValidToken: allowAny,
	}, nil
}

func hasUnsafeURLPathCharacter(value string) bool {
	if strings.Contains(value, "\\") {
		return true
	}
	for _, char := range value {
		if char < ' ' || char == '\x7f' {
			return true
		}
	}
	return false
}

func isLoopbackHostname(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

type sseSessionBinding struct {
	key [sha256.Size]byte
}

func newSSESessionBinding() (*sseSessionBinding, error) {
	binding := &sseSessionBinding{}
	if _, err := rand.Read(binding.key[:]); err != nil {
		return nil, fmt.Errorf("generate SSE session binding key: %w", err)
	}
	return binding, nil
}

func (b *sseSessionBinding) NewSessionID(ctx context.Context, _ *http.Request) (string, error) {
	if b == nil {
		return "", fmt.Errorf("SSE session binding is not configured")
	}
	identity, err := sessionBindingIdentity(ctx)
	if err != nil {
		return "", err
	}
	var nonce [18]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("generate SSE session nonce: %w", err)
	}
	encodedNonce := base64.RawURLEncoding.EncodeToString(nonce[:])
	return encodedNonce + "." + b.signature(identity, encodedNonce), nil
}

func (b *sseSessionBinding) Authorize(ctx context.Context, sessionID string) bool {
	if b == nil {
		return false
	}
	parts := strings.Split(sessionID, ".")
	if len(parts) != 2 {
		return false
	}
	nonce, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(nonce) != 18 {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(signature) != sha256.Size {
		return false
	}
	identity, err := sessionBindingIdentity(ctx)
	if err != nil {
		return false
	}
	expected, err := base64.RawURLEncoding.DecodeString(b.signature(identity, parts[0]))
	return err == nil && hmac.Equal(signature, expected)
}

func (b *sseSessionBinding) signature(identity, nonce string) string {
	mac := hmac.New(sha256.New, b.key[:])
	_, _ = mac.Write([]byte("aura-tracker-gcp/sse-session/v1\x00"))
	_, _ = mac.Write([]byte(identity))
	_, _ = mac.Write([]byte{'\x00'})
	_, _ = mac.Write([]byte(nonce))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func sessionBindingIdentity(ctx context.Context) (string, error) {
	principal, ok := requestmeta.PrincipalFromContext(ctx)
	if !ok {
		return "anonymous-loopback\x00", nil
	}
	identity := principal.IdentityKey()
	if identity == "" {
		return "", fmt.Errorf("authenticated SSE principal has no subject")
	}
	return identity + "\x00" + principal.Audience, nil
}

func authenticatedMCPHandler(next http.Handler, validator identityTokenValidator, cfg sseAuthConfig, log *slog.Logger, binding *sseSessionBinding) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if binding == nil {
			http.Error(w, "SSE session security is unavailable", http.StatusInternalServerError)
			return
		}
		if cfg.Mode == authModeDisabled {
			requestContext := r.Context()
			if !authorizeSSESessionRequest(w, r, requestContext, binding, log) {
				return
			}
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
		if err != nil || payload == nil {
			log.Warn("mcp authentication rejected", "err", err)
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			http.Error(w, "invalid bearer token", http.StatusUnauthorized)
			return
		}
		issuer, ok := canonicalGoogleIdentityIssuer(payload.Issuer)
		if !ok {
			log.Warn("mcp authentication rejected", "reason", "untrusted token issuer")
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
		if payload.Subject == "" || payload.Subject != strings.TrimSpace(payload.Subject) || strings.ContainsRune(payload.Subject, '\x00') {
			http.Error(w, "token subject is required", http.StatusUnauthorized)
			return
		}
		if payload.Audience != cfg.Audience {
			http.Error(w, "invalid bearer token", http.StatusUnauthorized)
			return
		}
		principal := requestmeta.Principal{Subject: payload.Subject, Email: email, Audience: payload.Audience, Issuer: issuer}
		log.Info("authenticated mcp request", "actor", principal.Actor(), "path", r.URL.Path)
		requestContext := requestmeta.WithPrincipal(r.Context(), principal)
		if !authorizeSSESessionRequest(w, r, requestContext, binding, log) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func canonicalGoogleIdentityIssuer(issuer string) (string, bool) {
	switch issuer {
	case googleIssuerHTTP, googleIssuerHTTPS:
		return googleIssuerHTTPS, true
	default:
		return "", false
	}
}

func authorizeSSESessionRequest(w http.ResponseWriter, r *http.Request, ctx context.Context, binding *sseSessionBinding, log *slog.Logger) bool {
	sessionValues, present := r.URL.Query()["sessionId"]
	if len(sessionValues) > 1 {
		http.Error(w, "invalid session ID", http.StatusBadRequest)
		return false
	}
	sessionID := ""
	if present && len(sessionValues) == 1 {
		sessionID = sessionValues[0]
	}
	if sessionID == "" {
		*r = *r.WithContext(ctx)
		return true
	}
	if !validSessionID(sessionID) {
		http.Error(w, "invalid session ID", http.StatusBadRequest)
		return false
	}
	if !binding.Authorize(ctx, sessionID) {
		principal, _ := requestmeta.PrincipalFromContext(ctx)
		log.Warn("mcp session principal mismatch", "subject", principal.Subject, "path", r.URL.Path)
		http.Error(w, "session is not authorized for this caller", http.StatusForbidden)
		return false
	}
	requestContext := requestmeta.WithSessionID(ctx, sessionID)
	*r = *r.WithContext(requestContext)
	return true
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
