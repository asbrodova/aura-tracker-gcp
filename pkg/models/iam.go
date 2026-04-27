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

// MyPermissionsReport is returned by the gcp://{project}/iam/my-permissions resource.
// It splits permissions into granted and denied so the AI can explain capability gaps.
type MyPermissionsReport struct {
	ProjectID string             `json:"project_id"`
	Granted   []PermissionResult `json:"granted"`
	Denied    []PermissionResult `json:"denied"`
}
