package gcp

// clientKey identifies a named GCP client slot in gcpAdapter.
type clientKey string

const (
	clientClusterMgr clientKey = "clusterMgr"
	clientRunSvc     clientKey = "runSvc"
	clientPubSub     clientKey = "pubsub"
	clientLogAdmin   clientKey = "logAdmin"
	clientMetric     clientKey = "metric"
	clientCRM        clientKey = "crm"
	clientBQ         clientKey = "bq"
	clientGCS        clientKey = "gcs"
)

// moduleClientDeps maps each module name to the GCP clients it requires.
// "_resources" is a pseudo-module for always-on MCP Resources (BigQuery, Storage, IAM).
var moduleClientDeps = map[string][]clientKey{
	"gke":        {clientClusterMgr, clientMetric, clientLogAdmin}, // GetClusterBottlenecks fans out to metric+logAdmin
	"cloudrun":   {clientRunSvc},
	"pubsub":     {clientPubSub},
	"logging":    {clientLogAdmin},
	"monitoring": {clientMetric},
	"iam":        {clientCRM},
	"topology":   {clientRunSvc, clientPubSub}, // scans run annotations + pubsub push subscriptions
	"aura":       {clientMetric, clientRunSvc, clientClusterMgr}, // GKE cluster discovery + control-plane health
	"storage":    {clientGCS},
	"_resources": {clientBQ, clientGCS, clientCRM}, // always initialized for MCP Resources
}

// neededClients returns the union of client keys for the given module set,
// always including the _resources pseudo-module.
// nil enabled = all modules = all clients.
// empty (non-nil) enabled = zero tools, but _resources clients still initialized.
func neededClients(enabled map[string]bool) map[clientKey]bool {
	out := make(map[clientKey]bool)
	for _, k := range moduleClientDeps["_resources"] {
		out[k] = true
	}
	if enabled == nil {
		for mod, keys := range moduleClientDeps {
			if mod == "_resources" {
				continue
			}
			for _, k := range keys {
				out[k] = true
			}
		}
		return out
	}
	for mod := range enabled {
		for _, k := range moduleClientDeps[mod] {
			out[k] = true
		}
	}
	return out
}
