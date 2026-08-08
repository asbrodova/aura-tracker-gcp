package gcp

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"google.golang.org/api/compute/v1"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (a *gcpAdapter) collectEffectiveFirewallSecurityFacts(ctx context.Context, projectID string) (models.FirewallSecurityFacts, error) {
	result := models.FirewallSecurityFacts{Firewalls: []models.FirewallSecurityFact{}, Coverage: []models.SecurityCoverageUnit{}}
	networks, err := a.computeSvc.Networks.List(projectID).Context(ctx).Do()
	if err != nil {
		return result, wrapGCPError("security.ListFirewallSecurityFacts.Networks", err)
	}
	result.Coverage = append(result.Coverage, securityCoverageUnit("firewall_networks", "project", "projects/"+projectID, "complete", len(networks.Items), ""))
	seen := make(map[string]bool)
	for _, network := range networks.Items {
		if err := a.rateWait(ctx, "security.GetEffectiveFirewalls"); err != nil {
			return result, err
		}
		response, err := a.computeSvc.Networks.GetEffectiveFirewalls(projectID, network.Name).Context(ctx).Do()
		if err != nil {
			result.Coverage = append(result.Coverage, securityCoverageUnit("firewall_effective", "network", network.Name, "error", 0, err.Error()))
			continue
		}
		count := 0
		for order, firewall := range response.Firewalls {
			// Classic VPC rules are evaluated after the ordered policy layers
			// returned in FirewallPolicys.
			fact := classicFirewallFact(projectID, network.Name, firewall, 1_000_000_000+order)
			appendFirewallFact(&result.Firewalls, seen, fact)
			count++
		}
		for policyOrder, policy := range response.FirewallPolicys {
			for ruleOrder, rule := range policy.Rules {
				fact := policyFirewallFact(projectID, network.Name, "", policy.Name, policy.ShortName, policy.Type, policy.Priority, rule, policyOrder*100000+ruleOrder)
				appendFirewallFact(&result.Firewalls, seen, fact)
				count++
			}
		}
		result.Coverage = append(result.Coverage, securityCoverageUnit("firewall_effective", "network", network.Name, "complete", count, ""))
	}

	// Regional network firewall policies are not included in the global network
	// effective-firewall response. Discover the regions with configured policies,
	// then query only their associated networks.
	regions, err := a.computeSvc.Regions.List(projectID).Context(ctx).Do()
	if err != nil {
		result.Coverage = append(result.Coverage, securityCoverageUnit("firewall_regions", "project", "projects/"+projectID, "error", 0, err.Error()))
	} else {
		result.Coverage = append(result.Coverage, securityCoverageUnit("firewall_regions", "project", "projects/"+projectID, "complete", len(regions.Items), ""))
		for _, region := range regions.Items {
			if err := a.collectRegionalEffectiveFirewalls(ctx, projectID, region.Name, &result, seen); err != nil {
				result.Coverage = append(result.Coverage, securityCoverageUnit("firewall_regional", "region", region.Name, "error", 0, err.Error()))
			}
		}
	}

	// Effective endpoints omit disabled classic rules. Retain those as hygiene
	// evidence without using active rules from this secondary list.
	err = a.computeSvc.Firewalls.List(projectID).Context(ctx).Pages(ctx, func(page *compute.FirewallList) error {
		for _, firewall := range page.Items {
			if !firewall.Disabled {
				continue
			}
			appendFirewallFact(&result.Firewalls, seen, classicFirewallFact(projectID, path.Base(firewall.Network), firewall, len(result.Firewalls)))
		}
		return nil
	})
	if err != nil {
		result.Coverage = append(result.Coverage, securityCoverageUnit("firewall_disabled_hygiene", "project", "projects/"+projectID, "error", 0, err.Error()))
	} else {
		result.Coverage = append(result.Coverage, securityCoverageUnit("firewall_disabled_hygiene", "project", "projects/"+projectID, "complete", 0, ""))
	}
	sort.Slice(result.Firewalls, func(i, j int) bool {
		left, right := result.Firewalls[i], result.Firewalls[j]
		if left.Network != right.Network {
			return left.Network < right.Network
		}
		if left.Region != right.Region {
			return left.Region < right.Region
		}
		if left.EffectiveOrder != right.EffectiveOrder {
			return left.EffectiveOrder < right.EffectiveOrder
		}
		return left.ResourceName < right.ResourceName
	})
	return result, nil
}

func (a *gcpAdapter) collectRegionalEffectiveFirewalls(ctx context.Context, projectID, region string, result *models.FirewallSecurityFacts, seen map[string]bool) error {
	if err := a.rateWait(ctx, "security.ListRegionalFirewallPolicies"); err != nil {
		return err
	}
	policies, err := a.computeSvc.RegionNetworkFirewallPolicies.List(projectID, region).Context(ctx).Do()
	if err != nil {
		return err
	}
	requested := make(map[string]bool)
	for _, policy := range policies.Items {
		for _, association := range policy.Associations {
			network := path.Base(association.AttachmentTarget)
			if network == "" || requested[network] {
				continue
			}
			requested[network] = true
			if err := a.rateWait(ctx, "security.GetRegionalEffectiveFirewalls"); err != nil {
				return err
			}
			response, err := a.computeSvc.RegionNetworkFirewallPolicies.GetEffectiveFirewalls(projectID, region, network).Context(ctx).Do()
			if err != nil {
				result.Coverage = append(result.Coverage, securityCoverageUnit("firewall_regional_effective", "network_region", network+"/"+region, "error", 0, err.Error()))
				continue
			}
			count := 0
			for policyOrder, effective := range response.FirewallPolicys {
				if effective.Type != "NETWORK_REGIONAL" && effective.Type != "SYSTEM_REGIONAL" {
					continue
				}
				for ruleOrder, rule := range effective.Rules {
					fact := policyFirewallFact(projectID, network, region, effective.Name, "", effective.Type, effective.Priority, rule, policyOrder*100000+ruleOrder)
					appendFirewallFact(&result.Firewalls, seen, fact)
					count++
				}
			}
			result.Coverage = append(result.Coverage, securityCoverageUnit("firewall_regional_effective", "network_region", network+"/"+region, "complete", count, ""))
		}
	}
	result.Coverage = append(result.Coverage, securityCoverageUnit("firewall_regional", "region", region, "complete", len(requested), ""))
	return nil
}

