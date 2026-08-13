package gcp

import (
	"testing"

	"google.golang.org/api/compute/v1"
)

func TestPolicyFirewallFactPreservesSourceSelectors(t *testing.T) {
	rule := &compute.FirewallPolicyRule{
		Direction: "INGRESS", Action: "allow", Priority: 100,
		Match: &compute.FirewallPolicyRuleMatcher{
			SrcAddressGroups:       []string{"addressGroups/trusted"},
			SrcFqdns:               []string{"trusted.example"},
			SrcNetworks:            []string{"networks/trusted"},
			SrcRegionCodes:         []string{"ID"},
			SrcThreatIntelligences: []string{"iplist-known-malicious-ips"},
			SrcNetworkContext:      "INTRA_VPC",
			SrcNetworkType:         "VPC_NETWORKS",
			SrcSecureTags: []*compute.FirewallPolicyRuleSecureTag{{
				Name: "tagValues/1", State: "EFFECTIVE",
			}},
			Layer4Configs: []*compute.FirewallPolicyRuleMatcherLayer4Config{{IpProtocol: "tcp", Ports: []string{"22"}}},
		},
		TargetSecureTags: []*compute.FirewallPolicyRuleSecureTag{{Name: "tagValues/2", State: "INEFFECTIVE"}},
	}

	fact := policyFirewallFact("project", "network", "", "policy", "", "NETWORK", 0, rule, 1)
	if len(fact.SourceAddressGroups) != 1 || len(fact.SourceFQDNs) != 1 || len(fact.SourceNetworks) != 1 ||
		len(fact.SourceRegionCodes) != 1 || len(fact.SourceThreatIntel) != 1 || fact.SourceNetworkContext != "INTRA_VPC" ||
		fact.SourceNetworkType != "VPC_NETWORKS" || len(fact.SourceSecureTags) != 1 || fact.SourceSecureTags[0] != "tagValues/1:EFFECTIVE" ||
		len(fact.TargetSecureTags) != 1 || fact.TargetSecureTags[0] != "tagValues/2:INEFFECTIVE" {
		t.Fatalf("source selectors were not preserved: %+v", fact)
	}
}
