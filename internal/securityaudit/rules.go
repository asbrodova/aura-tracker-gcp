package securityaudit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

const (
	docsIAM      = "https://cloud.google.com/iam/docs/using-iam-securely"
	docsSAKeys   = "https://cloud.google.com/iam/docs/best-practices-for-managing-service-account-keys"
	docsSecrets  = "https://cloud.google.com/secret-manager/docs/rotation-recommendations"
	docsCloudRun = "https://cloud.google.com/run/docs/authenticating/public"
	docsFirewall = "https://cloud.google.com/firewall/docs/firewalls"
	docsWIF      = "https://cloud.google.com/kubernetes-engine/docs/concepts/workload-identity"
)

func evaluateRules(facts collectedFacts, now time.Time, projectID string) []models.SecurityFinding {
	var findings []models.SecurityFinding
	if facts.errors[models.SecurityCategoryIAM] == nil {
		findings = append(findings, evaluateIAM(facts.iam)...)
	}
	if facts.errors[models.SecurityCategoryServiceAccounts] == nil {
		findings = append(findings, evaluateServiceAccounts(facts.serviceAccounts, facts.iam, now)...)
	}
	if facts.errors[models.SecurityCategorySecrets] == nil {
		findings = append(findings, evaluateSecrets(facts.secrets, facts.iam, now)...)
	}
	if facts.errors[models.SecurityCategoryPublicServices] == nil {
		findings = append(findings, evaluatePublicServices(facts.publicServices, facts.iam)...)
	}
	if facts.errors[models.SecurityCategoryFirewall] == nil {
		findings = append(findings, evaluateFirewalls(facts.firewalls)...)
	}
	if facts.errors[models.SecurityCategoryWorkloadIdentity] == nil {
		findings = append(findings, evaluateWorkloadIdentity(facts.workloadIdentity)...)
	}
	if facts.recommendationErr == nil && facts.recommendations.Enabled {
		findings = append(findings, evaluateProviderRecommendations(facts.recommendations, projectID)...)
	}
	return deduplicateAndSort(findings)
}

func newFinding(rule string, severity models.SecuritySeverity, category models.SecurityCategory, title, resource, risk, recommendation, docs string, evidence ...string) models.SecurityFinding {
	sum := sha256.Sum256([]byte(rule + "|" + resource + "|" + strings.Join(evidence, "|")))
	return models.SecurityFinding{
		ID: rule + "-" + strings.ToUpper(hex.EncodeToString(sum[:3])), RuleID: rule,
		Severity: severity, Category: category, Title: title, Resource: resource,
		Evidence: evidence, Risk: risk, Recommendation: recommendation,
		Source: "aura-rule", Confidence: "high", DocsURL: docs,
	}
}

func evaluateIAM(facts models.SecurityIAMPolicyFacts) []models.SecurityFinding {
	var out []models.SecurityFinding
	for _, policy := range facts.Policies {
		for _, binding := range policy.Bindings {
			for _, member := range binding.Members {
				conditional := binding.Condition != nil
				conditionNote := "unconditional binding"
				if conditional {
					conditionNote = "conditional binding: " + binding.Condition.Title
				}
				originNote := "direct binding"
				if policy.Inherited {
					originNote = "inherited from " + policy.OriginScope
				}
				denyNote := ""
				if count := matchingDenyRuleCount(facts.DenyRules, member); count > 0 {
					denyNote = fmt.Sprintf("%d inherited or direct deny rule(s) may constrain this allow grant", count)
				}
				if isPublicMember(member) {
					if isPublicInvocationRole(binding.Role) || isSecretAccessRole(binding.Role) {
						continue // Correlated by the resource-specific rule sets.
					}
					severity := models.SecuritySeverityHigh
					if isPrivilegedRole(binding.Role) {
						severity = models.SecuritySeverityCritical
					}
					out = append(out, newFinding("IAM-001", severity, models.SecurityCategoryIAM,
						"Public principal has an IAM role", policy.Resource,
						"A public principal can exercise the granted permissions without membership in your organization.",
						"Remove the public binding or replace it with the narrowest intended principal and role.", docsIAM,
						compactEvidence(fmt.Sprintf("%s has %s", member, binding.Role), conditionNote, originNote, denyNote)...))
					continue
				}
				if (binding.Role == "roles/owner" || binding.Role == "roles/editor") && !isGoogleManagedServiceAgent(member) {
					title := "Primitive Owner or Editor role granted"
					if isDefaultServiceAccount(member) {
						title = "Default service account has a primitive role"
					}
					out = append(out, newFinding("IAM-002", models.SecuritySeverityHigh, models.SecurityCategoryIAM,
						title, policy.Resource,
						"Primitive roles contain broad permissions and make least-privilege review difficult.",
						"Replace the primitive role with task-specific predefined or custom roles.", docsIAM,
						compactEvidence(fmt.Sprintf("%s has %s", member, binding.Role), conditionNote, originNote, denyNote)...))
				}
				if isProjectResource(policy.Resource) && (binding.Role == "roles/iam.serviceAccountTokenCreator" || binding.Role == "roles/iam.serviceAccountUser") {
					out = append(out, newFinding("IAM-003", models.SecuritySeverityHigh, models.SecurityCategoryIAM,
						"Service-account impersonation granted project-wide", policy.Resource,
						"The principal might impersonate or attach multiple service accounts in the project.",
						"Grant impersonation only on the individual service accounts that require it.", docsIAM,
						compactEvidence(fmt.Sprintf("%s has %s", member, binding.Role), conditionNote, originNote, denyNote)...))
				}
				if strings.HasPrefix(member, "deleted:") {
					out = append(out, newFinding("IAM-004", models.SecuritySeverityLow, models.SecurityCategoryIAM,
						"Deleted principal remains in IAM policy", policy.Resource,
						"Stale bindings obscure access reviews and might be restored if the principal is recovered.",
						"Confirm the principal is no longer needed and remove the stale binding.", docsIAM,
						fmt.Sprintf("%s retains %s", member, binding.Role)))
				}
			}
		}
	}
	return out
}

