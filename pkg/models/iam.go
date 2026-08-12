package models

type TestPermissionsRequest struct {
	ProjectID   string   `json:"project_id"`
	Permissions []string `json:"permissions"`
}

type PermissionResult struct {
	Permission string `json:"permission"`
	Allowed    bool   `json:"allowed"`
}

type TestPermissionsResponse struct {
	ProjectID      string             `json:"project_id"`
	Results        []PermissionResult `json:"results"`
	CallerIdentity string             `json:"caller_identity,omitempty"`
}

// --- Resource IAM Bindings ---

type GetResourceIAMBindingsRequest struct {
	ProjectID string `json:"project_id"`
	URN       string `json:"urn"`
}

type IAMBinding struct {
	Role    string   `json:"role"`
	Members []string `json:"members"`
}

type GetResourceIAMBindingsResponse struct {
	URN      string       `json:"urn"`
	Bindings []IAMBinding `json:"bindings"`
}

// --- Service Accounts ---

type ListServiceAccountsRequest struct {
	ProjectID string `json:"project_id"`
	PageSize  int    `json:"page_size,omitempty"`
	PageToken string `json:"page_token,omitempty"`
}

type ServiceAccountSummary struct {
	Name        string `json:"name"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
	UniqueID    string `json:"unique_id,omitempty"`
}

type ListServiceAccountsResponse struct {
	ServiceAccounts []ServiceAccountSummary `json:"service_accounts"`
	NextPageToken   string                  `json:"next_page_token,omitempty"`
	Truncated       bool                    `json:"truncated,omitempty"`
}

// MyPermissionsReport is returned by the gcp://{project}/iam/my-permissions resource.
// It splits permissions into granted and denied so the AI can explain capability gaps.
type MyPermissionsReport struct {
	ProjectID string             `json:"project_id"`
	Granted   []PermissionResult `json:"granted"`
	Denied    []PermissionResult `json:"denied"`
}
