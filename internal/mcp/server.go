// Package mcp wires the MCP protocol layer. It imports ports.GCPService and
// internal/mcp/tools, but NEVER imports internal/gcp — that is the adapter
// layer, wired exclusively in cmd/.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/internal/anonymize"
	"github.com/asbrodova/aura-tracker-gcp/internal/costreasoning"
	"github.com/asbrodova/aura-tracker-gcp/internal/diagnostics"
	"github.com/asbrodova/aura-tracker-gcp/internal/diagram"
	"github.com/asbrodova/aura-tracker-gcp/internal/environments"
	"github.com/asbrodova/aura-tracker-gcp/internal/mcp/middleware"
	"github.com/asbrodova/aura-tracker-gcp/internal/mcp/prompts"
	"github.com/asbrodova/aura-tracker-gcp/internal/mcp/resources"
	"github.com/asbrodova/aura-tracker-gcp/internal/mcp/tools"
	"github.com/asbrodova/aura-tracker-gcp/internal/securityaudit"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

const serverName = "aura-tracker-gcp"

// Option configures the MCP server created by New.
type Option func(*serverOptions)

type serverOptions struct {
	anonymizer               anonymize.Anonymizer
	enabledModules           map[string]bool // nil = all modules
	environments             *environments.Registry
	projectIDReplacements    map[string]string
	projectIDPlaceholder     string
	defaultProjectID         string
	enableRecommenderExport  bool
	recommenderExportDataset string
	enableCostReasoning      bool
	costReasoningConfig      costreasoning.Config
	securityAuditConfig      securityaudit.Config
}

// WithEnvironments configures the allowed project selectors, default project,
// and public display aliases used by every MCP surface.
func WithEnvironments(registry *environments.Registry) Option {
	return func(o *serverOptions) { o.environments = registry }
}

// WithProjectIDReplacements adds mandatory output-only replacements, used for
// the legacy ANONYMIZE_PROJECT_ID placeholder on an unaliased environment.
func WithProjectIDReplacements(replacements map[string]string) Option {
	return func(o *serverOptions) { o.projectIDReplacements = replacements }
}

// WithProjectIDPlaceholder hides an unaliased default project in tool schemas
// when the legacy ANONYMIZE_PROJECT_ID mode is enabled.
func WithProjectIDPlaceholder(placeholder string) Option {
	return func(o *serverOptions) { o.projectIDPlaceholder = placeholder }
}

// WithAnonymizer attaches an Anonymizer to every registered tool handler.
// When not provided, NoopAnonymizer is used (pass-through, no overhead).
func WithAnonymizer(a anonymize.Anonymizer) Option {
	return func(o *serverOptions) { o.anonymizer = a }
}

// WithModules restricts which tool modules are registered.
// A nil map registers all modules; an empty map registers none.
func WithModules(modules map[string]bool) Option {
	return func(o *serverOptions) { o.enabledModules = modules }
}

// WithDefaultProjectID sets the fallback GCP project ID injected into any tool
// call that does not supply a project_id argument. This lets the LLM omit
// project_id without causing an empty-string error in the adapter.
func WithDefaultProjectID(id string) Option {
	return func(o *serverOptions) { o.defaultProjectID = id }
}

// WithRecommenderExport registers the gcp_export_recommendations_to_bq tool.
// Off by default; enable via RECOMMENDER_BQ_EXPORT_ENABLED=true.
func WithRecommenderExport(defaultDataset ...string) Option {
	return func(o *serverOptions) {
		o.enableRecommenderExport = true
		if len(defaultDataset) > 0 {
			o.recommenderExportDataset = strings.TrimSpace(defaultDataset[0])
		}
	}
}

// WithCostReasoning registers the opt-in, read-only billing explanation tool.
func WithCostReasoning(cfg costreasoning.Config) Option {
	return func(o *serverOptions) {
		o.enableCostReasoning = true
		o.costReasoningConfig = cfg
	}
}

// WithSecurityAuditConfig supplies explicit, time-bounded accepted-risk suppressions.
func WithSecurityAuditConfig(cfg securityaudit.Config) Option {
	return func(o *serverOptions) { o.securityAuditConfig = cfg }
}