func evaluateServiceAccounts(facts models.ServiceAccountSecurityFacts, iamFacts models.SecurityIAMPolicyFacts, now time.Time) []models.SecurityFinding {
	var out []models.SecurityFinding
	bindings := serviceAccountBindingCount(iamFacts)
	for _, account := range facts.ServiceAccounts {
		resource := "//iam.googleapis.com/" + account.Name
		for _, key := range account.Keys {
			keyResource := "//iam.googleapis.com/" + key.Name
			if key.Exposed && !key.Disabled {
				out = append(out, newFinding("SA-001", models.SecuritySeverityCritical, models.SecurityCategoryServiceAccounts,
					"Exposed service-account key is active", keyResource,
					"An exposed private key can authenticate as the service account until the key is disabled.",
					"Disable the key immediately, rotate dependent workloads, and investigate its use.", docsSAKeys,
					"Google key metadata marks this key as exposed or compromised"))
				continue
			}
			if key.Exposed {
				out = append(out, newFinding("SA-002", models.SecuritySeverityHigh, models.SecurityCategoryServiceAccounts,
					"Previously exposed service-account key remains", keyResource,
					"The key has a permanent exposure indicator and should not be trusted again.",
					"Delete the disabled key after verifying that workloads no longer require it.", docsSAKeys,
					"Google key metadata marks this disabled key as exposed or compromised"))
				continue
			}
			if !key.Disabled {
				age := keyAgeDays(key, now)
				evidence := "active user-managed key"
				if age >= 0 {
					evidence = fmt.Sprintf("active user-managed key is %d days old", age)
				}
				out = append(out, newFinding("SA-003", models.SecuritySeverityHigh, models.SecurityCategoryServiceAccounts,
					"Active user-managed service-account key", keyResource,
					"Long-lived private keys can leak and authenticate without an interactive identity challenge.",
					"Migrate to an attached identity or Workload Identity Federation, then disable and delete the key.", docsSAKeys, evidence))
			} else {
				out = append(out, newFinding("SA-004", models.SecuritySeverityLow, models.SecurityCategoryServiceAccounts,
					"Disabled service-account key is retained", keyResource,
					"Retaining obsolete key metadata increases operational clutter and can delay credential cleanup.",
					"Delete the key after confirming it is no longer required.", docsSAKeys, "user-managed key is disabled"))
			}
		}
		if account.Disabled && bindings["serviceAccount:"+account.Email] > 0 {
			out = append(out, newFinding("SA-005", models.SecuritySeverityLow, models.SecurityCategoryServiceAccounts,
				"Disabled service account retains IAM bindings", resource,
				"Stale access grants make IAM reviews harder and could become active if the account is re-enabled.",
				"Review and remove bindings that are no longer required.", docsSAKeys,
				fmt.Sprintf("disabled account appears in %d IAM bindings", bindings["serviceAccount:"+account.Email])))
		}
	}
	return out
}

