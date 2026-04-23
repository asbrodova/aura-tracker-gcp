// Package ports defines the hexagon boundary between the MCP protocol layer
// and the GCP adapter layer. The MCP layer imports only this package — never
// internal/gcp — keeping the two sides strictly decoupled.
package ports

import (
	"context"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// GCPService is the single secondary-port interface that all GCP adapters must
// implement. Tool handlers in internal/mcp call only these methods; they never
// import a GCP SDK type directly.
//
// All implementations must be safe for concurrent use.
type GCPService interface {
	// --- GKE ---
	ListClusters(ctx context.Context, req models.ListClustersRequest) (models.ListClustersResponse, error)
	GetClusterDetails(ctx context.Context, req models.GetClusterDetailsRequest) (models.ClusterDetails, error)
	GetClusterBottlenecks(ctx context.Context, req models.GetClusterBottlenecksRequest) (models.ClusterBottleneckReport, error)
	ScaleDeployment(ctx context.Context, req models.ScaleDeploymentRequest) (models.ScaleDeploymentResponse, error)

	// --- Cloud Run ---
	ListServices(ctx context.Context, req models.ListServicesRequest) (models.ListServicesResponse, error)
	GetServiceDetails(ctx context.Context, req models.GetServiceDetailsRequest) (models.ServiceDetails, error)
	UpdateTraffic(ctx context.Context, req models.UpdateTrafficRequest) (models.UpdateTrafficResponse, error)

	// --- Pub/Sub ---
	ListTopics(ctx context.Context, req models.ListTopicsRequest) (models.ListTopicsResponse, error)
	InspectTopicHealth(ctx context.Context, req models.InspectTopicHealthRequest) (models.TopicHealthReport, error)

	// --- Cloud Logging ---
	QueryRecentLogs(ctx context.Context, req models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error)

	// --- Cloud Monitoring ---
	GetMetrics(ctx context.Context, req models.GetMetricsRequest) (models.GetMetricsResponse, error)

	// --- IAM ---
	TestPermissions(ctx context.Context, req models.TestPermissionsRequest) (models.TestPermissionsResponse, error)

	// --- Topology ---
	GetServiceTopology(ctx context.Context, req models.GetServiceTopologyRequest) (models.ServiceTopologyReport, error)
}
