package models

// SecuritySeverity is the user-facing priority of a security finding.
type SecuritySeverity string

const (
	SecuritySeverityCritical SecuritySeverity = "critical"
	SecuritySeverityHigh     SecuritySeverity = "high"
	SecuritySeverityMedium   SecuritySeverity = "medium"
	SecuritySeverityLow      SecuritySeverity = "low"
)

// SecurityCategory identifies one independently scored audit domain.
type SecurityCategory string

const (
	SecurityCategoryIAM              SecurityCategory = "iam"
	SecurityCategoryServiceAccounts  SecurityCategory = "service_accounts"
	SecurityCategorySecrets          SecurityCategory = "secret_manager"
	SecurityCategoryPublicServices   SecurityCategory = "public_services"
	SecurityCategoryFirewall         SecurityCategory = "firewall"
	SecurityCategoryWorkloadIdentity SecurityCategory = "workload_identity"
	SecurityCategoryRecommendations  SecurityCategory = "recommendations"
)

// SecurityFactsRequest selects the project whose read-only security facts are collected.
type SecurityFactsRequest struct {
	ProjectID string `json:"project_id"`
}

// SecurityAuditRequest is the input for gcp_project_security_audit.
type SecurityAuditRequest struct {
	ProjectID string `json:"project_id"`
	// Refresh bypasses the short-lived in-process report cache.
	Refresh bool `json:"refresh,omitempty"`
}

// SecurityCoverageUnit describes one independently verifiable scope. A
// category is complete only when all of its required units are complete.
type SecurityCoverageUnit struct {
	Collector    string `json:"collector"`
	ScopeType    string `json:"scope_type"`
	Scope        string `json:"scope"`
	Status       string `json:"status"`
	ItemsScanned int    `json:"items_scanned"`
	Message      string `json:"message,omitempty"`
}

type SecurityHierarchyNode struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name,omitempty"`
	Parent      string `json:"parent,omitempty"`
	Depth       int    `json:"depth"`
}

type SecurityHierarchyFact struct {
	ProjectID     string                  `json:"project_id"`
	ProjectNumber string                  `json:"project_number,omitempty"`
	Organization  string                  `json:"organization,omitempty"`
	Nodes         []SecurityHierarchyNode `json:"nodes"`
}

// SecurityIAMCondition preserves an IAM condition without attempting to evaluate
// request-dependent CEL expressions during an offline posture audit.
type SecurityIAMCondition struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Expression  string `json:"expression,omitempty"`
}

type SecurityIAMBindingFact struct {
	Role      string                `json:"role"`
	Members   []string              `json:"members"`
	Condition *SecurityIAMCondition `json:"condition,omitempty"`
}

type SecurityIAMPolicyFact struct {
	Resource    string                   `json:"resource"`
	AssetType   string                   `json:"asset_type,omitempty"`
	OriginScope string                   `json:"origin_scope,omitempty"`
	ScopeType   string                   `json:"scope_type,omitempty"`
	PolicyKind  string                   `json:"policy_kind,omitempty"`
	Inherited   bool                     `json:"inherited,omitempty"`
	Bindings    []SecurityIAMBindingFact `json:"bindings"`
}

type SecurityIAMDenyRuleFact struct {
	PolicyName           string                `json:"policy_name"`
	OriginScope          string                `json:"origin_scope"`
	ScopeType            string                `json:"scope_type"`
	Inherited            bool                  `json:"inherited,omitempty"`
	Description          string                `json:"description,omitempty"`
	DeniedPrincipals     []string              `json:"denied_principals"`
	ExceptionPrincipals  []string              `json:"exception_principals,omitempty"`
	DeniedPermissions    []string              `json:"denied_permissions"`
	ExceptionPermissions []string              `json:"exception_permissions,omitempty"`
	Condition            *SecurityIAMCondition `json:"condition,omitempty"`
}

type SecurityIAMPolicyFacts struct {
	Hierarchy SecurityHierarchyFact     `json:"hierarchy"`
	Policies  []SecurityIAMPolicyFact   `json:"policies"`
	DenyRules []SecurityIAMDenyRuleFact `json:"deny_rules,omitempty"`
	Coverage  []SecurityCoverageUnit    `json:"coverage,omitempty"`
	Truncated bool                      `json:"truncated,omitempty"`
}

