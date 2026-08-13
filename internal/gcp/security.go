package gcp

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	runpb "cloud.google.com/go/run/apiv2/runpb"
	secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/api/cloudasset/v1"
	"google.golang.org/api/cloudfunctions/v1"
	iamapi "google.golang.org/api/iam/v1"
	"google.golang.org/api/iterator"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

const maxSecurityIAMPolicies = 10000

var errSecurityPolicyLimit = errors.New("security IAM policy limit reached")

func (a *gcpAdapter) SearchSecurityIAMPolicies(ctx context.Context, req models.SecurityFactsRequest) (models.SecurityIAMPolicyFacts, error) {
	if err := a.rateWait(ctx, "security.SearchSecurityIAMPolicies"); err != nil {
		return models.SecurityIAMPolicyFacts{}, err
	}
	if a.assetSvc == nil {
		return models.SecurityIAMPolicyFacts{}, errors.New("security.SearchSecurityIAMPolicies: Cloud Asset client is unavailable")
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	hierarchy, ancestorPolicies, denyRules, coverage := a.collectSecurityHierarchy(ctx, req.ProjectID)
	result := models.SecurityIAMPolicyFacts{
		Hierarchy: hierarchy, Policies: ancestorPolicies, DenyRules: denyRules, Coverage: coverage,
	}
	err := a.assetSvc.V1.SearchAllIamPolicies("projects/"+req.ProjectID).
		PageSize(500).
		Pages(ctx, func(page *cloudasset.SearchAllIamPoliciesResponse) error {
			for _, item := range page.Results {
				if item == nil || item.Policy == nil {
					continue
				}
				fact := models.SecurityIAMPolicyFact{
					Resource:    item.Resource,
					AssetType:   item.AssetType,
					OriginScope: "projects/" + req.ProjectID,
					ScopeType:   "resource",
					PolicyKind:  "allow",
					Bindings:    make([]models.SecurityIAMBindingFact, 0, len(item.Policy.Bindings)),
				}
				if item.AssetType == "cloudresourcemanager.googleapis.com/Project" {
					fact.ScopeType = "project"
				}
				for _, binding := range item.Policy.Bindings {
					if binding == nil {
						continue
					}
					out := models.SecurityIAMBindingFact{Role: binding.Role, Members: append([]string(nil), binding.Members...)}
					if binding.Condition != nil {
						out.Condition = &models.SecurityIAMCondition{
							Title: binding.Condition.Title, Description: binding.Condition.Description,
							Expression: binding.Condition.Expression,
						}
					}
					sort.Strings(out.Members)
					fact.Bindings = append(fact.Bindings, out)
				}
				result.Policies = append(result.Policies, fact)
				if len(result.Policies) >= maxSecurityIAMPolicies {
					result.Truncated = true
					return errSecurityPolicyLimit
				}
			}
			return nil
		})
	if err != nil && !errors.Is(err, errSecurityPolicyLimit) {
		return models.SecurityIAMPolicyFacts{}, wrapGCPError("security.SearchSecurityIAMPolicies", err)
	}
	status, message := "complete", ""
	if result.Truncated {
		status, message = "truncated", "Cloud Asset IAM policy search reached the 10,000-policy safety cap"
	}
	result.Coverage = append(result.Coverage, securityCoverageUnit("iam_project_resources", "project", "projects/"+req.ProjectID, status, len(result.Policies)-len(ancestorPolicies), message))
	sort.Slice(result.Policies, func(i, j int) bool { return result.Policies[i].Resource < result.Policies[j].Resource })
	return result, nil
}

func (a *gcpAdapter) ListServiceAccountSecurityFacts(ctx context.Context, req models.SecurityFactsRequest) (models.ServiceAccountSecurityFacts, error) {
	if err := a.rateWait(ctx, "security.ListServiceAccountSecurityFacts"); err != nil {
		return models.ServiceAccountSecurityFacts{}, err
	}
	if a.iamAdminSvc == nil {
		return models.ServiceAccountSecurityFacts{}, errors.New("security.ListServiceAccountSecurityFacts: IAM client is unavailable")
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	result := models.ServiceAccountSecurityFacts{ServiceAccounts: []models.ServiceAccountSecurityFact{}}
	err := a.iamAdminSvc.Projects.ServiceAccounts.List("projects/"+req.ProjectID).
		Pages(ctx, func(page *iamapi.ListServiceAccountsResponse) error {
			for _, sa := range page.Accounts {
				if err := a.rateWait(ctx, "security.ListServiceAccountKeys"); err != nil {
					return err
				}
				keys, err := a.iamAdminSvc.Projects.ServiceAccounts.Keys.List(sa.Name).
					KeyTypes("USER_MANAGED").Context(ctx).Do()
				if err != nil {
					return err
				}
				fact := models.ServiceAccountSecurityFact{
					Name: sa.Name, Email: sa.Email, DisplayName: sa.DisplayName,
					Description: sa.Description, Disabled: sa.Disabled, Keys: []models.ServiceAccountKeyFact{},
				}
				for _, key := range keys.Keys {
					exposed := strings.Contains(key.DisableReason, "EXPOSED") || strings.Contains(key.DisableReason, "COMPROMISE")
					for _, status := range key.ExtendedStatus {
						exposed = exposed || strings.Contains(status.Key, "EXPOSED") || strings.Contains(status.Key, "COMPROMISE")
					}
					fact.Keys = append(fact.Keys, models.ServiceAccountKeyFact{
						Name: key.Name, ID: path.Base(key.Name), Origin: key.KeyOrigin, KeyType: key.KeyType,
						ValidAfterTime: key.ValidAfterTime, ValidBeforeTime: key.ValidBeforeTime,
						Disabled: key.Disabled, DisableReason: key.DisableReason, Exposed: exposed,
					})
				}
				sort.Slice(fact.Keys, func(i, j int) bool { return fact.Keys[i].ID < fact.Keys[j].ID })
				result.ServiceAccounts = append(result.ServiceAccounts, fact)
			}
			return nil
		})
	if err != nil {
		return models.ServiceAccountSecurityFacts{}, wrapGCPError("security.ListServiceAccountSecurityFacts", err)
	}
	sort.Slice(result.ServiceAccounts, func(i, j int) bool { return result.ServiceAccounts[i].Email < result.ServiceAccounts[j].Email })
	return result, nil
}

func (a *gcpAdapter) ListSecretSecurityFacts(ctx context.Context, req models.SecurityFactsRequest) (models.SecretSecurityFacts, error) {
	if err := a.rateWait(ctx, "security.ListSecretSecurityFacts"); err != nil {
		return models.SecretSecurityFacts{}, err
	}
	if a.secretMgrClient == nil {
		return models.SecretSecurityFacts{}, errors.New("security.ListSecretSecurityFacts: Secret Manager client is unavailable")
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	result := models.SecretSecurityFacts{Secrets: []models.SecretSecurityFact{}}
	it := a.secretMgrClient.ListSecrets(ctx, &secretmanagerpb.ListSecretsRequest{Parent: "projects/" + req.ProjectID})
	for {
		secret, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return models.SecretSecurityFacts{}, wrapGCPError("security.ListSecretSecurityFacts", err)
		}
		if err := a.rateWait(ctx, "security.ListSecretVersions"); err != nil {
			return models.SecretSecurityFacts{}, err
		}
		fact := secretSecurityFact(secret)
		versions := a.secretMgrClient.ListSecretVersions(ctx, &secretmanagerpb.ListSecretVersionsRequest{
			Parent: secret.Name, PageSize: 100,
		})
		for len(fact.Versions) < 100 {
			version, err := versions.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return models.SecretSecurityFacts{}, wrapGCPError("security.ListSecretVersions", err)
			}
			created := ""
			if version.CreateTime != nil {
				created = version.CreateTime.AsTime().UTC().Format(tsFormat)
			}
			fact.Versions = append(fact.Versions, models.SecretVersionSecurityFact{
				Name: path.Base(version.Name), CreateTime: created, State: version.State.String(),
			})
		}
		if len(fact.Versions) == 100 {
			result.Warnings = appendUnique(result.Warnings, "secret version metadata is capped at 100 versions per secret")
		}
		result.Secrets = append(result.Secrets, fact)
	}
	if len(result.Secrets) > 0 {
		summaries := make([]models.SecretSummary, len(result.Secrets))
		for i := range result.Secrets {
			summaries[i] = models.SecretSummary{Name: result.Secrets[i].Name}
		}
		var referencesTruncated bool
		summaries, referencesTruncated = a.enrichSecretsWithReferences(ctx, req.ProjectID, summaries)
		if referencesTruncated {
			result.Warnings = appendUnique(result.Warnings, "Cloud Run secret-reference discovery was incomplete")
		}
		for i := range summaries {
			result.Secrets[i].ReferencedBy = summaries[i].ReferencedBy
		}
	}
	sort.Slice(result.Secrets, func(i, j int) bool { return result.Secrets[i].Name < result.Secrets[j].Name })
	return result, nil
}

func secretSecurityFact(secret *secretmanagerpb.Secret) models.SecretSecurityFact {
	fact := models.SecretSecurityFact{
		Name: path.Base(secret.Name), ResourceName: "//secretmanager.googleapis.com/" + secret.Name,
		Replication: "automatic", TopicCount: len(secret.Topics), Versions: []models.SecretVersionSecurityFact{},
	}
	if secret.CreateTime != nil {
		fact.CreateTime = secret.CreateTime.AsTime().UTC().Format(tsFormat)
	}
	if secret.Replication != nil && secret.Replication.GetUserManaged() != nil {
		fact.Replication = "user_managed"
	}
	if secret.Rotation != nil {
		if secret.Rotation.RotationPeriod != nil {
			fact.RotationPeriod = secret.Rotation.RotationPeriod.String()
		}
		if secret.Rotation.NextRotationTime != nil {
			fact.NextRotationTime = secret.Rotation.NextRotationTime.AsTime().UTC().Format(tsFormat)
		}
	}
	if secret.GetExpireTime() != nil {
		fact.ExpireTime = secret.GetExpireTime().AsTime().UTC().Format(tsFormat)
	}
	return fact
}

func (a *gcpAdapter) ListPublicServiceSecurityFacts(ctx context.Context, req models.SecurityFactsRequest) (models.PublicServiceSecurityFacts, error) {
	if err := a.rateWait(ctx, "security.ListPublicServiceSecurityFacts"); err != nil {
		return models.PublicServiceSecurityFacts{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	result := models.PublicServiceSecurityFacts{
		Services: []models.PublicServiceSecurityFact{},
		Coverage: []models.SecurityCoverageUnit{},
	}
	if a.runSvc != nil {
		it := a.runSvc.ListServices(ctx, &runpb.ListServicesRequest{Parent: "projects/" + req.ProjectID + "/locations/-"})
		for {
			service, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return models.PublicServiceSecurityFacts{}, wrapGCPError("security.ListPublicServiceSecurityFacts.CloudRun", err)
			}
			region, name := parseSvcResourceName(service.Name)
			fact := models.PublicServiceSecurityFact{
				ResourceName: "//run.googleapis.com/" + service.Name, Name: name, Kind: "cloud_run", Region: region,
				Ingress: service.Ingress.String(), InvokerIAMDisabled: service.InvokerIamDisabled,
				IAPEnabled: service.IapEnabled, DefaultURIDisabled: service.DefaultUriDisabled,
			}
			if service.Template != nil {
				fact.ServiceAccount = service.Template.ServiceAccount
				for _, container := range service.Template.Containers {
					for _, env := range container.Env {
						if ref := env.GetValueSource().GetSecretKeyRef(); ref != nil {
							fact.SecretReferences = appendUnique(fact.SecretReferences, ref.Secret)
						}
					}
				}
			}
			result.Services = append(result.Services, fact)
		}
	}
	if a.fnGen1 != nil {
		parent := "projects/" + req.ProjectID + "/locations/-"
		err := a.fnGen1.Projects.Locations.Functions.List(parent).Context(ctx).Pages(ctx, func(page *cloudfunctions.ListFunctionsResponse) error {
			for _, fn := range page.Functions {
				if fn.HttpsTrigger == nil {
					continue
				}
				parts := strings.Split(fn.Name, "/")
				region, name := "", path.Base(fn.Name)
				if len(parts) >= 6 {
					region = parts[3]
				}
				result.Services = append(result.Services, models.PublicServiceSecurityFact{
					ResourceName: "//cloudfunctions.googleapis.com/" + fn.Name, Name: name,
					Kind: "cloud_function_gen1", Region: region, Ingress: fn.IngressSettings,
					ServiceAccount: fn.ServiceAccountEmail, External: true,
					TLSEnabled: fn.HttpsTrigger.SecurityLevel != "SECURE_OPTIONAL",
				})
			}
			return nil
		})
		if err != nil {
			return models.PublicServiceSecurityFacts{}, wrapGCPError("security.ListPublicServiceSecurityFacts.Functions", err)
		}
	}
	a.collectGKEPublicServiceSecurityFacts(ctx, req.ProjectID, &result)
	sort.Slice(result.Services, func(i, j int) bool { return result.Services[i].ResourceName < result.Services[j].ResourceName })
	return result, nil
}

func (a *gcpAdapter) ListFirewallSecurityFacts(ctx context.Context, req models.SecurityFactsRequest) (models.FirewallSecurityFacts, error) {
	if err := a.rateWait(ctx, "security.ListFirewallSecurityFacts"); err != nil {
		return models.FirewallSecurityFacts{}, err
	}
	if a.computeSvc == nil {
		return models.FirewallSecurityFacts{}, errors.New("security.ListFirewallSecurityFacts: Compute client is unavailable")
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	return a.collectEffectiveFirewallSecurityFacts(ctx, req.ProjectID)
}

func (a *gcpAdapter) ListWorkloadIdentitySecurityFacts(ctx context.Context, req models.SecurityFactsRequest) (models.WorkloadIdentitySecurityFacts, error) {
	if err := a.rateWait(ctx, "security.ListWorkloadIdentitySecurityFacts"); err != nil {
		return models.WorkloadIdentitySecurityFacts{}, err
	}
	if a.clusterMgr == nil {
		return models.WorkloadIdentitySecurityFacts{}, errors.New("security.ListWorkloadIdentitySecurityFacts: GKE client is unavailable")
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	return a.collectWorkloadIdentitySecurityFacts(ctx, req.ProjectID)
}

func (a *gcpAdapter) ListSecurityRecommendations(ctx context.Context, req models.SecurityFactsRequest) (models.SecurityRecommendationFacts, error) {
	const op = "security.ListSecurityRecommendations"
	if a.rec == nil {
		return models.SecurityRecommendationFacts{Recommendations: []models.SecurityRecommendationFact{}, Enabled: false}, nil
	}
	if err := a.rateWait(ctx, op); err != nil {
		return models.SecurityRecommendationFacts{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	result := models.SecurityRecommendationFacts{Recommendations: []models.SecurityRecommendationFact{}, Enabled: true}
	for _, recommenderID := range []string{"google.iam.policy.Recommender"} {
		it, err := a.activeRecommendations(ctx, op, req.ProjectID, "global", recommenderID, maxInventoryPageSize)
		if err != nil {
			return models.SecurityRecommendationFacts{}, err
		}
		for {
			recommendation, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return models.SecurityRecommendationFacts{}, err
			}
			if len(result.Recommendations) >= maxUnpagedInventoryItems {
				result.Truncated = true
				result.Warnings = append(result.Warnings, fmt.Sprintf("security recommendation inventory reached the %d-item safety cap", maxUnpagedInventoryItems))
				break
			}
			fact := models.SecurityRecommendationFact{
				Name: recommendation.Name, RecommenderID: recommenderID, Subtype: recommendation.RecommenderSubtype,
				Description: recommendation.Description, Priority: recommendation.Priority.String(),
			}
			if recommendation.LastRefreshTime != nil {
				fact.LastRefreshTime = recommendation.LastRefreshTime.AsTime().UTC().Format(tsFormat)
			}
			if recommendation.Content != nil {
				for _, group := range recommendation.Content.OperationGroups {
					for _, operation := range group.Operations {
						if operation.Resource != "" {
							fact.TargetResources = appendUnique(fact.TargetResources, operation.Resource)
						}
					}
				}
			}
			result.Recommendations = append(result.Recommendations, fact)
		}
	}
	sort.Slice(result.Recommendations, func(i, j int) bool { return result.Recommendations[i].Name < result.Recommendations[j].Name })
	return result, nil
}
