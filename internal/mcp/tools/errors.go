package tools

import (
	"errors"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/asbrodova/aura-tracker-gcp/internal/gcp"
)

// handleServiceError converts typed GCP errors into the appropriate MCP response.
//
// User-actionable errors (permission denied, resource not found) become a
// CallToolResult with IsError=true so the LLM can read and react to the message.
//
// Unexpected infrastructure errors are returned as Go errors; mcp-go serialises
// those as JSON-RPC -32603 Internal Error responses.
func handleServiceError(op string, err error) (*mcp.CallToolResult, error) {
	var permDenied *gcp.PermissionDeniedError
	if errors.As(err, &permDenied) {
		return mcp.NewToolResultError(fmt.Sprintf(
			"%s: permission denied — verify the service account has the required IAM roles. Detail: %v",
			op, permDenied,
		)), nil
	}

	var notFound *gcp.NotFoundError
	if errors.As(err, &notFound) {
		return mcp.NewToolResultError(fmt.Sprintf(
			"%s: resource not found — verify project ID, location, and resource name. Detail: %v",
			op, notFound,
		)), nil
	}

	return nil, fmt.Errorf("%s: %w", op, err)
}
