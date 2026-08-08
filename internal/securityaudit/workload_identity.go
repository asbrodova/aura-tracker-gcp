package securityaudit

import (
	"sort"
	"strings"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func correlateWorkloadIdentity(workloadFacts *models.WorkloadIdentitySecurityFacts, iamFacts models.SecurityIAMPolicyFacts) {
	if workloadFacts == nil {
		return
	}
	roles := rolesByMember(iamFacts)
	for clusterIndex := range workloadFacts.Clusters {
		cluster := &workloadFacts.Clusters[clusterIndex]
		for accountIndex := range cluster.ServiceAccounts {
			account := &cluster.ServiceAccounts[accountIndex]
			directSuffix := "/subject/ns/" + account.Namespace + "/sa/" + account.Name
			namespaceSuffix := "/namespace/" + account.Namespace
			for member, grantedRoles := range roles {
				if !identityMemberUsesPool(member, cluster.WorkloadPool) {
					continue
				}
				switch {
				case strings.HasPrefix(member, "principal://") && strings.HasSuffix(member, directSuffix):
					account.DirectRoles = appendUniqueRoles(account.DirectRoles, grantedRoles...)
				case strings.HasPrefix(member, "principalSet://") && (strings.HasSuffix(member, namespaceSuffix) || strings.HasSuffix(member, "/attribute.namespace/"+account.Namespace)):
					account.NamespaceRoles = appendUniqueRoles(account.NamespaceRoles, grantedRoles...)
				}
			}

			if account.GoogleServiceAccount != "" {
				account.MappedGSARoles = appendUniqueRoles(account.MappedGSARoles, roles["serviceAccount:"+account.GoogleServiceAccount]...)
				legacyMember := "serviceAccount:" + cluster.WorkloadPool + "[" + account.Namespace + "/" + account.Name + "]"
				if serviceAccountAllowsImpersonation(iamFacts, account.GoogleServiceAccount, legacyMember) {
					account.ImpersonationVerified = true
				}
			}
			sort.Strings(account.DirectRoles)
			sort.Strings(account.NamespaceRoles)
			sort.Strings(account.MappedGSARoles)
		}
	}
}

func identityMemberUsesPool(member, pool string) bool {
	return pool != "" && strings.Contains(member, "/workloadIdentityPools/"+pool+"/")
}

func serviceAccountAllowsImpersonation(facts models.SecurityIAMPolicyFacts, gsa, member string) bool {
	for _, policy := range facts.Policies {
		if !strings.Contains(policy.Resource, "/serviceAccounts/"+gsa) {
			continue
		}
		for _, binding := range policy.Bindings {
			if binding.Role == "roles/iam.workloadIdentityUser" && contains(binding.Members, member) {
				return true
			}
		}
	}
	return false
}

func appendUniqueRoles(values []string, additions ...string) []string {
	seen := make(map[string]bool, len(values)+len(additions))
	for _, value := range values {
		seen[value] = true
	}
	for _, value := range additions {
		if value == "" || seen[value] || value == "roles/iam.workloadIdentityUser" {
			continue
		}
		seen[value] = true
		values = append(values, value)
	}
	return values
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
