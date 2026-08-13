package mcp

import "sort"

// resourcePermissions are required by the always-on MCP resources, regardless
// of the selected tool modules.
var resourcePermissions = []string{
	"bigquery.datasets.get",
	"bigquery.tables.get",
	"bigquery.tables.list",
	"resourcemanager.projects.get",
	"run.revisions.list",
	"run.services.get",
	"run.services.list",
	"storage.buckets.get",
	"storage.buckets.list",
	"storage.objects.list",
}

var (
	permissionsGKE = []string{
		"container.clusters.get", "container.clusters.list", "container.clusters.update",
	}
	permissionsRun = []string{
		"run.executions.list", "run.jobs.get", "run.jobs.list", "run.revisions.list",
		"run.services.get", "run.services.list", "run.services.update",
	}
	permissionsPubSub = []string{
		"pubsub.subscriptions.get", "pubsub.subscriptions.list",
		"pubsub.topics.get", "pubsub.topics.list",
	}
	permissionsObservability = []string{
		"logging.logEntries.list", "monitoring.timeSeries.list",
	}
	permissionsMonitoring = []string{
		"cloudtrace.traces.list", "monitoring.alertPolicies.list", "monitoring.dashboards.list",
		"monitoring.metricDescriptors.list", "monitoring.services.list",
		"monitoring.slos.list", "monitoring.timeSeries.list", "monitoring.uptimeCheckConfigs.list",
	}
	permissionsIAM = []string{
		"iam.serviceAccounts.list", "resourcemanager.projects.get", "resourcemanager.projects.getIamPolicy",
	}
	permissionsFunctions = []string{
		"cloudfunctions.functions.get", "cloudfunctions.functions.list", "run.services.get", "run.services.list",
	}
	permissionsRegionalServerless = []string{
		"cloudscheduler.jobs.list", "cloudtasks.queues.list", "eventarc.triggers.get", "eventarc.triggers.list",
		"vpcaccess.connectors.list", "workflows.executions.list", "workflows.workflows.get", "workflows.workflows.list",
	}
	permissionsNetworking = []string{
		"apigateway.gateways.list", "compute.forwardingRules.list", "compute.globalForwardingRules.list",
		"compute.networkEndpointGroups.list", "compute.networks.list", "compute.serviceAttachments.list",
		"compute.subnetworks.list", "compute.urlMaps.list",
	}
	permissionsDatastores = []string{
		"alloydb.clusters.list", "datastore.databases.list", "redis.instances.list", "spanner.instances.list",
	}
	permissionsSupplyChain = []string{
		"artifactregistry.dockerimages.list", "artifactregistry.repositories.list",
		"cloudbuild.builds.list", "servicedirectory.namespaces.list", "servicedirectory.services.list",
	}
	permissionsSecurity = []string{
		"cloudasset.assets.searchAllIamPolicies", "cloudasset.assets.searchAllResources",
		"compute.firewallPolicies.get", "compute.firewalls.list", "compute.networks.getEffectiveFirewalls",
		"gkehub.memberships.list", "iam.denypolicies.list", "iam.serviceAccountKeys.list",
		"iam.serviceAccounts.getIamPolicy", "resourcemanager.folders.getIamPolicy",
		"resourcemanager.organizations.getIamPolicy", "resourcemanager.tagBindings.list",
		"secretmanager.secrets.list", "secretmanager.versions.list",
	}
	permissionsCost = []string{
		"bigquery.datasets.get", "bigquery.jobs.create", "bigquery.tables.get", "bigquery.tables.getData",
		"cloudasset.assets.searchAllResources", "serviceusage.services.use",
	}
)