// New creates and configures the MCP server, registering all tools, resources, and prompts.
// svc is the GCPService port — the only GCP dependency visible to this layer.
func New(svc ports.GCPService, log *slog.Logger, version string, opts ...Option) *server.MCPServer {
	o := &serverOptions{anonymizer: anonymize.NoopAnonymizer{}}
	for _, opt := range opts {
		opt(o)
	}

	if o.environments == nil && o.defaultProjectID != "" {
		o.environments, _ = environments.NewRegistry([]environments.Environment{{ProjectID: o.defaultProjectID}})
	}
	replacements := make(map[string]string)
	if o.environments != nil {
		for projectID, replacement := range o.environments.ReplacementMap() {
			replacements[projectID] = replacement
		}
	}
	for projectID, replacement := range o.projectIDReplacements {
		if _, aliased := replacements[projectID]; !aliased {
			replacements[projectID] = replacement
		}
	}
	privacyMasker := anonymize.NewProjectIDReplacer(replacements)

	serverOpts := []server.ServerOption{
		server.WithToolCapabilities(false),
		server.WithResourceCapabilities(false, true), // subscribe=false, listChanged=true
		server.WithPromptCapabilities(true),          // listChanged=true
	}
	if instructions := environmentInstructions(o.environments, o.projectIDPlaceholder); instructions != "" {
		serverOpts = append(serverOpts, server.WithInstructions(instructions))
	}
	s := server.NewMCPServer(
		serverName,
		version,
		serverOpts...,
	)

	wrap := func(t server.ServerTool) server.ServerTool {
		projectKeys := make([]string, 0, 1)
		for _, key := range []string{"project_id", "project"} {
			if raw, ok := t.Tool.InputSchema.Properties[key]; ok {
				projectKeys = append(projectKeys, key)
				if prop, ok := raw.(map[string]any); ok && o.environments != nil {
					if o.projectIDPlaceholder != "" {
						prop["description"] = "Configured GCP project. Pass '" + o.projectIDPlaceholder + "' or omit it to use the private server default."
						prop["default"] = o.projectIDPlaceholder
					} else {
						prop["description"] = o.environments.SelectorDescription()
						prop["default"] = o.environments.Default().DisplayName()
					}
				}
			}
		}
		orig := t.Handler
		t.Handler = func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var resolvedProject string
			if o.environments != nil && len(projectKeys) > 0 {
				args, _ := req.Params.Arguments.(map[string]any)
				if args == nil {
					args = make(map[string]any)
				}
				for _, key := range projectKeys {
					selector := ""
					if value, exists := args[key]; exists && value != nil {
						var ok bool
						selector, ok = value.(string)
						if !ok {
							return mcp.NewToolResultError(environmentSelectionError(o.environments)), nil
						}
					}
					if o.projectIDPlaceholder != "" && selector == o.projectIDPlaceholder {
						selector = ""
					}
					environment, err := o.environments.Resolve(selector)
					if err != nil {
						return mcp.NewToolResultError(environmentSelectionError(o.environments)), nil
					}
					args[key] = environment.ProjectID
					if resolvedProject == "" {
						resolvedProject = environment.ProjectID
					} else if resolvedProject != environment.ProjectID {
						return mcp.NewToolResultError("all project selectors in one request must resolve to the same configured environment"), nil
					}
				}
				if err := validateScopedArguments(args, resolvedProject); err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				req.Params.Arguments = args
			}
			result, err := orig(middleware.WithCorrelationID(ctx), req)
			if err != nil {
				return nil, errors.New(privacyMasker.ReplaceString(err.Error()))
			}
			scrubbed, err := privacyMasker.Scrub(ctx, result)
			if err != nil {
				return mcp.NewToolResultError("project identifier privacy filter failed; result withheld"), nil
			}
			return scrubbed, nil
		}
		return anonymize.WrapHandler(t, o.anonymizer)
	}
	wrapResource := func(resource server.ServerResource) server.ServerResource {
		original := resource.Handler
		resource.Handler = func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			contents, err := original(ctx, req)
			if err != nil {
				return nil, anonymize.ScrubError(ctx, o.anonymizer, errors.New(privacyMasker.ReplaceString(err.Error())))
			}
			scrubbed, err := privacyMasker.ScrubResourceContents(contents)
			if err != nil {
				return nil, errors.New("project identifier privacy filter failed; resource withheld")
			}
			return anonymize.ScrubResourceContents(ctx, o.anonymizer, scrubbed)
		}
		return resource
	}
	wrapResourceTemplate := func(resource server.ServerResourceTemplate) server.ServerResourceTemplate {
		original := resource.Handler
		resource.Handler = func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			contents, err := original(ctx, req)
			if err != nil {
				return nil, anonymize.ScrubError(ctx, o.anonymizer, errors.New(privacyMasker.ReplaceString(err.Error())))
			}
			scrubbed, err := privacyMasker.ScrubResourceContents(contents)
			if err != nil {
				return nil, errors.New("project identifier privacy filter failed; resource withheld")
			}
			return anonymize.ScrubResourceContents(ctx, o.anonymizer, scrubbed)
		}
		return resource
	}
	wrapPrompt := func(prompt server.ServerPrompt) server.ServerPrompt {
		original := prompt.Handler
		prompt.Handler = func(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			result, err := original(ctx, req)
			if err != nil {
				return nil, anonymize.ScrubError(ctx, o.anonymizer, errors.New(privacyMasker.ReplaceString(err.Error())))
			}
			scrubbed, err := privacyMasker.ScrubPromptResult(result)
			if err != nil {
				return nil, errors.New("project identifier privacy filter failed; prompt withheld")
			}
			return anonymize.ScrubPromptResult(ctx, o.anonymizer, scrubbed)
		}
		return prompt
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
		tools.NewSecurityTools(securityaudit.New(svc, log, securityaudit.WithConfig(o.securityAuditConfig)), log),
	}
	if o.enableRecommenderExport {
		allModules = append(allModules, tools.NewRecommenderExportTools(svc, log, o.recommenderExportDataset))
	}
	if o.enableCostReasoning {
		allModules = append(allModules, tools.NewCostTools(costreasoning.New(svc, log, o.costReasoningConfig), log))
	}
	for _, t := range FilteredRegistry(allModules, o.enabledModules) {
		s.AddTools(wrap(t))
	}

	// --- Resources ---
	bqRes := resources.NewBigQueryResources(svc, log, o.environments, o.projectIDPlaceholder)
	crRes := resources.NewCloudRunResources(svc, log, o.environments, o.projectIDPlaceholder)
	gcsRes := resources.NewStorageResources(svc, log, o.environments, o.projectIDPlaceholder)
	iamRes := resources.NewIAMResources(svc, log, o.environments, o.projectIDPlaceholder)

	configuredEnvironments := []environments.Environment{{}}
	if o.environments != nil {
		configuredEnvironments = o.environments.Environments()
		if o.projectIDPlaceholder != "" {
			for i := range configuredEnvironments {
				if configuredEnvironments[i].Alias == "" {
					configuredEnvironments[i].Alias = o.projectIDPlaceholder
				}
			}
		}
	}
	staticResources := make([]server.ServerResource, 0, len(configuredEnvironments)*4)
	for _, environment := range configuredEnvironments {
		staticResources = append(staticResources,
			wrapResource(bqRes.DatasetList(environment)),
			wrapResource(crRes.ServiceList(environment)),
			wrapResource(gcsRes.BucketList(environment)),
			wrapResource(iamRes.MyPermissions(environment)),
		)
	}
	s.AddResources(staticResources...)

	s.AddResourceTemplates(
		wrapResourceTemplate(bqRes.TableListTemplate()),
		wrapResourceTemplate(bqRes.TableSchemaTemplate()),
		wrapResourceTemplate(crRes.ServiceSnapshotTemplate()),
		wrapResourceTemplate(crRes.RevisionsTemplate()),
		wrapResourceTemplate(gcsRes.BucketMetadataTemplate()),
		wrapResourceTemplate(gcsRes.ObjectListTemplate()),
	)

	// --- Prompts ---
	prm := prompts.NewGCPPrompts(svc, log, o.environments, o.projectIDPlaceholder)
	promptList := []server.ServerPrompt{prm.OptimizeBigQueryCosts()}
	if o.enabledModules == nil || o.enabledModules[ModuleSecurity] {
		promptList = append(promptList, prm.AuditSecurityPosture())
	}
	if o.enabledModules == nil || o.enabledModules[ModuleIncident] {
		promptList = append(promptList, prm.IncidentResponseHelper())
	}
	for i := range promptList {
		promptList[i] = wrapPrompt(promptList[i])
	}
	s.AddPrompts(promptList...)

	return s
}