func classicFirewallFact(projectID, network string, firewall *compute.Firewall, order int) models.FirewallSecurityFact {
	fact := models.FirewallSecurityFact{
		Name: firewall.Name, ResourceName: fmt.Sprintf("//compute.googleapis.com/projects/%s/global/firewalls/%s", projectID, firewall.Name),
		Network: network, Direction: firewall.Direction, Priority: firewall.Priority,
		SourceRanges: append([]string(nil), firewall.SourceRanges...), DestinationRanges: append([]string(nil), firewall.DestinationRanges...),
		SourceTags: append([]string(nil), firewall.SourceTags...), TargetTags: append([]string(nil), firewall.TargetTags...),
		SourceServiceAccounts: append([]string(nil), firewall.SourceServiceAccounts...),
		TargetServiceAccounts: append([]string(nil), firewall.TargetServiceAccounts...), Disabled: firewall.Disabled,
		Layer: "classic", PolicyType: "CLASSIC", EffectiveOrder: order,
	}
	if firewall.LogConfig != nil {
		fact.LoggingEnabled = firewall.LogConfig.Enable
	}
	for _, allowed := range firewall.Allowed {
		fact.Allowed = append(fact.Allowed, models.FirewallProtocolFact{Protocol: allowed.IPProtocol, Ports: append([]string(nil), allowed.Ports...)})
	}
	for _, denied := range firewall.Denied {
		fact.Denied = append(fact.Denied, models.FirewallProtocolFact{Protocol: denied.IPProtocol, Ports: append([]string(nil), denied.Ports...)})
	}
	if len(fact.Allowed) > 0 {
		fact.Action = "allow"
	} else if len(fact.Denied) > 0 {
		fact.Action = "deny"
	}
	return fact
}

func policyFirewallFact(projectID, network, region, policyID, shortName, policyType string, associationPriority int64, rule *compute.FirewallPolicyRule, order int) models.FirewallSecurityFact {
	name := rule.RuleName
	if name == "" {
		name = fmt.Sprintf("%s-%d", policyID, rule.Priority)
	}
	fact := models.FirewallSecurityFact{
		Name: name, ResourceName: fmt.Sprintf("//compute.googleapis.com/projects/%s/firewallPolicies/%s/rules/%d", projectID, policyID, rule.Priority),
		Network: network, Region: region, Direction: rule.Direction, Priority: rule.Priority,
		TargetServiceAccounts: append([]string(nil), rule.TargetServiceAccounts...), Disabled: rule.Disabled,
		Layer: firewallLayer(policyType), PolicyName: firstNonEmpty(shortName, policyID), PolicyType: policyType,
		Action: strings.ToLower(rule.Action), LoggingEnabled: rule.EnableLogging,
		AssociationPriority: associationPriority, EffectiveOrder: order,
	}
	if rule.Match != nil {
		fact.SourceRanges = append([]string(nil), rule.Match.SrcIpRanges...)
		fact.DestinationRanges = append([]string(nil), rule.Match.DestIpRanges...)
		for _, layer4 := range rule.Match.Layer4Configs {
			protocol := models.FirewallProtocolFact{Protocol: layer4.IpProtocol, Ports: append([]string(nil), layer4.Ports...)}
			if fact.Action == "allow" {
				fact.Allowed = append(fact.Allowed, protocol)
			} else if fact.Action == "deny" {
				fact.Denied = append(fact.Denied, protocol)
			}
		}
		for _, tag := range rule.Match.SrcSecureTags {
			fact.SourceSecureTags = append(fact.SourceSecureTags, tag.Name+":"+tag.State)
		}
	}
	for _, tag := range rule.TargetSecureTags {
		fact.TargetSecureTags = append(fact.TargetSecureTags, tag.Name+":"+tag.State)
	}
	return fact
}

func appendFirewallFact(out *[]models.FirewallSecurityFact, seen map[string]bool, fact models.FirewallSecurityFact) {
	key := fmt.Sprintf("%s|%s|%s|%s|%d|%s", fact.Network, fact.Region, fact.PolicyType, fact.PolicyName, fact.Priority, fact.ResourceName)
	if seen[key] {
		return
	}
	seen[key] = true
	*out = append(*out, fact)
}

func firewallLayer(policyType string) string {
	switch policyType {
	case "HIERARCHY":
		return "hierarchical"
	case "NETWORK":
		return "global_network"
	case "NETWORK_REGIONAL":
		return "regional_network"
	case "SYSTEM", "SYSTEM_GLOBAL", "SYSTEM_REGIONAL":
		return "system"
	default:
		return "policy"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
