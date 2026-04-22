package gcp

import (
	"context"
	"fmt"

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

	return models.TestPermissionsResponse{
		ProjectID: req.ProjectID,
		Results:   results,
		// CallerIdentity is populated from the service account ADC if available.
		// For simplicity, we surface the project resource being tested.
		CallerIdentity: fmt.Sprintf("project:%s (caller identity from ADC)", req.ProjectID),
	}, nil
}
