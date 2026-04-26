package ports

import (
	"context"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// DLPInspectRequest is the input to a DLP text inspection call.
type DLPInspectRequest struct {
	Content   string
	InfoTypes []string
	ProjectID string
}

// DLPInspectResponse is the result of a DLP text inspection call.
type DLPInspectResponse struct {
	Findings []models.DLPFinding
}

// DLPService is the secondary hexagon port for GCP Data Loss Prevention.
// Implementations live in internal/gcp/dlp.go.
// All implementations must be safe for concurrent use.
type DLPService interface {
	InspectText(ctx context.Context, req DLPInspectRequest) (DLPInspectResponse, error)
}
