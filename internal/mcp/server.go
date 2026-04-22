// Package mcp wires the MCP protocol layer. It imports ports.GCPService and
// internal/mcp/tools, but NEVER imports internal/gcp — that is the adapter
// layer, wired exclusively in cmd/.
package mcp

import (
	"log/slog"

	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/internal/mcp/tools"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

const (
	serverName    = "aura-tracker-gcp"
	serverVersion = "0.1.0"
)

// New creates and configures the MCP server, registering all tools.
// svc is the GCPService port — the only GCP dependency visible to this layer.
func New(svc ports.GCPService, log *slog.Logger) *server.MCPServer {
	s := server.NewMCPServer(
		serverName,
		serverVersion,
		server.WithToolCapabilities(false),
	)

	gke := tools.NewGKETools(svc, log)
	cr := tools.NewCloudRunTools(svc, log)
	ps := tools.NewPubSubTools(svc, log)
	lg := tools.NewLoggingTools(svc, log)
	mon := tools.NewMonitoringTools(svc, log)
	iam := tools.NewIAMTools(svc, log)

	s.AddTools(
		gke.ListClusters(),
		gke.GetClusterDetails(),
		gke.GetClusterBottlenecks(),
		gke.ScaleDeployment(),
		cr.ListServices(),
		cr.GetServiceDetails(),
		cr.UpdateTraffic(),
		ps.ListTopics(),
		ps.InspectTopicHealth(),
		lg.QueryRecent(),
		mon.GetMetrics(),
		iam.TestPermissions(),
	)

	return s
}
