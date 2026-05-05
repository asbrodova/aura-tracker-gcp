package tools

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/asbrodova/aura-tracker-gcp/internal/gcp"
	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// handleServiceError converts typed GCP errors into the appropriate MCP response.
//
// Permission denied and retriable errors are returned as a structured ToolError
// JSON body (IsError=true) so the LLM can read and react to the structured fields.
//
// Not-found and confirmation-required errors use human-readable messages.
//
// Unexpected infrastructure errors are returned as Go errors; mcp-go serialises
// those as JSON-RPC -32603 Internal Error responses.
func handleServiceError(op string, err error) (*mcp.CallToolResult, error) {
	var permDenied *gcp.PermissionDeniedError
	if errors.As(err, &permDenied) {
		return toolErrorResult(models.ToolError{
			FailingAPI: permDenied.Op,
			Message: fmt.Sprintf(
				"%s: permission denied — verify the service account has the required IAM roles. Detail: %v",
				op, permDenied,
			),
			Retriable: false,
		})
	}

	var retriable *gcp.RetriableError
	if errors.As(err, &retriable) {
		return toolErrorResult(models.ToolError{
			FailingAPI: retriable.Op,
			Message: fmt.Sprintf(
				"%s: transient error — retry after back-off. Detail: %v",
				op, retriable,
			),
			Retriable: true,
		})
	}

	var notFound *gcp.NotFoundError
	if errors.As(err, &notFound) {
		return mcp.NewToolResultError(fmt.Sprintf(
			"%s: resource not found — verify project ID, location, and resource name. Detail: %v",
			op, notFound,
		)), nil
	}

	var confirmReq *gcp.ConfirmationRequiredError
	if errors.As(err, &confirmReq) {
		return mcp.NewToolResultError(fmt.Sprintf(
			"%s: confirmation required — %s",
			op, confirmReq.Message,
		)), nil
	}

	return nil, fmt.Errorf("%s: %w", op, err)
}

// toolErrorResult marshals a ToolError to JSON and wraps it in an IsError=true
// tool result. The LLM receives structured JSON it can parse and act on.
func toolErrorResult(te models.ToolError) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(te)
	if err != nil {
		return mcp.NewToolResultError(te.Message), nil
	}
	return mcp.NewToolResultError(string(b)), nil
}