func evaluateSecrets(facts models.SecretSecurityFacts, iamFacts models.SecurityIAMPolicyFacts, now time.Time) []models.SecurityFinding {
	var out []models.SecurityFinding
	for _, secret := range facts.Secrets {
		for _, role := range publicRolesForResource(iamFacts, secret.ResourceName) {
			severity := models.SecuritySeverityHigh
			if isSecretAccessRole(role) || isPrivilegedRole(role) {
				severity = models.SecuritySeverityCritical
			}
			out = append(out, newFinding("SEC-001", severity, models.SecurityCategorySecrets,
				"Secret Manager secret is publicly accessible", secret.ResourceName,
				"A public principal can use permissions granted directly on this secret.",
				"Remove allUsers and allAuthenticatedUsers bindings from the secret.", docsSecrets,
				"public principal has "+role))
		}
		enabled := 0
		latestEnabled := time.Time{}
		for _, version := range secret.Versions {
			if version.State != "ENABLED" {
				continue
			}
			enabled++
			if created, err := time.Parse(time.RFC3339Nano, version.CreateTime); err == nil && created.After(latestEnabled) {
				latestEnabled = created
			}
		}
		if enabled == 0 {
			out = append(out, newFinding("SEC-002", models.SecuritySeverityMedium, models.SecurityCategorySecrets,
				"Secret has no enabled version", secret.ResourceName,
				"Consumers cannot retrieve a current value and might fall back to unsafe configuration handling.",
				"Create or enable the intended version and remove obsolete versions after validation.", docsSecrets,
				fmt.Sprintf("%d versions inspected; none enabled", len(secret.Versions))))
		}
		if secret.NextRotationTime != "" {
			if next, err := time.Parse(time.RFC3339Nano, secret.NextRotationTime); err == nil && next.Before(now) {
				out = append(out, newFinding("SEC-003", models.SecuritySeverityHigh, models.SecurityCategorySecrets,
					"Secret rotation is overdue", secret.ResourceName,
					"The configured rotation deadline passed without advancing the schedule.",
					"Rotate the backing credential, add a new version, and repair the rotation workflow.", docsSecrets,
					"next rotation time was "+secret.NextRotationTime))
			}
		}
		if len(secret.ReferencedBy) > 0 && secret.RotationPeriod == "" && secret.NextRotationTime == "" {
			evidence := fmt.Sprintf("referenced by %d Cloud Run services; no rotation schedule", len(secret.ReferencedBy))
			if !latestEnabled.IsZero() {
				evidence += fmt.Sprintf("; latest enabled version is %d days old", int(now.Sub(latestEnabled).Hours()/24))
			}
			out = append(out, newFinding("SEC-004", models.SecuritySeverityMedium, models.SecurityCategorySecrets,
				"Referenced secret has no rotation schedule", secret.ResourceName,
				"Long-lived application credentials increase the impact window of accidental disclosure.",
				"Define a rotation workflow and notification schedule appropriate for the credential.", docsSecrets, evidence))
		}
	}
	return out
}

func evaluatePublicServices(facts models.PublicServiceSecurityFacts, iamFacts models.SecurityIAMPolicyFacts) []models.SecurityFinding {
	var out []models.SecurityFinding
	rolesByMember := rolesByMember(iamFacts)
	for _, service := range facts.Services {
		publicRole := ""
		for _, role := range publicRolesForResource(iamFacts, service.ResourceName) {
			if isPublicInvocationRole(role) || isPrivilegedRole(role) {
				publicRole = role
				break
			}
		}
		isGKE := strings.HasPrefix(service.Kind, "gke_")
		isPublic := service.External && isGKE
		if !isGKE {
			isPublic = (service.InvokerIAMDisabled || publicRole != "") && permitsExternalIngress(service)
		}
		if !isPublic || service.IAPEnabled {
			continue
		}
		severity := models.SecuritySeverityMedium
		evidence := []string{}
		if service.InvokerIAMDisabled {
			evidence = append(evidence, "invoker IAM check is disabled")
		}
		if publicRole != "" {
			evidence = append(evidence, "public principal has "+publicRole)
		}
		if service.Ingress != "" {
			evidence = append(evidence, "ingress is "+service.Ingress)
		}
		if service.ExposureReason != "" {
			evidence = append(evidence, service.ExposureReason)
		}
		if len(service.Addresses) > 0 {
			evidence = append(evidence, "external addresses: "+strings.Join(service.Addresses, ", "))
		}
		if service.ServiceAccount != "" && memberHasPrivilegedRole(rolesByMember, "serviceAccount:"+service.ServiceAccount) {
			severity = models.SecuritySeverityHigh
			evidence = append(evidence, "runtime service account has a privileged project role")
		}
		if service.Kind == "cloud_function_gen1" && !service.TLSEnabled {
			severity = models.SecuritySeverityHigh
			evidence = append(evidence, "HTTP is accepted without mandatory HTTPS redirect")
		}
		title := "Service allows unauthenticated public invocation"
		if isGKE {
			title = "Kubernetes service is internet-facing"
		}
		finding := newFinding("PUB-001", severity, models.SecurityCategoryPublicServices,
			title, service.ResourceName,
			"An internet-accessible endpoint increases attack surface and must be an explicit design decision.",
			"Require IAM authentication or document the endpoint as intentionally public and constrain its runtime identity.", docsCloudRun, evidence...)
		finding.Region = service.Region
		out = append(out, finding)
		if isGKE && service.Kind != "gke_service" && (!service.TLSEnabled || service.PlaintextEnabled) {
			evidence := "public " + service.Kind + " has no TLS configuration"
			if service.TLSEnabled && service.PlaintextEnabled {
				evidence = "public endpoint has both TLS and plaintext listeners enabled"
			}
			out = append(out, newFinding("PUB-002", models.SecuritySeverityHigh, models.SecurityCategoryPublicServices,
				"Internet-facing Kubernetes endpoint has no TLS listener", service.ResourceName,
				"Plaintext application traffic can be observed or modified before it reaches the workload.",
				"Configure HTTPS/TLS on the Ingress or Gateway and disable or redirect plaintext traffic.", docsCloudRun,
				evidence))
		}
		if dangerousPorts := sensitiveServicePorts(service.Ports); len(dangerousPorts) > 0 {
			out = append(out, newFinding("PUB-003", models.SecuritySeverityHigh, models.SecurityCategoryPublicServices,
				"Kubernetes endpoint exposes a sensitive port", service.ResourceName,
				"Administrative and database protocols exposed directly to the internet are frequent attack targets.",
				"Use a private Service or restrict source ranges to trusted networks and authenticated access paths.", docsCloudRun,
				fmt.Sprintf("public endpoint exposes sensitive ports %v", dangerousPorts)))
		}
	}
	return out
}

