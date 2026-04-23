package models

// GetServiceTopologyRequest specifies which Cloud Run service to trace.
type GetServiceTopologyRequest struct {
	Project     string `json:"project"`
	Region      string `json:"region"`
	ServiceName string `json:"service_name"`
	// Depth controls how many hops to follow from the root service.
	// 1 = direct dependencies only; 2 = deps-of-deps. Capped at 2.
	Depth int `json:"depth"`
}

// TopologyNode is a resource vertex in the dependency graph.
type TopologyNode struct {
	ID     string `json:"id"`     // unique within the report, e.g. "cloudrun:my-api"
	Kind   string `json:"kind"`   // cloudrun_service | cloudsql_instance | pubsub_topic | gcs_bucket | secret | vpc_connector | external_db
	Name   string `json:"name"`
	Region string `json:"region,omitempty"`
	URL    string `json:"url,omitempty"`
}

// TopologyEdge is a directed relationship between two nodes.
type TopologyEdge struct {
	From         string `json:"from"`         // TopologyNode.ID
	To           string `json:"to"`           // TopologyNode.ID
	Relationship string `json:"relationship"` // connects_to_db | publishes_to | triggers | reads_secret | reads_writes_storage | network_via_vpc
	Evidence     string `json:"evidence"`     // e.g. "cloud_sql_annotation", "env_var:DATABASE_URL", "push_subscription:sub-name"
	Confidence   string `json:"confidence"`   // high | medium | low
}

// ServiceTopologyReport is the complete discovery result for a single root service.
type ServiceTopologyReport struct {
	RootService   string         `json:"root_service"`
	Project       string         `json:"project"`
	Depth         int            `json:"depth"`
	Nodes         []TopologyNode `json:"nodes"`
	Edges         []TopologyEdge `json:"edges"`
	// Relationships is a flat list of human-readable dependency statements,
	// optimised for LLM reasoning. Each entry has the form:
	//   "<from> -[<rel>]-> <to> (evidence: <e>, confidence: <c>)"
	Relationships []string `json:"relationships"`
	Warnings      []string `json:"warnings,omitempty"`
}
