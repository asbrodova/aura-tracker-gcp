package gcp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	dlpv2 "cloud.google.com/go/dlp/apiv2"
	"cloud.google.com/go/dlp/apiv2/dlppb"
	"golang.org/x/time/rate"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

// dlpAdapter implements ports.DLPService using the GCP DLP API.
type dlpAdapter struct {
	client      *dlpv2.Client
	limiter     *rate.Limiter
	callTimeout time.Duration
	log         *slog.Logger
}

// NewDLPAdapter creates a dlpAdapter backed by Application Default Credentials.
// Call Close() when done to release the gRPC connection.
func NewDLPAdapter(ctx context.Context, log *slog.Logger) (*dlpAdapter, error) {
	a := &dlpAdapter{
		limiter:     rate.NewLimiter(10, 20),
		callTimeout: 30 * time.Second,
		log:         log,
	}
	var err error
	a.client, err = dlpv2.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("gcp: create dlp client: %w", err)
	}
	return a, nil
}

// Close releases the underlying gRPC connection.
func (a *dlpAdapter) Close() error { return a.client.Close() }

func (a *dlpAdapter) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, a.callTimeout)
}

func (a *dlpAdapter) rateWait(ctx context.Context, op string) error {
	if err := a.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("%s: rate limiter: %w", op, err)
	}
	return nil
}

var defaultInfoTypes = []string{
	"EMAIL_ADDRESS",
	"IP_ADDRESS",
	"PHONE_NUMBER",
	"CREDIT_CARD_NUMBER",
	"US_SOCIAL_SECURITY_NUMBER",
}

func (a *dlpAdapter) InspectText(ctx context.Context, req ports.DLPInspectRequest) (ports.DLPInspectResponse, error) {
	if err := a.rateWait(ctx, "dlp.InspectText"); err != nil {
		return ports.DLPInspectResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	infoTypeNames := req.InfoTypes
	if len(infoTypeNames) == 0 {
		infoTypeNames = defaultInfoTypes
	}
	pbInfoTypes := make([]*dlppb.InfoType, len(infoTypeNames))
	for i, name := range infoTypeNames {
		pbInfoTypes[i] = &dlppb.InfoType{Name: name}
	}

	pbReq := &dlppb.InspectContentRequest{
		Parent: fmt.Sprintf("projects/%s/locations/global", req.ProjectID),
		InspectConfig: &dlppb.InspectConfig{
			InfoTypes:     pbInfoTypes,
			IncludeQuote:  true,
			MinLikelihood: dlppb.Likelihood_POSSIBLE,
		},
		Item: &dlppb.ContentItem{
			DataItem: &dlppb.ContentItem_Value{Value: req.Content},
		},
	}

	resp, err := a.client.InspectContent(ctx, pbReq)
	if err != nil {
		return ports.DLPInspectResponse{}, wrapGCPError("dlp.InspectText", err)
	}

	raw := resp.GetResult().GetFindings()
	findings := make([]models.DLPFinding, 0, len(raw))
	for _, f := range raw {
		findings = append(findings, mapFinding(f))
	}
	return ports.DLPInspectResponse{Findings: findings}, nil
}

// mapFinding converts one dlppb.Finding to models.DLPFinding.
// Extracted for unit testing without a live DLP client.
func mapFinding(f *dlppb.Finding) models.DLPFinding {
	out := models.DLPFinding{
		InfoType: f.GetInfoType().GetName(),
		Quote:    f.GetQuote(),
	}
	if br := f.GetLocation().GetByteRange(); br != nil {
		out.Offset = int(br.GetStart())
		out.Length = int(br.GetEnd() - br.GetStart())
	}
	return out
}

// Compile-time interface check.
var _ ports.DLPService = (*dlpAdapter)(nil)