func permitsExternalIngress(service models.PublicServiceSecurityFact) bool {
	ingress := strings.ToUpper(service.Ingress)
	switch service.Kind {
	case "cloud_run":
		return ingress == "" || ingress == "INGRESS_TRAFFIC_ALL" || ingress == "INGRESS_TRAFFIC_INTERNAL_LOAD_BALANCER"
	case "cloud_function_gen1":
		return ingress == "" || ingress == "ALLOW_ALL" || ingress == "ALLOW_INTERNAL_AND_GCLB"
	default:
		return service.External
	}
}

func evaluateFirewalls(facts models.FirewallSecurityFacts) []models.SecurityFinding {
	var out []models.SecurityFinding
	for _, firewall := range facts.Firewalls {
		if strings.ToUpper(firewall.Direction) != "INGRESS" || !allowsWorld(firewall) || len(firewall.Allowed) == 0 {
			continue
		}
		allTraffic := false
		dangerous := []int{}
		webOnly := true
		for _, allowed := range firewall.Allowed {
			protocol := strings.ToLower(allowed.Protocol)
			if protocol == "all" || ((protocol == "tcp" || protocol == "udp") && len(allowed.Ports) == 0) {
				allTraffic = true
				webOnly = false
			}
			for _, port := range []int{22, 23, 3389, 3306, 5432, 6379, 27017, 9200, 11211} {
				if protocol == "tcp" || protocol == "udp" {
					if portsContain(allowed.Ports, port) {
						dangerous = appendUniqueInt(dangerous, port)
					}
				}
			}
			if protocol != "tcp" || !portsSubsetOf(allowed.Ports, 80, 443) {
				webOnly = false
			}
		}
		noTarget := len(firewall.TargetTags) == 0 && len(firewall.TargetServiceAccounts) == 0
		if firewall.Disabled {
			if allTraffic || len(dangerous) > 0 {
				out = append(out, newFinding("FW-004", models.SecuritySeverityLow, models.SecurityCategoryFirewall,
					"Disabled firewall rule has dangerous configuration", firewall.ResourceName,
					"Re-enabling this rule would expose sensitive traffic from the internet.",
					"Delete the obsolete rule or narrow it before any future enablement.", docsFirewall,
					"world-accessible allow rule is currently disabled"))
			}
			continue
		}
		if deny, ok := fullyShadowingDeny(firewall, facts.Firewalls); ok {
			out = append(out, newFinding("FW-006", models.SecuritySeverityLow, models.SecurityCategoryFirewall,
				"Internet allow rule is shadowed by an effective deny", firewall.ResourceName,
				"A stale allow rule obscures the effective policy and could become active after unrelated policy changes.",
				"Remove the obsolete allow rule or document why it must remain.", docsFirewall,
				"shadowed by "+deny.ResourceName))
			continue
		}
		if allTraffic {
			evidence := []string{"allows all ports or protocols from the internet"}
			if noTarget {
				evidence = append(evidence, "applies to all instances in the network")
			}
			out = append(out, newFinding("FW-001", models.SecuritySeverityCritical, models.SecurityCategoryFirewall,
				"Firewall allows unrestricted internet ingress", firewall.ResourceName,
				"Any reachable workload can be probed on every permitted protocol and port.",
				"Restrict source ranges, protocols, ports, and target service accounts to the minimum required.", docsFirewall, evidence...))
		} else if len(dangerous) > 0 {
			evidence := []string{fmt.Sprintf("internet ingress reaches sensitive ports %v", dangerous)}
			if noTarget {
				evidence = append(evidence, "applies to all instances in the network")
			}
			out = append(out, newFinding("FW-002", models.SecuritySeverityHigh, models.SecurityCategoryFirewall,
				"Sensitive ports are exposed to the internet", firewall.ResourceName,
				"Administrative and database services are frequent targets for credential attacks and exploitation.",
				"Restrict access to trusted CIDRs or identity-aware access and target only intended workloads.", docsFirewall, evidence...))
		} else if webOnly {
			out = append(out, newFinding("FW-003", models.SecuritySeverityMedium, models.SecurityCategoryFirewall,
				"Web ingress is open to the internet", firewall.ResourceName,
				"Public web exposure should be intentional and terminate through the expected load-balancing controls.",
				"Confirm the rule is required, target only web workloads, and use an external load balancer with appropriate protections.", docsFirewall,
				"allows tcp:80/443 from the internet"))
		}
		if !firewall.LoggingEnabled {
			out = append(out, newFinding("FW-005", models.SecuritySeverityLow, models.SecurityCategoryFirewall,
				"Internet-facing firewall rule has logging disabled", firewall.ResourceName,
				"Without rule logging, access reviews and incident investigation have less network evidence.",
				"Enable firewall rule logging after reviewing expected log volume and cost.", docsFirewall,
				"logging is disabled on an active world-accessible allow rule"))
		}
	}
	return out
}

