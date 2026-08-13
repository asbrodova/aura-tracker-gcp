package securityaudit

import (
	"strings"
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func TestGoogleManagedServiceAgentRequiresCanonicalIAMIdentity(t *testing.T) {
	for _, member := range []string{
		"serviceAccount:123456789@cloudservices.gserviceaccount.com",
		"serviceAccount:service-123456789@gcp-sa-run.iam.gserviceaccount.com",
		"serviceAccount:service-123456789@container-engine-robot.iam.gserviceaccount.com",
	} {
		if !isGoogleManagedServiceAgent(member) {
			t.Errorf("canonical service agent %q was not recognized", member)
		}
	}
	for _, member := range []string{
		"user:service-owner@example.com",
		"group:admins@gcp-sa-example.com",
		"serviceAccount:service-attacker@example.com",
		"serviceAccount:not-numeric@cloudservices.gserviceaccount.com",
		"serviceAccount:service-not-numeric@gcp-sa-run.iam.gserviceaccount.com",
	} {
		if isGoogleManagedServiceAgent(member) {
			t.Errorf("ordinary principal %q was classified as a service agent", member)
		}
	}

	findings := evaluateIAM(models.SecurityIAMPolicyFacts{Policies: []models.SecurityIAMPolicyFact{{
		Resource: "//cloudresourcemanager.googleapis.com/projects/p", Bindings: []models.SecurityIAMBindingFact{{
			Role: "roles/owner", Members: []string{"user:service-owner@example.com"},
		}},
	}}})
	if !hasRule(findings, "IAM-002") {
		t.Fatalf("ordinary principal with service-like text escaped IAM-002: %v", findingRules(findings))
	}
}

func TestDenyPrincipalMatchingPreservesIdentityType(t *testing.T) {
	if !denyPrincipalMatches("principal://goog/subject/admin@example.com", "user:admin@example.com") {
		t.Fatal("matching user deny principal was missed")
	}
	if denyPrincipalMatches("principalSet://goog/group/admin@example.com", "user:admin@example.com") {
		t.Fatal("group deny principal was conflated with a user having the same email")
	}
	if denyPrincipalMatches("principal://iam.googleapis.com/projects/-/serviceAccounts/admin@example.com", "group:admin@example.com") {
		t.Fatal("service-account deny principal was conflated with a group")
	}
	if !denyPrincipalMatches("principalSet://goog/public:all", "serviceAccount:runtime@example.iam.gserviceaccount.com") {
		t.Fatal("universal deny principal did not match a named principal")
	}
}

func TestEvaluateFirewallsHonorsSourceSelectorsAndSecureTagState(t *testing.T) {
	base := models.FirewallSecurityFact{
		ResourceName: "allow", Network: "default", Direction: "INGRESS", Action: "allow", EffectiveOrder: 2,
		Allowed: []models.FirewallProtocolFact{{Protocol: "tcp", Ports: []string{"22"}}},
	}
	tests := []struct {
		name   string
		mutate func(*models.FirewallSecurityFact)
		world  bool
	}{
		{name: "unrestricted", world: true, mutate: func(*models.FirewallSecurityFact) {}},
		{name: "secure tag", mutate: func(f *models.FirewallSecurityFact) { f.SourceSecureTags = []string{"tagValues/1:EFFECTIVE"} }},
		{name: "address group", mutate: func(f *models.FirewallSecurityFact) { f.SourceAddressGroups = []string{"addressGroups/trusted"} }},
		{name: "fqdn", mutate: func(f *models.FirewallSecurityFact) { f.SourceFQDNs = []string{"trusted.example"} }},
		{name: "network", mutate: func(f *models.FirewallSecurityFact) { f.SourceNetworks = []string{"networks/trusted"} }},
		{name: "region", mutate: func(f *models.FirewallSecurityFact) { f.SourceRegionCodes = []string{"ID"} }},
		{name: "threat intelligence", mutate: func(f *models.FirewallSecurityFact) { f.SourceThreatIntel = []string{"iplist-known-malicious-ips"} }},
		{name: "network context", mutate: func(f *models.FirewallSecurityFact) { f.SourceNetworkContext = "INTRA_VPC" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			firewall := base
			test.mutate(&firewall)
			if got := hasRule(evaluateFirewalls(models.FirewallSecurityFacts{Firewalls: []models.FirewallSecurityFact{firewall}}), "FW-002"); got != test.world {
				t.Fatalf("FW-002 present=%v, want %v", got, test.world)
			}
		})
	}

	targeted := base
	targeted.TargetSecureTags = []string{"tagValues/1:EFFECTIVE"}
	findings := evaluateFirewalls(models.FirewallSecurityFacts{Firewalls: []models.FirewallSecurityFact{targeted}})
	for _, finding := range findings {
		for _, evidence := range finding.Evidence {
			if strings.Contains(evidence, "applies to all instances") {
				t.Fatalf("secure-tag-targeted rule was described as untargeted: %+v", finding)
			}
		}
	}

	inactive := base
	inactive.TargetSecureTags = []string{"tagValues/1:INEFFECTIVE"}
	if findings := evaluateFirewalls(models.FirewallSecurityFacts{Firewalls: []models.FirewallSecurityFact{inactive}}); len(findings) != 0 {
		t.Fatalf("rule with only ineffective targets produced findings: %+v", findings)
	}
}

func TestIneffectiveSecureTagDenyDoesNotShadowActiveAllow(t *testing.T) {
	allow := models.FirewallSecurityFact{
		ResourceName: "allow", Network: "default", Direction: "INGRESS", Action: "allow", EffectiveOrder: 2,
		SourceRanges: []string{"0.0.0.0/0"}, TargetSecureTags: []string{"tagValues/1:EFFECTIVE"},
		Allowed: []models.FirewallProtocolFact{{Protocol: "tcp", Ports: []string{"22"}}},
	}
	deny := models.FirewallSecurityFact{
		ResourceName: "deny", Network: "default", Direction: "INGRESS", Action: "deny", EffectiveOrder: 1,
		SourceRanges: []string{"0.0.0.0/0"}, TargetSecureTags: []string{"tagValues/1:INEFFECTIVE"},
		Denied: []models.FirewallProtocolFact{{Protocol: "all"}},
	}
	findings := evaluateFirewalls(models.FirewallSecurityFacts{Firewalls: []models.FirewallSecurityFact{deny, allow}})
	if hasRule(findings, "FW-006") || !hasRule(findings, "FW-002") {
		t.Fatalf("rules = %v, want active exposure without false shadowing", findingRules(findings))
	}
}

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
