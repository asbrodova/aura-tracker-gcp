package gcp

import (
	"testing"
	"time"
)

func TestCreatedAssetQueryUsesDocumentedEpochTimestampSyntax(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	if got, want := createdAssetQuery(start, end), "createTime >= 1785542400 AND createTime < 1785628800"; got != want {
		t.Fatalf("createdAssetQuery() = %q, want %q", got, want)
	}
}