type ServiceAccountKeyFact struct {
	Name            string `json:"name"`
	ID              string `json:"id"`
	Origin          string `json:"origin,omitempty"`
	KeyType         string `json:"key_type,omitempty"`
	ValidAfterTime  string `json:"valid_after_time,omitempty"`
	ValidBeforeTime string `json:"valid_before_time,omitempty"`
	Disabled        bool   `json:"disabled,omitempty"`
	DisableReason   string `json:"disable_reason,omitempty"`
	Exposed         bool   `json:"exposed,omitempty"`
}

type ServiceAccountSecurityFact struct {
	Name        string                  `json:"name"`
	Email       string                  `json:"email"`
	DisplayName string                  `json:"display_name,omitempty"`
	Description string                  `json:"description,omitempty"`
	Disabled    bool                    `json:"disabled,omitempty"`
	Keys        []ServiceAccountKeyFact `json:"keys"`
}

type ServiceAccountSecurityFacts struct {
	ServiceAccounts []ServiceAccountSecurityFact `json:"service_accounts"`
}

type SecretVersionSecurityFact struct {
	Name       string `json:"name"`
	CreateTime string `json:"create_time,omitempty"`
	State      string `json:"state,omitempty"`
}

type SecretSecurityFact struct {
	Name             string                      `json:"name"`
	ResourceName     string                      `json:"resource_name"`
	CreateTime       string                      `json:"create_time,omitempty"`
	Replication      string                      `json:"replication,omitempty"`
	RotationPeriod   string                      `json:"rotation_period,omitempty"`
	NextRotationTime string                      `json:"next_rotation_time,omitempty"`
	ExpireTime       string                      `json:"expire_time,omitempty"`
	TopicCount       int                         `json:"topic_count"`
	Versions         []SecretVersionSecurityFact `json:"versions"`
	ReferencedBy     []string                    `json:"referenced_by,omitempty"`
}

type SecretSecurityFacts struct {
	Secrets  []SecretSecurityFact `json:"secrets"`
	Warnings []string             `json:"warnings,omitempty"`
}

type PublicServiceSecurityFact struct {
	ResourceName       string   `json:"resource_name"`
	Name               string   `json:"name"`
	Kind               string   `json:"kind"`
	Region             string   `json:"region,omitempty"`
	Ingress            string   `json:"ingress,omitempty"`
	InvokerIAMDisabled bool     `json:"invoker_iam_disabled,omitempty"`
	IAPEnabled         bool     `json:"iap_enabled,omitempty"`
	DefaultURIDisabled bool     `json:"default_uri_disabled,omitempty"`
	ServiceAccount     string   `json:"service_account,omitempty"`
	SecretReferences   []string `json:"secret_references,omitempty"`
	External           bool     `json:"external,omitempty"`
	TLSEnabled         bool     `json:"tls_enabled,omitempty"`
	PlaintextEnabled   bool     `json:"plaintext_enabled,omitempty"`
	ClusterName        string   `json:"cluster_name,omitempty"`
	Namespace          string   `json:"namespace,omitempty"`
	Addresses          []string `json:"addresses,omitempty"`
	Hosts              []string `json:"hosts,omitempty"`
	Ports              []int32  `json:"ports,omitempty"`
	SourceRanges       []string `json:"source_ranges,omitempty"`
	BackendServices    []string `json:"backend_services,omitempty"`
	WorkloadIdentities []string `json:"workload_identities,omitempty"`
	ExposureReason     string   `json:"exposure_reason,omitempty"`
}

type PublicServiceSecurityFacts struct {
	Services []PublicServiceSecurityFact `json:"services"`
	Coverage []SecurityCoverageUnit      `json:"coverage,omitempty"`
	Warnings []string                    `json:"warnings,omitempty"`
}

type FirewallProtocolFact struct {
	Protocol string   `json:"protocol"`
	Ports    []string `json:"ports,omitempty"`
}