func validateScopedArguments(args map[string]any, projectID string) error {
	if err := validateArgumentStrings(args, "arguments"); err != nil {
		return err
	}
	segmentKeys := map[string]bool{
		"bucket_name": true, "cluster_name": true, "dataset": true,
		"function_name": true, "job_name": true, "location": true,
		"name": true, "namespace": true, "node_pool_name": true,
		"region": true, "repository": true, "service_name": true,
		"table": true, "topic_name": true, "trigger_name": true,
		"workflow_name": true,
	}
	for key := range segmentKeys {
		if raw, ok := args[key]; ok {
			if value, ok := raw.(string); ok && (strings.ContainsAny(value, `/\\`) || value == "." || value == "..") {
				return fmt.Errorf("%s must be a resource name segment, not a path", key)
			}
		}
	}
	if rawRegions, ok := args["regions"].([]any); ok {
		for _, rawRegion := range rawRegions {
			if region, ok := rawRegion.(string); ok && strings.ContainsAny(region, `/\\`) {
				return errors.New("regions entries must be region names, not paths")
			}
		}
	}
	raw, exists := args["urn"]
	if !exists || raw == nil {
		return nil
	}
	urn, ok := raw.(string)
	if !ok {
		return errors.New("resource URN must be a string")
	}
	parts := strings.SplitN(urn, ":", 6)
	if len(parts) != 6 || parts[0] != "urn" || parts[1] != "gcp" || parts[4] == "" {
		return errors.New("resource URN must use urn:gcp:{kind}:{region}:{project_id}:{resource-name}")
	}
	if parts[4] != projectID {
		return errors.New("resource URN is outside the selected configured environment")
	}
	if parts[2] == "project" && parts[5] != projectID {
		return errors.New("project URN resource must match its project_id segment")
	}
	return nil
}

