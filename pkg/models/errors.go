package models

// ToolError is the structured error envelope returned by every tool when a
// user-actionable failure occurs (e.g. missing IAM permission, retriable
// quota exhaustion). The LLM receives this as the IsError=true result body.
type ToolError struct {
	FailingAPI            string   `json:"failing_api"`
	MissingIAMPermissions []string `json:"missing_iam_permissions,omitempty"`
	Message               string   `json:"message"`
	Retriable             bool     `json:"retriable"`
	RetryAt               string   `json:"retry_at,omitempty"`
}