type FirewallSecurityFact struct {
	Name                  string                 `json:"name"`
	ResourceName          string                 `json:"resource_name"`
	Network               string                 `json:"network,omitempty"`
	Direction             string                 `json:"direction"`
	Priority              int64                  `json:"priority"`
	SourceRanges          []string               `json:"source_ranges,omitempty"`
	DestinationRanges     []string               `json:"destination_ranges,omitempty"`
	SourceTags            []string               `json:"source_tags,omitempty"`
	TargetTags            []string               `json:"target_tags,omitempty"`
	SourceServiceAccounts []string               `json:"source_service_accounts,omitempty"`
	TargetServiceAccounts []string               `json:"target_service_accounts,omitempty"`
	Allowed               []FirewallProtocolFact `json:"allowed,omitempty"`
	Denied                []FirewallProtocolFact `json:"denied,omitempty"`
	LoggingEnabled        bool                   `json:"logging_enabled"`
	Disabled              bool                   `json:"disabled"`
	Layer                 string                 `json:"layer,omitempty"`
	PolicyName            string                 `json:"policy_name,omitempty"`
	PolicyType            string                 `json:"policy_type,omitempty"`
	Action                string                 `json:"action,omitempty"`
	Region                string                 `json:"region,omitempty"`
	AssociationPriority   int64                  `json:"association_priority,omitempty"`
	EffectiveOrder        int                    `json:"effective_order,omitempty"`
	TargetSecureTags      []string               `json:"target_secure_tags,omitempty"`
	SourceSecureTags      []string               `json:"source_secure_tags,omitempty"`
	SourceAddressGroups   []string               `json:"source_address_groups,omitempty"`
	SourceFQDNs           []string               `json:"source_fqdns,omitempty"`
	SourceNetworks        []string               `json:"source_networks,omitempty"`
	SourceRegionCodes     []string               `json:"source_region_codes,omitempty"`
	SourceThreatIntel     []string               `json:"source_threat_intelligence,omitempty"`
	SourceNetworkContext  string                 `json:"source_network_context,omitempty"`
	SourceNetworkType     string                 `json:"source_network_type,omitempty"`
}

type FirewallSecurityFacts struct {
	Firewalls []FirewallSecurityFact `json:"firewalls"`
	Coverage  []SecurityCoverageUnit `json:"coverage,omitempty"`
}

type NodePoolIdentityFact struct {
	Name           string `json:"name"`
	ServiceAccount string `json:"service_account,omitempty"`
	MetadataMode   string `json:"metadata_mode,omitempty"`
}

type WorkloadIdentitySecurityFact struct {
	ResourceName    string                           `json:"resource_name"`
	ClusterName     string                           `json:"cluster_name"`
	Location        string                           `json:"location"`
	WorkloadPool    string                           `json:"workload_pool,omitempty"`
	PrivateNodes    bool                             `json:"private_nodes,omitempty"`
	NodePools       []NodePoolIdentityFact           `json:"node_pools"`
	AccessMode      string                           `json:"access_mode,omitempty"`
	ServiceAccounts []KubernetesServiceAccountFact   `json:"service_accounts,omitempty"`
	Workloads       []KubernetesWorkloadIdentityFact `json:"workloads,omitempty"`
}

type KubernetesServiceAccountFact struct {
	Namespace             string   `json:"namespace"`
	Name                  string   `json:"name"`
	GoogleServiceAccount  string   `json:"google_service_account,omitempty"`
	AutomountToken        *bool    `json:"automount_service_account_token,omitempty"`
	Workloads             []string `json:"workloads,omitempty"`
	DirectRoles           []string `json:"direct_roles,omitempty"`
	NamespaceRoles        []string `json:"namespace_roles,omitempty"`
	MappedGSARoles        []string `json:"mapped_gsa_roles,omitempty"`
	ImpersonationVerified bool     `json:"impersonation_verified,omitempty"`
}

