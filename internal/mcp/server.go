// Package mcp wires the MCP protocol layer. It imports ports.GCPService and
// internal/mcp/tools, but NEVER imports internal/gcp — that is the adapter
// layer, wired exclusively in cmd/.
package mcp

import (
	"log/slog"

	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/internal/anonymize"
	"github.com/asbrodova/aura-tracker-gcp/internal/mcp/tools"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

const serverName = "aura-tracker-gcp"

// Option configures the MCP server created by New.
type Option func(*serverOptions)

type serverOptions struct {
	anonymizer anonymize.Anonymizer
}

// WithAnonymizer attaches an Anonymizer to every registered tool handler.
// When not provided, NoopAnonymizer is used (pass-through, no overhead).
func WithAnonymizer(a anonymize.Anonymizer) Option {
	return func(o *serverOptions) { o.anonymizer = a }
}

// New creates and configures the MCP server, registering all tools.
// svc is the GCPService port — the only GCP dependency visible to this layer.
// Existing callers passing only (svc, log, version) are unaffected.
func New(svc ports.GCPService, log *slog.Logger, version string, opts ...Option) *server.MCPServer {
	o := &serverOptions{anonymizer: anonymize.NoopAnonymizer{}}
	for _, opt := range opts {
		opt(o)
	}

	s := server.NewMCPServer(
		serverName,
		version,
		server.WithToolCapabilities(false),
	)

	wrap := func(t server.ServerTool) server.ServerTool {
		return anonymize.WrapHandler(t, o.anonymizer)
	}

	gke := tools.NewGKETools(svc, log)
	cr := tools.NewCloudRunTools(svc, log)
	ps := tools.NewPubSubTools(svc, log)
	lg := tools.NewLoggingTools(svc, log)
	mon := tools.NewMonitoringTools(svc, log)
	iam := tools.NewIAMTools(svc, log)
	topo := tools.NewTopologyTools(svc, log)

	s.AddTools(
		wrap(gke.ListClusters()),
		wrap(gke.GetClusterDetails()),
		wrap(gke.GetClusterBottlenecks()),
		wrap(gke.ScaleDeployment()),
		wrap(cr.ListServices()),
		wrap(cr.GetServiceDetails()),
		wrap(cr.UpdateTraffic()),
		wrap(ps.ListTopics()),
		wrap(ps.InspectTopicHealth()),
		wrap(lg.QueryRecent()),
		wrap(mon.GetMetrics()),
		wrap(iam.TestPermissions()),
		wrap(topo.GetServiceTopology()),
	)

	return s
}
