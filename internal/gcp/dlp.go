package gcp

import (
	"context"
	"fmt"

	"github.com/asbrodova/aura-tracker-gcp/ports"
)

// dlpAdapter implements ports.DLPService using the GCP DLP API.
// Phase 2: full implementation is deferred; the skeleton ensures the interface
// is satisfiable before the cloud.google.com/go/dlp SDK is wired.
type dlpAdapter struct{}

// NewDLPAdapter creates a dlpAdapter. Wire this in main.go when adding Phase 2.
func NewDLPAdapter() *dlpAdapter { return &dlpAdapter{} }

func (a *dlpAdapter) InspectText(_ context.Context, _ ports.DLPInspectRequest) (ports.DLPInspectResponse, error) {
	return ports.DLPInspectResponse{}, fmt.Errorf("dlp adapter: not yet implemented (Phase 2)")
}

// Compile-time interface check.
var _ ports.DLPService = (*dlpAdapter)(nil)
