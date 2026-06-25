package models

// SecretSummary is a lightweight view of a Secret Manager secret.
// Secret values are never read or returned — only metadata.
type SecretSummary struct {
	Name        string            `json:"name"`
	CreateTime  string            `json:"create_time,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Replication string            `json:"replication,omitempty"` // "automatic" or "user_managed"
	// ReferencedBy lists Cloud Run service names whose specs reference this secret
	// via secretKeyRef. Only populated when include_references=true.
	ReferencedBy []string `json:"referenced_by,omitempty"`
}

// ListSecretsRequest lists secrets (metadata only — values are never accessed).
type ListSecretsRequest struct {
	ProjectID         string `json:"project_id"`
	Filter            string `json:"filter,omitempty"`             // optional API-side filter
	IncludeReferences bool   `json:"include_references,omitempty"` // scan Cloud Run for secretKeyRef usage
}

// ListSecretsResponse holds the list result.
type ListSecretsResponse struct {
	Secrets []SecretSummary `json:"secrets"`
}
