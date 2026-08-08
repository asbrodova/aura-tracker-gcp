package gcp

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	crmv3 "google.golang.org/api/cloudresourcemanager/v3"
	iamv2 "google.golang.org/api/iam/v2"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

const maxSecurityHierarchyDepth = 32

func (a *gcpAdapter) collectSecurityHierarchy(ctx context.Context, projectID string) (models.SecurityHierarchyFact, []models.SecurityIAMPolicyFact, []models.SecurityIAMDenyRuleFact, []models.SecurityCoverageUnit) {
	hierarchy := models.SecurityHierarchyFact{ProjectID: projectID, Nodes: []models.SecurityHierarchyNode{}}
	var policies []models.SecurityIAMPolicyFact
	var denyRules []models.SecurityIAMDenyRuleFact
	var coverage []models.SecurityCoverageUnit
	if a.crmV3Svc == nil {
		coverage = append(coverage, securityCoverageUnit("hierarchy", "project", "projects/"+projectID, "error", 0, "Cloud Resource Manager v3 client is unavailable"))
		return hierarchy, policies, denyRules, coverage
	}

	project, err := a.crmV3Svc.Projects.Get("projects/" + projectID).Context(ctx).Do()
	if err != nil {
		coverage = append(coverage, securityCoverageUnit("hierarchy", "project", "projects/"+projectID, "error", 0, err.Error()))
		return hierarchy, policies, denyRules, coverage
	}
	hierarchy.ProjectID = project.ProjectId
	hierarchy.ProjectNumber = strings.TrimPrefix(project.Name, "projects/")
	nodes := []models.SecurityHierarchyNode{{Name: project.Name, Type: "project", DisplayName: project.DisplayName, Parent: project.Parent}}
	coverage = append(coverage, securityCoverageUnit("hierarchy", "project", project.Name, "complete", 1, ""))

	parent := project.Parent
	seen := map[string]bool{project.Name: true}
	for depth := 0; parent != "" && depth < maxSecurityHierarchyDepth; depth++ {
		if seen[parent] {
			coverage = append(coverage, securityCoverageUnit("hierarchy", scopeType(parent), parent, "error", 0, "resource hierarchy cycle detected"))
			break
		}
		seen[parent] = true
		switch {
		case strings.HasPrefix(parent, "folders/"):
			folder, err := a.crmV3Svc.Folders.Get(parent).Context(ctx).Do()
			if err != nil {
				coverage = append(coverage, securityCoverageUnit("hierarchy", "folder", parent, "error", 0, err.Error()))
				parent = ""
				continue
			}
			nodes = append(nodes, models.SecurityHierarchyNode{Name: folder.Name, Type: "folder", DisplayName: folder.DisplayName, Parent: folder.Parent})
			coverage = append(coverage, securityCoverageUnit("hierarchy", "folder", folder.Name, "complete", 1, ""))
			parent = folder.Parent
		case strings.HasPrefix(parent, "organizations/"):
			organization, err := a.crmV3Svc.Organizations.Get(parent).Context(ctx).Do()
			if err != nil {
				coverage = append(coverage, securityCoverageUnit("hierarchy", "organization", parent, "error", 0, err.Error()))
				parent = ""
				continue
			}
			nodes = append(nodes, models.SecurityHierarchyNode{Name: organization.Name, Type: "organization", DisplayName: organization.DisplayName})
			hierarchy.Organization = organization.Name
			coverage = append(coverage, securityCoverageUnit("hierarchy", "organization", organization.Name, "complete", 1, ""))
			parent = ""
		default:
			coverage = append(coverage, securityCoverageUnit("hierarchy", "unknown", parent, "error", 0, "unsupported hierarchy parent"))
			parent = ""
		}
	}
	if parent != "" {
		coverage = append(coverage, securityCoverageUnit("hierarchy", scopeType(parent), parent, "truncated", 0, "resource hierarchy depth limit reached"))
	}

	// Convert leaf-to-root discovery order into root-to-project report order.
	for left, right := 0, len(nodes)-1; left < right; left, right = left+1, right-1 {
		nodes[left], nodes[right] = nodes[right], nodes[left]
	}
	for i := range nodes {
		nodes[i].Depth = i
	}
	hierarchy.Nodes = nodes

	for _, node := range nodes {
		if node.Type == "project" {
			continue // Project and resource policies come from Cloud Asset below.
		}
		policy, err := a.getAncestorIAMPolicy(ctx, node)
		if err != nil {
			coverage = append(coverage, securityCoverageUnit("iam_allow", node.Type, node.Name, "error", 0, err.Error()))
		} else {
			policies = append(policies, policy)
			coverage = append(coverage, securityCoverageUnit("iam_allow", node.Type, node.Name, "complete", len(policy.Bindings), ""))
		}
	}

	if a.iamV2Svc == nil {
		coverage = append(coverage, securityCoverageUnit("iam_deny", "project", project.Name, "error", 0, "IAM v2 client is unavailable"))
		return hierarchy, policies, denyRules, coverage
	}
	for _, node := range nodes {
		rules, err := a.listSecurityDenyRules(ctx, node, node.Type != "project")
		if err != nil {
			coverage = append(coverage, securityCoverageUnit("iam_deny", node.Type, node.Name, "error", 0, err.Error()))
			continue
		}
		denyRules = append(denyRules, rules...)
		coverage = append(coverage, securityCoverageUnit("iam_deny", node.Type, node.Name, "complete", len(rules), ""))
	}
	return hierarchy, policies, denyRules, coverage
}

