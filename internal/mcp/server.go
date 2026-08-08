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
	"github.com/asbrodova/aura-tracker-gcp/internal/costreasoning"
	"github.com/asbrodova/aura-tracker-gcp/internal/diagnostics"
	"github.com/asbrodova/aura-tracker-gcp/internal/diagram"
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
	anonymizer              anonymize.Anonymizer
	enabledModules          map[string]bool // nil = all modules
	defaultProjectID        string
	projectIDPlaceholder    string
	enableRecommenderExport bool
	enableCostReasoning     bool
	costReasoningConfig     costreasoning.Config
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

// WithProjectIDPlaceholder sets a placeholder string (e.g. "your-project") to
// display in tool-call permission prompts when project ID masking is active.
// The tool schema is patched so the LLM passes this value; the server replaces
// it with the real project ID before the GCP adapter sees it.
func WithProjectIDPlaceholder(placeholder string) Option {
	return func(o *serverOptions) { o.projectIDPlaceholder = placeholder }
}

// WithRecommenderExport registers the gcp_export_recommendations_to_bq tool.
// Off by default; enable via RECOMMENDER_BQ_EXPORT_ENABLED=true.
func WithRecommenderExport() Option {
	return func(o *serverOptions) { o.enableRecommenderExport = true }
}

// WithCostReasoning registers the opt-in, read-only billing explanation tool.
func WithCostReasoning(cfg costreasoning.Config) Option {
	return func(o *serverOptions) {
		o.enableCostReasoning = true
		o.costReasoningConfig = cfg
	}
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
		// When placeholder mode is active, patch the tool's JSON Schema for
		// project_id / project so the LLM sends the placeholder string rather
		// than omitting the field, making the permission popup informative.
		if o.projectIDPlaceholder != "" {
			for _, key := range []string{"project_id", "project"} {
				if raw, ok := t.Tool.InputSchema.Properties[key]; ok {
					if prop, ok := raw.(map[string]any); ok {
						prop["description"] = "GCP project ID. Pass '" + o.projectIDPlaceholder + "' as a placeholder — the server will use its configured project."
						prop["default"] = o.projectIDPlaceholder
					}
				}
			}
		}

		orig := t.Handler
		t.Handler = func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// Inject default project_id (and "project" for topology) so the LLM can
			// omit the project argument when a server default is configured.
			// Also replaces the placeholder value so the real project ID reaches the adapter.
			if o.defaultProjectID != "" {
				args, _ := req.Params.Arguments.(map[string]any)
				if args == nil {
					args = make(map[string]any)
				}
				for _, key := range []string{"project_id", "project"} {
					v, ok := args[key]
					if !ok || v == "" || v == nil || (o.projectIDPlaceholder != "" && v == o.projectIDPlaceholder) {
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
		tools.NewArchGraphTools(svc, diagram.New(svc, log), log),
		tools.NewTaggingTools(svc, log),
		tools.NewIncidentTools(diagnostics.New(svc, log), log),
	}
	if o.enableRecommenderExport {
		allModules = append(allModules, tools.NewRecommenderExportTools(svc, log))
	}
	if o.enableCostReasoning {
		allModules = append(allModules, tools.NewCostTools(costreasoning.New(svc, log, o.costReasoningConfig), log))
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
	promptList := []server.ServerPrompt{
		prm.AuditSecurityPosture(),
		prm.OptimizeBigQueryCosts(),
	}
	if o.enabledModules == nil || o.enabledModules[ModuleIncident] {
		promptList = append(promptList, prm.IncidentResponseHelper())
	}
	s.AddPrompts(promptList...)

	return s
}
