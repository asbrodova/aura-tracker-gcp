package gcp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"google.golang.org/api/cloudresourcemanager/v1"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (a *gcpAdapter) TestPermissions(ctx context.Context, req models.TestPermissionsRequest) (models.TestPermissionsResponse, error) {
	if err := a.rateWait(ctx, "iam.TestPermissions"); err != nil {
		return models.TestPermissionsResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	resp, err := a.crm.Projects.TestIamPermissions(
		req.ProjectID,
		&cloudresourcemanager.TestIamPermissionsRequest{
			Permissions: req.Permissions,
		},
	).Context(ctx).Do()
	if err != nil {
		return models.TestPermissionsResponse{}, wrapGCPError("iam.TestPermissions", err)
	}

	// Build a set of allowed permissions from the response.
	allowed := make(map[string]bool, len(resp.Permissions))
	for _, p := range resp.Permissions {
		allowed[p] = true
	}

	results := make([]models.PermissionResult, 0, len(req.Permissions))
	for _, p := range req.Permissions {
		results = append(results, models.PermissionResult{
			Permission: p,
			Allowed:    allowed[p],
		})
	}

	identity := resolveCallerIdentity(ctx)
	if identity == "" {
		identity = fmt.Sprintf("project:%s (ADC)", req.ProjectID)
	}

	return models.TestPermissionsResponse{
		ProjectID:      req.ProjectID,
		Results:        results,
		CallerIdentity: identity,
	}, nil
}

// resolveCallerIdentity queries the GCP Metadata Server for the service account email
// of the running instance. Returns "" when not running on GCP (e.g. local dev with ADC).
func resolveCallerIdentity(ctx context.Context) string {
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet,
		"http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/email", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}
	email, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(email))
}
