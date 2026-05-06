package gcp

// clientKey identifies a named GCP client slot in gcpAdapter.
type clientKey string

const (
	clientClusterMgr  clientKey = "clusterMgr"
	clientRunSvc      clientKey = "runSvc"
	clientRunJobs     clientKey = "runJobs" // Cloud Run Jobs + Executions clients
	clientFunctionsV1 clientKey = "functionsV1"
	clientEventarc       clientKey = "eventarc"
	clientScheduler      clientKey = "scheduler"
	clientWorkflows      clientKey = "workflows"
	clientWorkflowExec   clientKey = "workflowExec"
	clientTasks          clientKey = "tasks"
	clientSecretMgr      clientKey = "secretMgr"
	clientVPCAccess      clientKey = "vpcAccess"
	clientSQLAdmin       clientKey = "sqlAdmin"
	clientTrace          clientKey = "trace"
	clientPubSub         clientKey = "pubsub"
	clientLogAdmin    clientKey = "logAdmin"
	clientCompute     clientKey = "compute"
	clientAPIGateway  clientKey = "apiGateway"
	clientMetric      clientKey = "metric"
	clientCRM         clientKey = "crm"
	clientBQ                 clientKey = "bq"
	clientGCS                clientKey = "gcs"
	clientSpanner            clientKey = "spanner"
	clientAlloyDB            clientKey = "alloydb"
	clientFirestore          clientKey = "firestore"
	clientMemorystore        clientKey = "memorystore"
	clientArtifactRegistry  clientKey = "artifactRegistry"
	clientCloudBuild         clientKey = "cloudBuild"
	clientServiceDirectory   clientKey = "serviceDirectory"
)

// moduleClientDeps maps each module name to the GCP clients it requires.
// "_resources" is a pseudo-module for always-on MCP Resources (BigQuery, Storage, IAM).
var moduleClientDeps = map[string][]clientKey{
	"gke":          {clientClusterMgr, clientMetric, clientLogAdmin}, // GetClusterBottlenecks fans out to metric+logAdmin
	"gke_workloads": {clientClusterMgr}, // dialK8s fetches cluster endpoint; K8s calls use thin HTTP client
	"gke_mesh":      {clientMetric, clientLogAdmin},             // Istio metrics primary; log-based fallback
	"networking":    {clientCompute, clientAPIGateway},          // LBs, URL maps, NEGs, VPC, PSC, API Gateway
	"cloudrun":   {clientRunSvc, clientRunJobs},
	"functions":  {clientFunctionsV1, clientRunSvc},
	"eventarc":      {clientEventarc},
	"scheduler":     {clientScheduler},
	"workflows":     {clientWorkflows, clientWorkflowExec},
	"tasks":         {clientTasks},
	"secretmanager": {clientSecretMgr, clientRunSvc},
	"vpcaccess":     {clientVPCAccess},
	"cloudsql":      {clientSQLAdmin},
	"pubsub":     {clientPubSub},
	"logging":    {clientLogAdmin},
	"monitoring": {clientMetric, clientTrace},
	"iam":        {clientCRM},
	"topology":   {clientRunSvc, clientPubSub}, // scans run annotations + pubsub push subscriptions
	"aura":       {clientMetric, clientRunSvc, clientClusterMgr}, // GKE cluster discovery + control-plane health
	"storage":     {clientGCS},
	"datastores":  {clientSpanner, clientAlloyDB, clientFirestore, clientMemorystore},
	"supplychain": {clientArtifactRegistry, clientCloudBuild, clientServiceDirectory},
	"_resources":  {clientBQ, clientGCS, clientCRM}, // always initialized for MCP Resources
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
