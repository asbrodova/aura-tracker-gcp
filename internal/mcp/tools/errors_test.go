package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

func TestHandleServiceError_PermissionDenied(t *testing.T) {
	pde := &ports.PermissionDeniedError{Op: "gke.ListClusters", Err: errors.New("denied")}

	result, err := handleServiceError("gcp_gke_list_clusters", pde)
	if err != nil {
		t.Fatalf("expected nil Go error, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil CallToolResult")
	}
	if !result.IsError {
		t.Error("expected IsError=true for permission denied")
	}
}

func TestHandleServiceError_NotFound(t *testing.T) {
	nfe := &ports.NotFoundError{Op: "gke.GetClusterDetails", Err: errors.New("not found")}

	result, err := handleServiceError("gcp_gke_get_cluster_details", nfe)
	if err != nil {
		t.Fatalf("expected nil Go error, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil CallToolResult")
	}
	if !result.IsError {
		t.Error("expected IsError=true for not found")
	}
}

func TestHandleServiceError_CostSourceNotConfigured(t *testing.T) {
	result, err := handleServiceError("gcp_cost_explain", fmt.Errorf("wrapped: %w", &ports.CostSourceNotConfiguredError{ProjectID: "preprod-project-123"}))
	if err != nil || result == nil || !result.IsError {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	content, ok := result.Content[0].(mcp.TextContent)
	if !ok || !strings.Contains(content.Text, "preprod-project-123") {
		t.Fatalf("content = %#v", result.Content)
	}
}

func TestHandleServiceError_Unexpected(t *testing.T) {
	unexpected := errors.New("connection reset by peer")

	result, err := handleServiceError("gcp_gke_list_clusters", unexpected)
	if err == nil {
		t.Fatal("expected non-nil Go error for unexpected error")
	}
	if result != nil {
		t.Error("expected nil result for unexpected error")
	}
}

func TestHandleServiceError_WrappedPermissionDenied(t *testing.T) {
	pde := &ports.PermissionDeniedError{Op: "op", Err: errors.New("x")}
	wrapped := errors.Join(errors.New("outer"), pde)

	result, goErr := handleServiceError("op", wrapped)
	if goErr != nil {
		t.Fatalf("expected nil Go error for wrapped PermissionDenied, got: %v", goErr)
	}
	if result == nil || !result.IsError {
		t.Error("expected IsError=true for wrapped PermissionDenied")
	}
}

func TestHandleServiceError_RecommenderQuotaExhausted(t *testing.T) {
	retryAt := time.Date(2026, time.August, 14, 7, 0, 0, 0, time.FixedZone("PDT", -7*60*60))
	quotaErr := &ports.RecommenderQuotaExhaustedError{
		Op:            "recommender.ListRecommendations",
		RecommenderID: "google.iam.policy.Recommender",
		RetryAt:       retryAt,
		Window:        "daily",
	}

	result, goErr := handleServiceError("gcp_security_audit", quotaErr)
	if goErr != nil {
		t.Fatalf("expected nil Go error for Recommender quota exhaustion, got: %v", goErr)
	}
	if result == nil || !result.IsError {
		t.Fatalf("expected IsError=true for Recommender quota exhaustion, got: %+v", result)
	}
	if len(result.Content) != 1 {
		t.Fatalf("content count = %d, want 1", len(result.Content))
	}
	content, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("content type = %T, want mcp.TextContent", result.Content[0])
	}

	var toolErr models.ToolError
	if err := json.Unmarshal([]byte(content.Text), &toolErr); err != nil {
		t.Fatalf("unmarshal ToolError: %v; content = %q", err, content.Text)
	}
	if toolErr.FailingAPI != quotaErr.Op {
		t.Errorf("failing_api = %q, want %q", toolErr.FailingAPI, quotaErr.Op)
	}
	if !toolErr.Retriable {
		t.Error("retriable = false, want true")
	}
	wantRetryAt := retryAt.UTC().Format(time.RFC3339)
	if toolErr.RetryAt != wantRetryAt {
		t.Errorf("retry_at = %q, want %q", toolErr.RetryAt, wantRetryAt)
	}
	if !strings.Contains(toolErr.Message, "do not retry before "+wantRetryAt) {
		t.Errorf("message %q does not contain retry instruction", toolErr.Message)
	}
}

func TestHandleServiceError_RecommenderQuotaTakesPrecedenceOverRetriable(t *testing.T) {
	retryAt := time.Date(2026, time.August, 14, 14, 0, 0, 0, time.UTC)
	quotaErr := &ports.RecommenderQuotaExhaustedError{
		Op:            "recommender.ListRecommendations",
		RecommenderID: "google.iam.policy.Recommender",
		RetryAt:       retryAt,
		Window:        "minute",
	}
	joined := errors.Join(
		&ports.RetriableError{Op: "recommender.ListRecommendations", Err: errors.New("resource exhausted")},
		quotaErr,
	)

	result, goErr := handleServiceError("gcp_security_audit", joined)
	if goErr != nil {
		t.Fatalf("expected nil Go error, got: %v", goErr)
	}
	content, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("content type = %T, want mcp.TextContent", result.Content[0])
	}
	var toolErr models.ToolError
	if err := json.Unmarshal([]byte(content.Text), &toolErr); err != nil {
		t.Fatalf("unmarshal ToolError: %v", err)
	}
	if toolErr.RetryAt != retryAt.Format(time.RFC3339) {
		t.Errorf("retry_at = %q, want quota-specific error", toolErr.RetryAt)
	}
}