// modulePermissions is the capability catalog used by the IAM resource. Each
// registered module must have an entry, including aggregate graph/drift modules
// whose dependencies span multiple products.
var modulePermissions = map[string][]string{
	ModuleGKE:             mergePermissionLists(permissionsGKE, permissionsObservability),
	ModuleCloudRun:        permissionsRun,
	ModulePubSub:          mergePermissionLists(permissionsPubSub, []string{"monitoring.timeSeries.list"}),
	ModuleLogging:         {"logging.logEntries.list"},
	ModuleMonitoring:      permissionsMonitoring,
	ModuleIAM:             permissionsIAM,
	ModuleTopology:        mergePermissionLists([]string{"run.services.get"}, permissionsPubSub),
	ModuleAura:            mergePermissionLists(permissionsGKE, permissionsRun, permissionsObservability, []string{"bigquery.datasets.get", "storage.buckets.get"}),
	ModuleStorage:         {"storage.buckets.get", "storage.buckets.list", "storage.objects.list"},
	ModuleFunctions:       permissionsFunctions,
	ModuleEventarc:        {"eventarc.triggers.get", "eventarc.triggers.list"},
	ModuleScheduler:       {"cloudscheduler.jobs.list"},
	ModuleWorkflows:       {"workflows.executions.list", "workflows.workflows.get", "workflows.workflows.list"},
	ModuleTasks:           {"cloudtasks.queues.list"},
	ModuleSecretManager:   {"run.services.list", "secretmanager.secrets.list"},
	ModuleVPCAccess:       {"vpcaccess.connectors.list"},
	ModuleCloudSQL:        {"cloudsql.instances.get", "cloudsql.instances.list"},
	ModuleServerlessGraph: mergePermissionLists(permissionsRun, permissionsPubSub, permissionsFunctions, permissionsRegionalServerless, []string{"cloudsql.instances.list", "secretmanager.secrets.list"}),
	ModuleGKEWorkloads:    {"container.clusters.get", "container.clusters.list"},
	ModuleGKEMesh:         permissionsObservability,
	ModuleNetworking:      permissionsNetworking,
	ModuleDatastores:      permissionsDatastores,
	ModuleSupplyChain:     permissionsSupplyChain,
	ModuleCoverage:        mergePermissionLists(permissionsMonitoring, []string{"logging.logEntries.list", "run.services.list"}),
	ModuleArchGraph: mergePermissionLists(
		permissionsGKE, permissionsRun, permissionsPubSub, permissionsFunctions, permissionsRegionalServerless,
		permissionsNetworking, permissionsDatastores, permissionsSupplyChain, permissionsIAM, permissionsObservability,
		[]string{"cloudsql.instances.list", "secretmanager.secrets.list"},
	),
	ModuleTagging:  {"resourcemanager.tagBindings.list"},
	ModuleIncident: mergePermissionLists(permissionsRun, permissionsPubSub, permissionsObservability, []string{"cloudsql.instances.list", "vpcaccess.connectors.list"}),
	ModuleCost:     permissionsCost,
	ModuleSecurity: mergePermissionLists(permissionsSecurity, permissionsGKE, permissionsFunctions, []string{"run.services.list"}),
	ModuleDrift: mergePermissionLists(
		permissionsGKE, permissionsRun, permissionsPubSub, permissionsFunctions, permissionsRegionalServerless,
		permissionsNetworking, permissionsDatastores, permissionsSupplyChain, permissionsIAM,
		[]string{"bigquery.datasets.get", "cloudsql.instances.list", "monitoring.alertPolicies.list", "monitoring.dashboards.list", "secretmanager.secrets.list", "storage.buckets.list"},
	),
	ModuleRecommenderExport: mergePermissionLists([]string{"bigquery.datasets.get", "bigquery.jobs.create", "bigquery.tables.get", "bigquery.tables.getData"}, recommenderPermissions()),
}

func recommenderPermissions() []string {
	return []string{
		"recommender.cloudsqlIdleInstanceRecommendations.list",
		"recommender.cloudsqlOverprovisionedInstanceRecommendations.list",
		"recommender.computeAddressIdleResourceRecommendations.list",
		"recommender.computeDiskIdleResourceRecommendations.list",
		"recommender.computeIdleResourceRecommendations.list",
		"recommender.computeImageIdleResourceRecommendations.list",
		"recommender.computeInstanceIdleResourceRecommendations.list",
		"recommender.containerDiagnosisRecommendations.list",
		"recommender.runServiceCostRecommendations.list",
	}
}

func permissionsForModules(enabled map[string]bool) []string {
	lists := [][]string{resourcePermissions}
	for module := range enabled {
		lists = append(lists, modulePermissions[module])
	}
	// Aura and cost analysis optionally consume recommendations; probing these
	// permissions makes that degraded coverage visible without claiming it is
	// required for the base score.
	if enabled[ModuleAura] || enabled[ModuleCost] {
		lists = append(lists, recommenderPermissions())
	}
	permissions := mergePermissionLists(lists...)
	sort.Strings(permissions)
	return permissions
}

func mergePermissionLists(lists ...[]string) []string {
	seen := make(map[string]struct{})
	for _, list := range lists {
		for _, permission := range list {
			if permission != "" {
				seen[permission] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for permission := range seen {
		out = append(out, permission)
	}
	return out
}

func activeModuleSet(modules []ToolModule, enabled map[string]bool) map[string]bool {
	active := make(map[string]bool)
	for _, module := range modules {
		if enabled == nil || enabled[module.Name()] {
			active[module.Name()] = true
		}
	}
	return active
}
