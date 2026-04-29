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
)

// AllModules is the default set used when --modules is absent.
var AllModules = []string{
	ModuleGKE, ModuleCloudRun, ModulePubSub, ModuleLogging,
	ModuleMonitoring, ModuleIAM, ModuleTopology, ModuleAura, ModuleStorage,
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
