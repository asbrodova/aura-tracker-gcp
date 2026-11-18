package requestmeta

import (
	"context"
	"testing"
)

func TestPrincipalActorPrefersEmail(t *testing.T) {
	if got := (Principal{Subject: "sub-1", Email: "user@example.com"}).Actor(); got != "user@example.com" {
		t.Fatalf("Actor() = %q, want email", got)
	}
	if got := (Principal{Subject: "sub-1"}).Actor(); got != "sub-1" {
		t.Fatalf("Actor() without email = %q, want subject", got)
	}
}

func TestPrincipalIdentityKeyExcludesEmail(t *testing.T) {
	base := Principal{Issuer: "https://accounts.google.com", Subject: "sub-1", Email: "old@example.com"}
	renamed := base
	renamed.Email = "new@example.com"
	if base.IdentityKey() == "" || base.IdentityKey() != renamed.IdentityKey() {
		t.Fatalf("identity key changed with email: %q vs %q", base.IdentityKey(), renamed.IdentityKey())
	}
	otherIssuer := base
	otherIssuer.Issuer = "https://other.example"
	if base.IdentityKey() == otherIssuer.IdentityKey() {
		t.Fatal("identity key ignores the issuer")
	}
	for _, incomplete := range []Principal{{Subject: "sub-1"}, {Issuer: "https://accounts.google.com"}} {
		if key := incomplete.IdentityKey(); key != "" {
			t.Fatalf("IdentityKey(%+v) = %q, want empty", incomplete, key)
		}
	}
}

func TestContextValuesRoundTrip(t *testing.T) {
	ctx := context.Background()
	if _, ok := PrincipalFromContext(ctx); ok {
		t.Fatal("empty context reported a principal")
	}
	if CorrelationID(ctx) != "" || SessionID(ctx) != "" {
		t.Fatal("empty context reported correlation or session IDs")
	}

	principal := Principal{Issuer: "iss", Subject: "sub", Email: "e@example.com", Audience: "aud"}
	ctx = WithSessionID(WithCorrelationID(WithPrincipal(ctx, principal), "corr-1"), "sess-1")
	got, ok := PrincipalFromContext(ctx)
	if !ok || got != principal {
		t.Fatalf("PrincipalFromContext() = %+v, %v", got, ok)
	}
	if CorrelationID(ctx) != "corr-1" || SessionID(ctx) != "sess-1" {
		t.Fatalf("correlation = %q, session = %q", CorrelationID(ctx), SessionID(ctx))
	}
}
