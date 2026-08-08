package securityaudit

import (
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func TestPublicRolesForResourceIncludesProjectAndAncestorPolicies(t *testing.T) {
	facts := models.SecurityIAMPolicyFacts{Policies: []models.SecurityIAMPolicyFact{
		{
			Resource: "//cloudresourcemanager.googleapis.com/folders/123", ScopeType: "folder", Inherited: true,
			Bindings: []models.SecurityIAMBindingFact{{Role: "roles/run.invoker", Members: []string{"allUsers"}}},
		},
		{
			Resource: "//cloudresourcemanager.googleapis.com/projects/456", ScopeType: "project",
			Bindings: []models.SecurityIAMBindingFact{{Role: "roles/secretmanager.secretAccessor", Members: []string{"allUsers"}}},
		},
	}}

	roles := publicRolesForResource(facts, "//run.googleapis.com/projects/example/locations/us/services/api")
	if !contains(roles, "roles/run.invoker") || !contains(roles, "roles/secretmanager.secretAccessor") {
		t.Fatalf("inherited public roles = %v", roles)
	}
}

func TestEvaluatePublicServicesFindsGKEExposure(t *testing.T) {
	facts := models.PublicServiceSecurityFacts{Services: []models.PublicServiceSecurityFact{{
		ResourceName: "//container.googleapis.com/projects/p/locations/us/clusters/c/kubernetes/namespaces/default/ingresses/web",
		Name:         "web", Kind: "gke_ingress", External: true, TLSEnabled: false, Ports: []int32{443}, ExposureReason: "external Kubernetes Ingress",
	}}}

	findings := evaluatePublicServices(facts, models.SecurityIAMPolicyFacts{})
	if !hasRule(findings, "PUB-001") || !hasRule(findings, "PUB-002") {
		t.Fatalf("rules = %v, want PUB-001 and PUB-002", findingRules(findings))
	}
}

func TestEvaluateFirewallsDoesNotReportShadowedAllowAsExposure(t *testing.T) {
	facts := models.FirewallSecurityFacts{Firewalls: []models.FirewallSecurityFact{
		{
			ResourceName: "deny", Network: "default", Direction: "INGRESS", Action: "deny", EffectiveOrder: 1,
			SourceRanges: []string{"0.0.0.0/0"}, Denied: []models.FirewallProtocolFact{{Protocol: "all"}},
		},
		{
			ResourceName: "allow", Network: "default", Direction: "INGRESS", Action: "allow", EffectiveOrder: 2,
			SourceRanges: []string{"0.0.0.0/0"}, Allowed: []models.FirewallProtocolFact{{Protocol: "tcp", Ports: []string{"22"}}},
		},
	}}

	findings := evaluateFirewalls(facts)
	if hasRule(findings, "FW-002") {
		t.Fatalf("shadowed allow was reported as active exposure: %v", findingRules(findings))
	}
	if !hasRule(findings, "FW-006") {
		t.Fatalf("rules = %v, want FW-006", findingRules(findings))
	}
}

func TestCorrelateWorkloadIdentityMapsDirectNamespaceAndLegacyRoles(t *testing.T) {
	workloads := models.WorkloadIdentitySecurityFacts{Clusters: []models.WorkloadIdentitySecurityFact{{
		WorkloadPool:    "project.svc.id.goog",
		ServiceAccounts: []models.KubernetesServiceAccountFact{{Namespace: "payments", Name: "api", GoogleServiceAccount: "runtime@gsa-project.iam.gserviceaccount.com"}},
	}}}
	direct := "principal://iam.googleapis.com/projects/123/locations/global/workloadIdentityPools/project.svc.id.goog/subject/ns/payments/sa/api"
	namespace := "principalSet://iam.googleapis.com/projects/123/locations/global/workloadIdentityPools/project.svc.id.goog/namespace/payments"
	legacy := "serviceAccount:project.svc.id.goog[payments/api]"
	iam := models.SecurityIAMPolicyFacts{Policies: []models.SecurityIAMPolicyFact{
		{Resource: "//cloudresourcemanager.googleapis.com/projects/project", Bindings: []models.SecurityIAMBindingFact{
			{Role: "roles/storage.objectViewer", Members: []string{direct}},
			{Role: "roles/logging.viewer", Members: []string{namespace}},
			{Role: "roles/editor", Members: []string{"serviceAccount:runtime@gsa-project.iam.gserviceaccount.com"}},
		}},
		{Resource: "//iam.googleapis.com/projects/gsa-project/serviceAccounts/runtime@gsa-project.iam.gserviceaccount.com", Bindings: []models.SecurityIAMBindingFact{
			{Role: "roles/iam.workloadIdentityUser", Members: []string{legacy}},
		}},
	}}

	correlateWorkloadIdentity(&workloads, iam)
	account := workloads.Clusters[0].ServiceAccounts[0]
	if !contains(account.DirectRoles, "roles/storage.objectViewer") || !contains(account.NamespaceRoles, "roles/logging.viewer") {
		t.Fatalf("direct=%v namespace=%v", account.DirectRoles, account.NamespaceRoles)
	}
	if !contains(account.MappedGSARoles, "roles/editor") || !account.ImpersonationVerified {
		t.Fatalf("mapped=%v impersonation=%v", account.MappedGSARoles, account.ImpersonationVerified)
	}
}

func TestGranularCoverageUsesCompletedUnitRatio(t *testing.T) {
	coverage := []models.SecurityCoverageCheck{{Status: "partial", Weight: 100, CompletedUnits: 1, TotalUnits: 4}}
	if got := coveragePercent(coverage); got != 25 {
		t.Fatalf("coveragePercent = %d, want 25", got)
	}
}

func hasRule(findings []models.SecurityFinding, rule string) bool {
	for _, finding := range findings {
		if finding.RuleID == rule {
			return true
		}
	}
	return false
}

func findingRules(findings []models.SecurityFinding) []string {
	rules := make([]string, 0, len(findings))
	for _, finding := range findings {
		rules = append(rules, finding.RuleID)
	}
	return rules
}