func evaluateWorkloadIdentity(facts models.WorkloadIdentitySecurityFacts) []models.SecurityFinding {
	var out []models.SecurityFinding
	for _, cluster := range facts.Clusters {
		if cluster.WorkloadPool == "" {
			out = append(out, newFinding("WIF-001", models.SecuritySeverityMedium, models.SecurityCategoryWorkloadIdentity,
				"Workload Identity Federation is disabled", cluster.ResourceName,
				"Pods might rely on node credentials or long-lived service-account keys for Google API access.",
				"Enable Workload Identity Federation and grant each workload a narrowly scoped identity.", docsWIF,
				"cluster has no workload identity pool"))
		}
		for _, pool := range cluster.NodePools {
			if cluster.WorkloadPool != "" && pool.MetadataMode != "GKE_METADATA" {
				out = append(out, newFinding("WIF-002", models.SecuritySeverityHigh, models.SecurityCategoryWorkloadIdentity,
					"Node pool does not use the GKE metadata server", cluster.ResourceName+"/nodePools/"+pool.Name,
					"Workloads on this node pool can miss the identity isolation expected from Workload Identity Federation.",
					"Update the node pool to use GKE_METADATA after validating workload compatibility.", docsWIF,
					"metadata mode is "+emptyAs(pool.MetadataMode, "unspecified")))
			}
			if pool.ServiceAccount == "" || pool.ServiceAccount == "default" || strings.Contains(pool.ServiceAccount, "-compute@developer.gserviceaccount.com") {
				out = append(out, newFinding("WIF-003", models.SecuritySeverityMedium, models.SecurityCategoryWorkloadIdentity,
					"Node pool uses the default Compute Engine service account", cluster.ResourceName+"/nodePools/"+pool.Name,
					"A shared default node identity increases the blast radius of node-level credential access.",
					"Use a dedicated least-privilege node service account and workload-specific federated identities.", docsWIF,
					"node service account is "+emptyAs(pool.ServiceAccount, "default")))
			}
		}
		for _, account := range cluster.ServiceAccounts {
			resource := cluster.ResourceName + "/kubernetes/namespaces/" + account.Namespace + "/serviceAccounts/" + account.Name
			if account.GoogleServiceAccount != "" && !account.ImpersonationVerified && gsaImpersonationCoverageComplete(facts.Coverage, account.GoogleServiceAccount) {
				out = append(out, newFinding("WIF-004", models.SecuritySeverityHigh, models.SecurityCategoryWorkloadIdentity,
					"Kubernetes service-account mapping cannot impersonate its GSA", resource,
					"The annotation claims an identity mapping, but the target Google service account does not authorize it.",
					"Grant roles/iam.workloadIdentityUser on the target GSA to this exact KSA, or remove the stale annotation.", docsWIF,
					"annotated GSA: "+account.GoogleServiceAccount, "no matching workloadIdentityUser binding"))
			}
			privileged := privilegedRoles(append(append([]string{}, account.DirectRoles...), account.MappedGSARoles...))
			if len(privileged) > 0 {
				severity := models.SecuritySeverityHigh
				if contains(privileged, "roles/owner") || contains(privileged, "roles/editor") {
					severity = models.SecuritySeverityCritical
				}
				out = append(out, newFinding("WIF-005", severity, models.SecurityCategoryWorkloadIdentity,
					"Kubernetes workload identity has a privileged Google Cloud role", resource,
					"A compromised pod can exercise broad permissions through its federated or mapped identity.",
					"Replace broad roles with resource-scoped, task-specific permissions for this workload.", docsWIF,
					"privileged roles: "+strings.Join(privileged, ", "), fmt.Sprintf("used by %d workload(s)", len(account.Workloads))))
			}
			if len(account.NamespaceRoles) > 0 {
				severity := models.SecuritySeverityMedium
				if len(privilegedRoles(account.NamespaceRoles)) > 0 {
					severity = models.SecuritySeverityHigh
				}
				out = append(out, newFinding("WIF-006", severity, models.SecurityCategoryWorkloadIdentity,
					"Google Cloud role is granted to an entire Kubernetes namespace", resource,
					"Every KSA in the namespace can receive the grant, increasing blast radius as workloads are added.",
					"Grant the role to exact KSA principals unless namespace-wide access is explicitly required.", docsWIF,
					"namespace roles: "+strings.Join(account.NamespaceRoles, ", ")))
			}
			if account.Name == "default" && len(account.Workloads) > 0 && (len(account.DirectRoles)+len(account.NamespaceRoles)+len(account.MappedGSARoles) > 0) {
				out = append(out, newFinding("WIF-007", models.SecuritySeverityHigh, models.SecurityCategoryWorkloadIdentity,
					"Default Kubernetes service account has Google Cloud access", resource,
					"Unspecified workloads in the namespace inherit the same Google Cloud identity and permissions.",
					"Create a dedicated KSA per workload and grant only the roles that workload needs.", docsWIF,
					fmt.Sprintf("default KSA is used by %d workload(s)", len(account.Workloads))))
			}
			if account.GoogleServiceAccount != "" && len(account.DirectRoles) > 0 {
				out = append(out, newFinding("WIF-008", models.SecuritySeverityMedium, models.SecurityCategoryWorkloadIdentity,
					"KSA mixes direct federation and GSA impersonation", resource,
					"Two authorization paths make effective permissions harder to review and revoke.",
					"Choose direct KSA principal grants or GSA impersonation for this workload and remove the redundant path.", docsWIF,
					"direct roles: "+strings.Join(account.DirectRoles, ", "), "annotated GSA: "+account.GoogleServiceAccount))
			}
		}
	}
	return out
}

