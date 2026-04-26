package gcp

import (
	"testing"

	"cloud.google.com/go/dlp/apiv2/dlppb"
)

func TestMapFinding_Normal(t *testing.T) {
	f := &dlppb.Finding{
		InfoType: &dlppb.InfoType{Name: "EMAIL_ADDRESS"},
		Quote:    "user@example.com",
		Location: &dlppb.Location{
			ByteRange: &dlppb.Range{Start: 10, End: 26},
		},
	}
	got := mapFinding(f)
	if got.InfoType != "EMAIL_ADDRESS" {
		t.Errorf("InfoType = %q, want EMAIL_ADDRESS", got.InfoType)
	}
	if got.Offset != 10 {
		t.Errorf("Offset = %d, want 10", got.Offset)
	}
	if got.Length != 16 {
		t.Errorf("Length = %d, want 16", got.Length)
	}
	if got.Quote != "user@example.com" {
		t.Errorf("Quote = %q, want user@example.com", got.Quote)
	}
}

func TestMapFinding_NilByteRange(t *testing.T) {
	f := &dlppb.Finding{
		InfoType: &dlppb.InfoType{Name: "IP_ADDRESS"},
		Location: &dlppb.Location{},
	}
	got := mapFinding(f)
	if got.Offset != 0 || got.Length != 0 {
		t.Errorf("expected zero offset/length for nil ByteRange, got offset=%d length=%d", got.Offset, got.Length)
	}
}

func TestMapFinding_EmptyQuote(t *testing.T) {
	f := &dlppb.Finding{
		InfoType: &dlppb.InfoType{Name: "PHONE_NUMBER"},
		Quote:    "",
		Location: &dlppb.Location{
			ByteRange: &dlppb.Range{Start: 0, End: 12},
		},
	}
	got := mapFinding(f)
	if got.Quote != "" {
		t.Errorf("Quote = %q, want empty", got.Quote)
	}
	if got.Length != 12 {
		t.Errorf("Length = %d, want 12", got.Length)
	}
}
