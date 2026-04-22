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