func (a *gcpAdapter) getAncestorIAMPolicy(ctx context.Context, node models.SecurityHierarchyNode) (models.SecurityIAMPolicyFact, error) {
	request := &crmv3.GetIamPolicyRequest{Options: &crmv3.GetPolicyOptions{RequestedPolicyVersion: 3}}
	var policy *crmv3.Policy
	var err error
	switch node.Type {
	case "folder":
		policy, err = a.crmV3Svc.Folders.GetIamPolicy(node.Name, request).Context(ctx).Do()
	case "organization":
		policy, err = a.crmV3Svc.Organizations.GetIamPolicy(node.Name, request).Context(ctx).Do()
	default:
		return models.SecurityIAMPolicyFact{}, fmt.Errorf("unsupported ancestor scope %q", node.Type)
	}
	if err != nil {
		return models.SecurityIAMPolicyFact{}, err
	}
	fact := models.SecurityIAMPolicyFact{
		Resource: "//cloudresourcemanager.googleapis.com/" + node.Name, AssetType: "cloudresourcemanager.googleapis.com/" + titleScope(node.Type),
		OriginScope: node.Name, ScopeType: node.Type, PolicyKind: "allow", Inherited: true,
		Bindings: []models.SecurityIAMBindingFact{},
	}
	for _, binding := range policy.Bindings {
		out := models.SecurityIAMBindingFact{Role: binding.Role, Members: append([]string(nil), binding.Members...)}
		if binding.Condition != nil {
			out.Condition = &models.SecurityIAMCondition{Title: binding.Condition.Title, Description: binding.Condition.Description, Expression: binding.Condition.Expression}
		}
		sort.Strings(out.Members)
		fact.Bindings = append(fact.Bindings, out)
	}
	return fact, nil
}

func titleScope(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func (a *gcpAdapter) listSecurityDenyRules(ctx context.Context, node models.SecurityHierarchyNode, inherited bool) ([]models.SecurityIAMDenyRuleFact, error) {
	attachment := "cloudresourcemanager.googleapis.com/" + node.Name
	parent := "policies/" + url.PathEscape(attachment) + "/denypolicies"
	var rules []models.SecurityIAMDenyRuleFact
	err := a.iamV2Svc.Policies.ListPolicies(parent).PageSize(100).Pages(ctx, func(page *iamv2.GoogleIamV2ListPoliciesResponse) error {
		for _, metadata := range page.Policies {
			policy, err := a.iamV2Svc.Policies.Get(metadata.Name).Context(ctx).Do()
			if err != nil {
				return err
			}
			for _, rule := range policy.Rules {
				if rule.DenyRule == nil {
					continue
				}
				fact := models.SecurityIAMDenyRuleFact{
					PolicyName: policy.Name, OriginScope: node.Name, ScopeType: node.Type, Inherited: inherited,
					Description:          rule.Description,
					DeniedPrincipals:     append([]string(nil), rule.DenyRule.DeniedPrincipals...),
					ExceptionPrincipals:  append([]string(nil), rule.DenyRule.ExceptionPrincipals...),
					DeniedPermissions:    append([]string(nil), rule.DenyRule.DeniedPermissions...),
					ExceptionPermissions: append([]string(nil), rule.DenyRule.ExceptionPermissions...),
				}
				if rule.DenyRule.DenialCondition != nil {
					fact.Condition = &models.SecurityIAMCondition{
						Title: rule.DenyRule.DenialCondition.Title, Description: rule.DenyRule.DenialCondition.Description,
						Expression: rule.DenyRule.DenialCondition.Expression,
					}
				}
				sort.Strings(fact.DeniedPrincipals)
				sort.Strings(fact.ExceptionPrincipals)
				sort.Strings(fact.DeniedPermissions)
				sort.Strings(fact.ExceptionPermissions)
				rules = append(rules, fact)
			}
		}
		return nil
	})
	return rules, err
}

func securityCoverageUnit(collector, scopeType, scope, status string, items int, message string) models.SecurityCoverageUnit {
	return models.SecurityCoverageUnit{Collector: collector, ScopeType: scopeType, Scope: scope, Status: status, ItemsScanned: items, Message: message}
}

func scopeType(resource string) string {
	if index := strings.IndexByte(resource, '/'); index > 0 {
		return strings.TrimSuffix(resource[:index], "s")
	}
	return "unknown"
}
