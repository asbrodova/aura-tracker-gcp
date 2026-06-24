package testutil_test

import (
	"github.com/asbrodova/aura-tracker-gcp/internal/testutil"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

// Compile-time assertion: FakeGCPService satisfies ports.GCPService.
var _ ports.GCPService = (*testutil.FakeGCPService)(nil)