func evaluateProviderRecommendations(facts models.SecurityRecommendationFacts, projectID string) []models.SecurityFinding {
	var out []models.SecurityFinding
	for _, recommendation := range facts.Recommendations {
		severity := models.SecuritySeverityLow
		switch recommendation.Priority {
		case "P1":
			severity = models.SecuritySeverityHigh
		case "P2":
			severity = models.SecuritySeverityMedium
		}
		resource := "//cloudresourcemanager.googleapis.com/projects/" + projectID
		if len(recommendation.TargetResources) > 0 {
			resource = recommendation.TargetResources[0]
		}
		finding := newFinding("REC-001", severity, models.SecurityCategoryIAM,
			"Google Active Assist security recommendation", resource,
			"Google observed permission usage or policy structure that can be tightened.",
			"Review the Active Assist recommendation and validate its proposed IAM change before applying it.",
			"https://cloud.google.com/policy-intelligence/docs/role-recommendations-overview",
			recommendation.Description, "subtype: "+recommendation.Subtype)
		finding.Source = "active-assist"
		finding.Confidence = "medium"
		out = append(out, finding)
	}
	return out
}

func isPublicMember(member string) bool {
	return member == "allUsers" || member == "allAuthenticatedUsers"
}

func isPublicInvocationRole(role string) bool {
	return role == "roles/run.invoker" || role == "roles/cloudfunctions.invoker"
}

func isSecretAccessRole(role string) bool {
	return role == "roles/secretmanager.secretAccessor" || role == "roles/secretmanager.admin"
}

func isPrivilegedRole(role string) bool {
	if role == "roles/owner" || role == "roles/editor" || role == "roles/iam.securityAdmin" || role == "roles/resourcemanager.projectIamAdmin" {
		return true
	}
	return strings.HasSuffix(role, ".admin") || strings.HasSuffix(role, "Admin")
}

func isGoogleManagedServiceAgent(member string) bool {
	return strings.Contains(member, "@cloudservices.gserviceaccount.com") ||
		strings.Contains(member, "@gcp-sa-") || strings.Contains(member, "service-") && strings.Contains(member, "@")
}

func isDefaultServiceAccount(member string) bool {
	return strings.Contains(member, "-compute@developer.gserviceaccount.com") || strings.Contains(member, "@appspot.gserviceaccount.com")
}

