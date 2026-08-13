package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
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
	var permDenied *ports.PermissionDeniedError
	if errors.As(err, &permDenied) {
		return toolErrorResult(models.ToolError{
			FailingAPI:            permDenied.Op,
			MissingIAMPermissions: permDenied.MissingPermissions,
			Message: fmt.Sprintf(
				"%s: permission denied — verify the service account has the required IAM roles. Detail: %v",
				op, permDenied,
			),
			Retriable: false,
		})
	}

	var quotaExhausted *ports.RecommenderQuotaExhaustedError
	if errors.As(err, &quotaExhausted) {
		retryAt := ""
		retryInstruction := "do not retry until the Recommender quota becomes available"
		if !quotaExhausted.RetryAt.IsZero() {
			retryAt = quotaExhausted.RetryAt.UTC().Format(time.RFC3339)
			retryInstruction = fmt.Sprintf("do not retry before %s", retryAt)
		}
		quotaDescription := "Cloud Recommender quota exhausted"
		if quotaExhausted.Window != "" {
			quotaDescription += fmt.Sprintf(" (%s window)", quotaExhausted.Window)
		}

		return toolErrorResult(models.ToolError{
			FailingAPI: quotaExhausted.Op,
			Message: fmt.Sprintf(
				"%s: %s — %s.",
				op, quotaDescription, retryInstruction,
			),
			Retriable: true,
			RetryAt:   retryAt,
		})
	}

	var retriable *ports.RetriableError
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

	var notFound *ports.NotFoundError
	if errors.As(err, &notFound) {
		return mcp.NewToolResultError(fmt.Sprintf(
			"%s: resource not found — verify project ID, location, and resource name. Detail: %v",
			op, notFound,
		)), nil
	}

	var confirmReq *ports.ConfirmationRequiredError
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
