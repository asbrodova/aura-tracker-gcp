package mcp

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	protocol "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/internal/safety"
)

// The approval prompt is built from the resolved project ID; it must reach
// the client with the environment alias, like every other MCP surface.
func TestApprovalPromptShowsAliasNotProjectID(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	s := New(safety.NewSafetyDecorator(&mockSvc{}, log), log, "test",
		WithModules(map[string]bool{ModuleGKE: true}),
		WithEnvironments(testEnvironmentRegistry(t)),
	)
	tool := s.GetTool("gcp_gke_scale_deployment")
	if tool == nil {
		t.Fatal("scale tool is not registered")
	}
	ctx := server.WithRequestProtocolInfo(context.Background(), &server.RequestProtocolInfo{
		Modern:             true,
		ProtocolVersion:    protocol.ProtocolVersion20260728,
		ClientCapabilities: &protocol.ClientCapabilities{Elicitation: &protocol.ElicitationCapability{}},
	})
	result, err := tool.Handler(ctx, protocol.CallToolRequest{Params: protocol.CallToolParams{
		Name: "gcp_gke_scale_deployment",
		Arguments: map[string]any{
			"project_id": "PROD", "location": "us-central1", "cluster_name": "c", "node_pool_name": "pool", "node_count": 3,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	request, ok := result.InputRequests["approval"]
	if result.ResultType != protocol.ResultTypeInputRequired || !ok || request.Elicitation == nil {
		t.Fatalf("expected an approval prompt, got %+v", result)
	}
	message := request.Elicitation.Message
	if strings.Contains(message, "private-prod-345") || !strings.Contains(message, "project prod") {
		t.Fatalf("approval prompt is not masked: %q", message)
	}
}

func TestMaskInputRequestsLeavesOtherResultsAlone(t *testing.T) {
	plain := protocol.NewToolResultText("private-prod-345")
	if got := maskInputRequests(plain, strings.ToUpper); got != plain {
		t.Fatal("results without input requests must be returned unchanged")
	}
}