func isProjectResource(resource string) bool {
	return strings.Contains(resource, "cloudresourcemanager.googleapis.com/projects/") && !strings.Contains(strings.TrimPrefix(resource, "//cloudresourcemanager.googleapis.com/projects/"), "/")
}

func publicRolesByResource(facts models.SecurityIAMPolicyFacts) map[string][]string {
	out := make(map[string][]string)
	for _, policy := range facts.Policies {
		for _, binding := range policy.Bindings {
			for _, member := range binding.Members {
				if isPublicMember(member) {
					out[policy.Resource] = append(out[policy.Resource], binding.Role)
				}
			}
		}
	}
	return out
}

func publicRolesForResource(facts models.SecurityIAMPolicyFacts, resource string) []string {
	roles := []string{}
	for _, policy := range facts.Policies {
		if policy.Resource != resource && !isInheritedResourcePolicy(policy) {
			continue
		}
		for _, binding := range policy.Bindings {
			for _, member := range binding.Members {
				if isPublicMember(member) {
					roles = appendUniqueRoles(roles, binding.Role)
				}
			}
		}
	}
	sort.Strings(roles)
	return roles
}

func isInheritedResourcePolicy(policy models.SecurityIAMPolicyFact) bool {
	if policy.PolicyKind != "" && policy.PolicyKind != "allow" {
		return false
	}
	switch policy.ScopeType {
	case "project", "folder", "organization":
		return strings.Contains(policy.Resource, "cloudresourcemanager.googleapis.com/")
	default:
		return false
	}
}

func rolesByMember(facts models.SecurityIAMPolicyFacts) map[string][]string {
	out := make(map[string][]string)
	for _, policy := range facts.Policies {
		for _, binding := range policy.Bindings {
			for _, member := range binding.Members {
				out[member] = append(out[member], binding.Role)
			}
		}
	}
	return out
}

func serviceAccountBindingCount(facts models.SecurityIAMPolicyFacts) map[string]int {
	out := make(map[string]int)
	for _, policy := range facts.Policies {
		for _, binding := range policy.Bindings {
			for _, member := range binding.Members {
				if strings.HasPrefix(member, "serviceAccount:") {
					out[member]++
				}
			}
		}
	}
	return out
}

func memberHasPrivilegedRole(roles map[string][]string, member string) bool {
	for _, role := range roles[member] {
		if isPrivilegedRole(role) {
			return true
		}
	}
	return false
}

func keyAgeDays(key models.ServiceAccountKeyFact, now time.Time) int {
	created, err := time.Parse(time.RFC3339Nano, key.ValidAfterTime)
	if err != nil {
		return -1
	}
	return int(now.Sub(created).Hours() / 24)
}

func allowsWorld(firewall models.FirewallSecurityFact) bool {
	if len(firewall.SourceRanges) == 0 && len(firewall.SourceTags) == 0 && len(firewall.SourceServiceAccounts) == 0 {
		return true
	}
	for _, value := range firewall.SourceRanges {
		if value == "0.0.0.0/0" || value == "::/0" {
			return true
		}
		_, network, err := net.ParseCIDR(value)
		if err == nil && (network.String() == "0.0.0.0/0" || network.String() == "::/0") {
			return true
		}
	}
	return false
}

func portsContain(ranges []string, wanted int) bool {
	if len(ranges) == 0 {
		return true
	}
	for _, value := range ranges {
		parts := strings.SplitN(value, "-", 2)
		start, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		end := start
		if len(parts) == 2 {
			end, err = strconv.Atoi(parts[1])
			if err != nil {
				continue
			}
		}
		if wanted >= start && wanted <= end {
			return true
		}
	}
	return false
}

func portsSubsetOf(ranges []string, allowed ...int) bool {
	if len(ranges) == 0 {
		return false
	}
	set := make(map[int]bool, len(allowed))
	for _, port := range allowed {
		set[port] = true
	}
	for _, value := range ranges {
		if strings.Contains(value, "-") {
			return false
		}
		port, err := strconv.Atoi(value)
		if err != nil || !set[port] {
			return false
		}
	}
	return true
}

func appendUniqueInt(values []int, value int) []int {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func sensitiveServicePorts(ports []int32) []int {
	values := []int{}
	for _, port := range ports {
		switch port {
		case 22, 23, 3389, 3306, 5432, 6379, 27017, 9200, 11211:
			values = appendUniqueInt(values, int(port))
		}
	}
	sort.Ints(values)
	return values
}

func matchingDenyRuleCount(rules []models.SecurityIAMDenyRuleFact, member string) int {
	count := 0
	for _, rule := range rules {
		for _, principal := range rule.DeniedPrincipals {
			if denyPrincipalMatches(principal, member) && !denyPrincipalExcepted(rule.ExceptionPrincipals, member) {
				count++
				break
			}
		}
	}
	return count
}

func denyPrincipalMatches(principal, member string) bool {
	if principal == member {
		return true
	}
	if member == "allUsers" {
		return strings.Contains(principal, "/public:all")
	}
	if member == "allAuthenticatedUsers" {
		return strings.Contains(principal, "/public:allAuthenticatedUsers")
	}
	identity := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(member, "user:"), "group:"), "serviceAccount:")
	return identity != "" && strings.HasSuffix(principal, "/"+identity)
}

