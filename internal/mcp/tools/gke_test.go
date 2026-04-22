package tools

import (
	"context"
	"log/slog"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/asbrodova/aura-tracker-gcp/internal/gcp"
	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func TestListClustersHandler_Success(t *testing.T) {
	mock := &mockGCPService{
		returnListClusters: models.ListClustersResponse{
			Clusters: []models.ClusterSummary{
				{Name: "cluster-a", Location: "us-central1"},
				{Name: "cluster-b", Location: "us-east1"},
			},
		},
	}
	tools := NewGKETools(mock, slog.Default())

	result, err := tools.listClustersHandler(context.Background(), mcp.CallToolRequest{},
		models.ListClustersRequest{ProjectID: "my-proj", Location: "-"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.IsError {
		t.Error("unexpected IsError=true on success")
	}
}

func TestListClustersHandler_PermissionDenied(t *testing.T) {
	mock := &mockGCPService{
		returnListClustersErr: &gcp.PermissionDeniedError{Op: "gke.ListClusters"},
	}
	tools := NewGKETools(mock, slog.Default())

	result, err := tools.listClustersHandler(context.Background(), mcp.CallToolRequest{},
		models.ListClustersRequest{ProjectID: "my-proj", Location: "-"},
	)
	if err != nil {
		t.Fatalf("expected nil Go error for permission denied, got: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for permission denied")
	}
}

func TestScaleDeploymentHandler_DryRun(t *testing.T) {
	mock := &mockGCPService{
		returnScaleDeployment: models.ScaleDeploymentResponse{
			DryRun:         true,
			NodePoolName:   "default-pool",
			RequestedCount: 5,
			Description:    "DRY RUN: would resize node pool",
		},
	}
	tools := NewGKETools(mock, slog.Default())

	result, err := tools.scaleDeploymentHandler(context.Background(), mcp.CallToolRequest{},
		models.ScaleDeploymentRequest{
			ProjectID:    "my-proj",
			ClusterName:  "my-cluster",
			NodePoolName: "default-pool",
			NodeCount:    5,
			DryRun:       true,
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.IsError {
		t.Error("unexpected IsError on dry run success")
	}
}

func TestScaleDeploymentHandler_NoChangeNeeded(t *testing.T) {
	mock := &mockGCPService{
		returnScaleDeployment: models.ScaleDeploymentResponse{
			DryRun:         false,
			NodePoolName:   "default-pool",
			PreviousCount:  3,
			RequestedCount: 3,
			NoChangeNeeded: true,
			Description:    "no change needed",
		},
	}
	tools := NewGKETools(mock, slog.Default())

	result, err := tools.scaleDeploymentHandler(context.Background(), mcp.CallToolRequest{},
		models.ScaleDeploymentRequest{NodeCount: 3},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.IsError {
		t.Error("expected success result")
	}
}

func TestGetClusterBottlenecksHandler_Success(t *testing.T) {
	mock := &mockGCPService{
		returnGetBottlenecks: models.ClusterBottleneckReport{
			ClusterName: "my-cluster",
			Severity:    models.SeverityNone,
			Summary:     "no bottlenecks",
		},
	}
	tools := NewGKETools(mock, slog.Default())

	result, err := tools.getClusterBottlenecksHandler(context.Background(), mcp.CallToolRequest{},
		models.GetClusterBottlenecksRequest{
			ProjectID:       "my-proj",
			ClusterName:     "my-cluster",
			LookbackMinutes: 30,
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.IsError {
		t.Error("expected success result")
	}
}
