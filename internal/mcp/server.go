// Package mcp wires the MCP protocol layer. It imports ports.GCPService and
// internal/mcp/tools, but NEVER imports internal/gcp — that is the adapter
// layer, wired exclusively in cmd/.
package mcp

import (
	"context"
	"log/slog"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/internal/anonymize"
	"github.com/asbrodova/aura-tracker-gcp/internal/mcp/middleware"
	"github.com/asbrodova/aura-tracker-gcp/internal/mcp/prompts"
	"github.com/asbrodova/aura-tracker-gcp/internal/mcp/resources"
	"github.com/asbrodova/aura-tracker-gcp/internal/mcp/tools"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

const serverName = "aura-tracker-gcp"

// Option configures the MCP server created by New.
type Option func(*serverOptions)

type serverOptions struct {
	anonymizer       anonymize.Anonymizer
	enabledModules   map[string]bool // nil = all modules
	defaultProjectID string
}

// WithAnonymizer attaches an Anonymizer to every registered tool handler.
// When not provided, NoopAnonymizer is used (pass-through, no overhead).
func WithAnonymizer(a anonymize.Anonymizer) Option {
	return func(o *serverOptions) { o.anonymizer = a }
}

// WithModules restricts which tool modules are registered.
// A nil or empty map registers all modules (default behavior).
func WithModules(modules map[string]bool) Option {
	return func(o *serverOptions) { o.enabledModules = modules }
}

// WithDefaultProjectID sets the fallback GCP project ID injected into any tool
// call that does not supply a project_id argument. This lets the LLM omit
// project_id without causing an empty-string error in the adapter.
func WithDefaultProjectID(id string) Option {
	return func(o *serverOptions) { o.defaultProjectID = id }
}

// New creates and configures the MCP server, registering all tools, resources, and prompts.
// svc is the GCPService port — the only GCP dependency visible to this layer.
func New(svc ports.GCPService, log *slog.Logger, version string, opts ...Option) *server.MCPServer {
	o := &serverOptions{anonymizer: anonymize.NoopAnonymizer{}}
	for _, opt := range opts {
		opt(o)
	}

	s := server.NewMCPServer(
		serverName,
		version,
		server.WithToolCapabilities(false),
		server.WithResourceCapabilities(false, true), // subscribe=false, listChanged=true
		server.WithPromptCapabilities(true),          // listChanged=true
	)

	wrap := func(t server.ServerTool) server.ServerTool {
		orig := t.Handler
		t.Handler = func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// Inject default project_id (and "project" for topology) so the LLM can
			// omit the project argument when a server default is configured.
			if o.defaultProjectID != "" {
				args, _ := req.Params.Arguments.(map[string]any)
				if args == nil {
					args = make(map[string]any)
				}
				for _, key := range []string{"project_id", "project"} {
					if v, ok := args[key]; !ok || v == "" || v == nil {
						args[key] = o.defaultProjectID
					}
				}
				req.Params.Arguments = args
			}
			return orig(middleware.WithCorrelationID(ctx), req)
		}
		return anonymize.WrapHandler(t, o.anonymizer)
	}

	// --- Tools ---
	allModules := []ToolModule{
		tools.NewGKETools(svc, log),
		tools.NewCloudRunTools(svc, log),
		tools.NewPubSubTools(svc, log),
		tools.NewLoggingTools(svc, log),
		tools.NewMonitoringTools(svc, log),
		tools.NewIAMTools(svc, log),
		tools.NewTopologyTools(svc, log),
		tools.NewAuraTools(svc, log),
		tools.NewStorageTools(svc, log),
		tools.NewFunctionsTools(svc, log),
		tools.NewEventarcTools(svc, log),
		tools.NewSchedulerTools(svc, log),
		tools.NewWorkflowsTools(svc, log),
		tools.NewTasksTools(svc, log),
		tools.NewSecretManagerTools(svc, log),
		tools.NewVPCAccessTools(svc, log),
		tools.NewCloudSQLTools(svc, log),
		tools.NewServerlessGraphTools(svc, log),
		tools.NewGKEWorkloadTools(svc, log),
		tools.NewGKEMeshTools(svc, log),
		tools.NewNetworkingTools(svc, log),
		tools.NewDatastoreTools(svc, log),
		tools.NewSupplyChainTools(svc, log),
		tools.NewCoverageTools(svc, log),
		tools.NewArchGraphTools(svc, log),
		tools.NewTaggingTools(svc, log),
	}
	for _, t := range FilteredRegistry(allModules, o.enabledModules) {
		s.AddTools(wrap(t))
	}

	// --- Resources ---
	project := os.Getenv("GCP_PROJECT_ID")
	bqRes := resources.NewBigQueryResources(svc, log)
	crRes := resources.NewCloudRunResources(svc, log)
	gcsRes := resources.NewStorageResources(svc, log)
	iamRes := resources.NewIAMResources(svc, log)

	s.AddResources(
		bqRes.DatasetList(project),
		crRes.ServiceList(project),
		gcsRes.BucketList(project),
		iamRes.MyPermissions(project),
	)

	s.AddResourceTemplates(
		bqRes.TableListTemplate(),
		bqRes.TableSchemaTemplate(),
		crRes.ServiceSnapshotTemplate(),
		crRes.RevisionsTemplate(),
		gcsRes.BucketMetadataTemplate(),
		gcsRes.ObjectListTemplate(),
	)

	// --- Prompts ---
	prm := prompts.NewGCPPrompts(svc, log)
	s.AddPrompts(
		prm.AuditSecurityPosture(),
		prm.OptimizeBigQueryCosts(),
		prm.IncidentResponseHelper(),
	)

	return s
}
