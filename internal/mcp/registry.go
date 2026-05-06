package mcp

import "github.com/mark3labs/mcp-go/server"

const (
	ModuleGKE        = "gke"
	ModuleCloudRun   = "cloudrun"
	ModulePubSub     = "pubsub"
	ModuleLogging    = "logging"
	ModuleMonitoring = "monitoring"
	ModuleIAM        = "iam"
	ModuleTopology   = "topology"
	ModuleAura       = "aura"
	ModuleStorage    = "storage"
	ModuleFunctions  = "functions"
	ModuleEventarc      = "eventarc"
	ModuleScheduler     = "scheduler"
	ModuleWorkflows     = "workflows"
	ModuleTasks         = "tasks"
	ModuleSecretManager = "secretmanager"
	ModuleVPCAccess        = "vpcaccess"
	ModuleCloudSQL         = "cloudsql"
	ModuleServerlessGraph  = "serverlessgraph"

	// Phase 2 modules
	ModuleGKEWorkloads = "gke_workloads"
	ModuleGKEMesh      = "gke_mesh"
	ModuleNetworking   = "networking"
	ModuleDatastores   = "datastores"
)

// AllModules is the default set used when --modules is absent.
var AllModules = []string{
	ModuleGKE, ModuleCloudRun, ModulePubSub, ModuleLogging,
	ModuleMonitoring, ModuleIAM, ModuleTopology, ModuleAura, ModuleStorage,
	ModuleFunctions, ModuleEventarc, ModuleScheduler,
	ModuleWorkflows, ModuleTasks, ModuleSecretManager,
	ModuleVPCAccess, ModuleCloudSQL, ModuleServerlessGraph,
	// Phase 2
	ModuleGKEWorkloads, ModuleGKEMesh, ModuleNetworking, ModuleDatastores,
}

// ToolModule is the interface every tool domain struct must satisfy.
type ToolModule interface {
	Name() string
	GetTools() []server.ServerTool
}

// FilteredRegistry returns tools from modules whose Name() appears in enabled.
// A nil enabled map includes all modules (zero-config default).
// An empty (non-nil) map registers zero tools (--modules=none).
func FilteredRegistry(modules []ToolModule, enabled map[string]bool) []server.ServerTool {
	var out []server.ServerTool
	for _, m := range modules {
		if enabled == nil || enabled[m.Name()] {
			out = append(out, m.GetTools()...)
		}
	}
	return out
}