func validateArgumentStrings(value any, path string) error {
	switch typed := value.(type) {
	case string:
		if len(typed) > 4096 {
			return fmt.Errorf("%s exceeds 4096 bytes", path)
		}
		for _, char := range typed {
			if char == 0 || (char < ' ' && char != '\t' && char != '\n' && char != '\r') {
				return fmt.Errorf("%s contains a control character", path)
			}
		}
	case []any:
		for i, item := range typed {
			if err := validateArgumentStrings(item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case map[string]any:
		for key, item := range typed {
			if len(key) > 256 {
				return fmt.Errorf("%s contains an oversized key", path)
			}
			if err := validateArgumentStrings(item, path+"."+key); err != nil {
				return err
			}
		}
	}
	return nil
}

func environmentSelectionError(registry *environments.Registry) string {
	if registry == nil {
		return "environment is not configured"
	}
	return "unknown environment; available environments: " + strings.Join(registry.DisplayNames(), ", ")
}

func environmentInstructions(registry *environments.Registry, placeholder string) string {
	if registry == nil {
		return ""
	}
	if placeholder != "" {
		return fmt.Sprintf(
			"One private default GCP project is configured. Pass %q in the tool's project_id/project argument or omit the argument to use it. Never reveal the underlying project ID.",
			placeholder,
		)
	}
	configured := make([]string, 0, len(registry.Environments()))
	for _, environment := range registry.Environments() {
		name := environment.DisplayName()
		if environment.Default {
			name += " (default)"
		}
		configured = append(configured, name)
	}
	return fmt.Sprintf(
		"Configured environments: %s. When the user names an environment, pass that alias or configured project ID in the tool's project_id/project argument. Alias matching is case-insensitive. When no environment is named, omit the argument to use the default. Always refer to aliased projects by alias in answers and never reveal their project IDs.",
		strings.Join(configured, ", "),
	)
}
