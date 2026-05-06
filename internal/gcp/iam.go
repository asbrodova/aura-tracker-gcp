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

func (a *gcpAdapter) GetResourceIAMBindings(ctx context.Context, req models.GetResourceIAMBindingsRequest) (models.GetResourceIAMBindingsResponse, error) {
	if err := a.rateWait(ctx, "iam.GetResourceIAMBindings"); err != nil {
		return models.GetResourceIAMBindingsResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	bindings, err := a.fetchResourceBindings(ctx, req.URN)
	if err != nil {
		return models.GetResourceIAMBindingsResponse{}, wrapGCPError("iam.GetResourceIAMBindings", err)
	}
	return models.GetResourceIAMBindingsResponse{URN: req.URN, Bindings: bindings}, nil
}

// fetchResourceBindings dispatches on URN kind to the appropriate getIamPolicy call.
// URN format: urn:gcp:{kind}:{region}:{project}:{resource-path}
//
// Supported kinds: storage_bucket (GCS IAM), project (CRM project policy).
// Other resource kinds return an informative error — extend the switch as
// resource-specific REST clients are added in later PRs.
func (a *gcpAdapter) fetchResourceBindings(ctx context.Context, urn string) ([]models.IAMBinding, error) {
	parts := strings.SplitN(urn, ":", 6)
	if len(parts) < 6 || parts[0] != "urn" || parts[1] != "gcp" {
		return nil, fmt.Errorf("unrecognised URN format: %q", urn)
	}
	kind, _, project, resource := parts[2], parts[3], parts[4], parts[5]

	switch kind {
	case "storage_bucket":
		if a.gcs == nil {
			return nil, nil
		}
		policy, err := a.gcs.Bucket(resource).IAM().Policy(ctx)
		if err != nil {
			return nil, err
		}
		var out []models.IAMBinding
		for _, role := range policy.Roles() {
			members := policy.Members(role)
			out = append(out, models.IAMBinding{
				Role:    string(role),
				Members: members,
			})
		}
		return out, nil

	case "project":
		if a.crm == nil {
			return nil, nil
		}
		resp, err := a.crm.Projects.GetIamPolicy(resource, &cloudresourcemanager.GetIamPolicyRequest{}).Context(ctx).Do()
		if err != nil {
			return nil, err
		}
		out := make([]models.IAMBinding, 0, len(resp.Bindings))
		for _, b := range resp.Bindings {
			out = append(out, models.IAMBinding{Role: b.Role, Members: b.Members})
		}
		return out, nil

	default:
		// project-level fallback for any other kind
		if a.crm == nil {
			return nil, fmt.Errorf("unsupported URN kind %q; supported: storage_bucket, project", kind)
		}
		resp, err := a.crm.Projects.GetIamPolicy(project, &cloudresourcemanager.GetIamPolicyRequest{}).Context(ctx).Do()
		if err != nil {
			return nil, err
		}
		out := make([]models.IAMBinding, 0, len(resp.Bindings))
		for _, b := range resp.Bindings {
			out = append(out, models.IAMBinding{Role: b.Role, Members: b.Members})
		}
		return out, nil
	}
}

func (a *gcpAdapter) ListServiceAccounts(ctx context.Context, req models.ListServiceAccountsRequest) (models.ListServiceAccountsResponse, error) {
	if err := a.rateWait(ctx, "iam.ListServiceAccounts"); err != nil {
		return models.ListServiceAccountsResponse{}, err
	}
	if a.iamAdminSvc == nil {
		return models.ListServiceAccountsResponse{ServiceAccounts: []models.ServiceAccountSummary{}}, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	resp, err := a.iamAdminSvc.Projects.ServiceAccounts.List("projects/" + req.ProjectID).Context(ctx).Do()
	if err != nil {
		return models.ListServiceAccountsResponse{}, wrapGCPError("iam.ListServiceAccounts", err)
	}

	accounts := make([]models.ServiceAccountSummary, 0, len(resp.Accounts))
	for _, sa := range resp.Accounts {
		accounts = append(accounts, models.ServiceAccountSummary{
			Name:        sa.Name,
			Email:       sa.Email,
			DisplayName: sa.DisplayName,
			Description: sa.Description,
			Disabled:    sa.Disabled,
			UniqueID:    sa.UniqueId,
		})
	}
	return models.ListServiceAccountsResponse{ServiceAccounts: accounts}, nil
}

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