type KubernetesWorkloadIdentityFact struct {
	Namespace      string            `json:"namespace"`
	Name           string            `json:"name"`
	Kind           string            `json:"kind"`
	ServiceAccount string            `json:"service_account"`
	AutomountToken *bool             `json:"automount_service_account_token,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
}

type WorkloadIdentitySecurityFacts struct {
	Clusters []WorkloadIdentitySecurityFact `json:"clusters"`
	Coverage []SecurityCoverageUnit         `json:"coverage,omitempty"`
	Warnings []string                       `json:"warnings,omitempty"`
}

type SecurityRecommendationFact struct {
	Name            string   `json:"name"`
	RecommenderID   string   `json:"recommender_id"`
	Subtype         string   `json:"subtype,omitempty"`
	Description     string   `json:"description"`
	Priority        string   `json:"priority,omitempty"`
	LastRefreshTime string   `json:"last_refresh_time,omitempty"`
	TargetResources []string `json:"target_resources,omitempty"`
}

type SecurityRecommendationFacts struct {
	Recommendations []SecurityRecommendationFact `json:"recommendations"`
	Enabled         bool                         `json:"enabled"`
	Truncated       bool                         `json:"truncated,omitempty"`
	Warnings        []string                     `json:"warnings,omitempty"`
}

type SecurityFinding struct {
	ID             string           `json:"id"`
	RuleID         string           `json:"rule_id"`
	Severity       SecuritySeverity `json:"severity"`
	Category       SecurityCategory `json:"category"`
	Title          string           `json:"title"`
	Resource       string           `json:"resource"`
	Region         string           `json:"region,omitempty"`
	Evidence       []string         `json:"evidence"`
	Risk           string           `json:"risk"`
	Recommendation string           `json:"recommendation"`
	Source         string           `json:"source"`
	Confidence     string           `json:"confidence"`
	DocsURL        string           `json:"docs_url,omitempty"`
}

type SecurityRecommendation struct {
	Priority      int              `json:"priority"`
	Severity      SecuritySeverity `json:"severity"`
	Title         string           `json:"title"`
	Action        string           `json:"action"`
	AffectedCount int              `json:"affected_count"`
	FindingIDs    []string         `json:"finding_ids"`
}

// SecuritySuppressedFinding records an accepted risk that was removed from the
// active finding set by a time-bounded operator configuration.
type SecuritySuppressedFinding struct {
	FindingID string `json:"finding_id"`
	RuleID    string `json:"rule_id"`
	Resource  string `json:"resource"`
	Reason    string `json:"reason"`
	Owner     string `json:"owner,omitempty"`
	ExpiresAt string `json:"expires_at"`
}

type SecurityCoverageCheck struct {
	Category       SecurityCategory       `json:"category"`
	Status         string                 `json:"status"`
	Weight         int                    `json:"weight"`
	ItemsScanned   int                    `json:"items_scanned"`
	Message        string                 `json:"message,omitempty"`
	CompletedUnits int                    `json:"completed_units,omitempty"`
	TotalUnits     int                    `json:"total_units,omitempty"`
	Units          []SecurityCoverageUnit `json:"units,omitempty"`
}

type SecurityCategoryScore struct {
	Category       SecurityCategory `json:"category"`
	Weight         int              `json:"weight"`
	Score          int              `json:"score"`
	Findings       int              `json:"findings"`
	CoverageStatus string           `json:"coverage_status"`
}

type SecuritySeverityCounts struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
}

// SecurityAuditReport is the deterministic, evidence-backed posture report.
// Score is nil when audit coverage is too low to make a defensible claim.
type SecurityAuditReport struct {
	AuditID         string                      `json:"audit_id"`
	ProjectID       string                      `json:"project_id"`
	GeneratedAt     string                      `json:"generated_at"`
	RuleVersion     string                      `json:"rule_version"`
	Score           *int                        `json:"score"`
	ScoreStatus     string                      `json:"score_status"`
	CoveragePercent int                         `json:"coverage_percent"`
	Counts          SecuritySeverityCounts      `json:"counts"`
	CategoryScores  []SecurityCategoryScore     `json:"category_scores"`
	Findings        []SecurityFinding           `json:"findings"`
	Recommendations []SecurityRecommendation    `json:"recommendations"`
	Suppressed      []SecuritySuppressedFinding `json:"suppressed,omitempty"`
	Coverage        []SecurityCoverageCheck     `json:"coverage"`
	Truncated       bool                        `json:"truncated,omitempty"`
	SummaryMarkdown string                      `json:"summary_markdown"`
}