func denyPrincipalExcepted(exceptions []string, member string) bool {
	for _, exception := range exceptions {
		if denyPrincipalMatches(exception, member) {
			return true
		}
	}
	return false
}

func compactEvidence(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func fullyShadowingDeny(allow models.FirewallSecurityFact, firewalls []models.FirewallSecurityFact) (models.FirewallSecurityFact, bool) {
	for _, deny := range firewalls {
		if deny.Disabled || deny.Network != allow.Network || deny.Region != allow.Region || strings.ToUpper(deny.Direction) != "INGRESS" {
			continue
		}
		if deny.EffectiveOrder >= allow.EffectiveOrder || (strings.ToLower(deny.Action) != "deny" && len(deny.Denied) == 0) {
			continue
		}
		if !allowsWorld(deny) || !firewallTargetsCover(deny, allow) || !firewallProtocolsCover(deny.Denied, allow.Allowed) {
			continue
		}
		return deny, true
	}
	return models.FirewallSecurityFact{}, false
}

func firewallTargetsCover(deny, allow models.FirewallSecurityFact) bool {
	if len(deny.TargetTags)+len(deny.TargetServiceAccounts)+len(deny.TargetSecureTags) == 0 {
		return true
	}
	return stringSetCovers(deny.TargetTags, allow.TargetTags) &&
		stringSetCovers(deny.TargetServiceAccounts, allow.TargetServiceAccounts) &&
		stringSetCovers(deny.TargetSecureTags, allow.TargetSecureTags)
}

func stringSetCovers(superset, subset []string) bool {
	if len(subset) == 0 {
		return len(superset) == 0
	}
	for _, value := range subset {
		if !contains(superset, value) {
			return false
		}
	}
	return true
}

func firewallProtocolsCover(denied, allowed []models.FirewallProtocolFact) bool {
	if len(denied) == 0 || len(allowed) == 0 {
		return false
	}
	for _, allow := range allowed {
		covered := false
		for _, deny := range denied {
			if strings.EqualFold(deny.Protocol, "all") || (strings.EqualFold(deny.Protocol, allow.Protocol) && portRangesCover(deny.Ports, allow.Ports)) {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}

func portRangesCover(denied, allowed []string) bool {
	if len(denied) == 0 {
		return true
	}
	if len(allowed) == 0 {
		return false
	}
	for _, allowedRange := range allowed {
		start, end, ok := parsePortRange(allowedRange)
		if !ok {
			return false
		}
		covered := false
		for _, deniedRange := range denied {
			denyStart, denyEnd, ok := parsePortRange(deniedRange)
			if ok && denyStart <= start && denyEnd >= end {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}

func parsePortRange(value string) (int, int, bool) {
	parts := strings.SplitN(value, "-", 2)
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	end := start
	if len(parts) == 2 {
		end, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, false
		}
	}
	return start, end, true
}

func privilegedRoles(roles []string) []string {
	out := []string{}
	for _, role := range roles {
		if isPrivilegedRole(role) {
			out = appendUniqueRoles(out, role)
		}
	}
	sort.Strings(out)
	return out
}

func gsaImpersonationCoverageComplete(coverage []models.SecurityCoverageUnit, gsa string) bool {
	for _, unit := range coverage {
		if unit.Collector == "gsa_impersonation_policy" && strings.Contains(unit.Scope, gsa) {
			return unit.Status == "complete"
		}
	}
	return false
}

func emptyAs(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func deduplicateAndSort(findings []models.SecurityFinding) []models.SecurityFinding {
	seen := make(map[string]bool)
	out := make([]models.SecurityFinding, 0, len(findings))
	for _, finding := range findings {
		if seen[finding.ID] {
			continue
		}
		seen[finding.ID] = true
		out = append(out, finding)
	}
	sort.SliceStable(out, func(i, j int) bool {
		left, right := severityRank(out[i].Severity), severityRank(out[j].Severity)
		if left != right {
			return left < right
		}
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		if out[i].RuleID != out[j].RuleID {
			return out[i].RuleID < out[j].RuleID
		}
		return out[i].Resource < out[j].Resource
	})
	return out
}

func severityRank(severity models.SecuritySeverity) int {
	switch severity {
	case models.SecuritySeverityCritical:
		return 0
	case models.SecuritySeverityHigh:
		return 1
	case models.SecuritySeverityMedium:
		return 2
	default:
		return 3
	}
}
