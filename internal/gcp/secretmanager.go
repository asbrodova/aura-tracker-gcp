package gcp

import (
	"context"
	"fmt"
	"strings"

	runpb "cloud.google.com/go/run/apiv2/runpb"
	secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (a *gcpAdapter) ListSecrets(ctx context.Context, req models.ListSecretsRequest) (models.ListSecretsResponse, error) {
	if err := a.rateWait(ctx, "secretmanager.ListSecrets"); err != nil {
		return models.ListSecretsResponse{}, err
	}
	if a.secretMgrClient == nil {
		return models.ListSecretsResponse{Secrets: []models.SecretSummary{}}, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	listReq := &secretmanagerpb.ListSecretsRequest{
		Parent: fmt.Sprintf("projects/%s", req.ProjectID),
		Filter: req.Filter,
	}
	it := a.secretMgrClient.ListSecrets(ctx, listReq)

	var secrets []models.SecretSummary
	for {
		s, err := it.Next()
		if err != nil {
			if isIteratorDone(err) {
				break
			}
			return models.ListSecretsResponse{}, wrapGCPError("secretmanager.ListSecrets", err)
		}
		name := parseSecretResourceName(s.Name)
		createTime := ""
		if s.CreateTime != nil {
			createTime = s.CreateTime.AsTime().Format(tsFormat)
		}
		replication := "automatic"
		if s.Replication != nil && s.Replication.GetUserManaged() != nil {
			replication = "user_managed"
		}
		secrets = append(secrets, models.SecretSummary{
			Name:        name,
			CreateTime:  createTime,
			Labels:      s.Labels,
			Replication: replication,
		})
	}
	if secrets == nil {
		secrets = []models.SecretSummary{}
	}

	if req.IncludeReferences {
		secrets = a.enrichSecretsWithReferences(ctx, req.ProjectID, secrets)
	}

	return models.ListSecretsResponse{Secrets: secrets}, nil
}

// enrichSecretsWithReferences scans all Cloud Run services for secretKeyRef
// references and populates SecretSummary.ReferencedBy with the service names.
func (a *gcpAdapter) enrichSecretsWithReferences(ctx context.Context, projectID string, secrets []models.SecretSummary) []models.SecretSummary {
	if a.runSvc == nil {
		return secrets
	}

	// Build a lookup map: bare secret name → index in secrets slice.
	idx := make(map[string]int, len(secrets))
	for i, s := range secrets {
		idx[s.Name] = i
	}
	// Also support full resource name as key.
	fullIdx := make(map[string]int, len(secrets))
	for i, s := range secrets {
		fullIdx[fmt.Sprintf("projects/%s/secrets/%s", projectID, s.Name)] = i
	}

	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	it := a.runSvc.ListServices(ctx, &runpb.ListServicesRequest{
		Parent: fmt.Sprintf("projects/%s/locations/-", projectID),
	})
	for {
		svc, err := it.Next()
		if err != nil {
			break
		}
		if svc.Labels["goog-managed-by"] == "cloudfunctions" {
			continue
		}
		_, svcName := parseSvcResourceName(svc.Name)
		if svc.Template == nil {
			continue
		}
		for _, c := range svc.Template.Containers {
			for _, env := range c.Env {
				if vs := env.GetValueSource(); vs != nil {
					if skr := vs.GetSecretKeyRef(); skr != nil {
						ref := skr.GetSecret()
						if i, ok := idx[ref]; ok {
							secrets[i].ReferencedBy = appendUnique(secrets[i].ReferencedBy, svcName)
						} else if i, ok := fullIdx[ref]; ok {
							secrets[i].ReferencedBy = appendUnique(secrets[i].ReferencedBy, svcName)
						}
					}
				}
			}
		}
	}
	return secrets
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}

// parseSecretResourceName extracts the bare secret name from:
// "projects/P/secrets/NAME"
func parseSecretResourceName(fullName string) string {
	parts := strings.Split(fullName, "/")
	if len(parts) == 4 && parts[0] == "projects" && parts[2] == "secrets" {
		return parts[3]
	}
	return fullName
}
