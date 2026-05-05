package models

// GraphNode is a resource node in the Phase 1 serverless graph.
// ID is a stable URN: urn:gcp:<service>:<region|"-">:<project_id>:<kind>/<name>[/<sub>...]
type GraphNode struct {
	ID             string            `json:"id"`
	Kind           string            `json:"kind"`
	Name           string            `json:"name"`
	Region         string            `json:"region,omitempty"`
	ProjectID      string            `json:"project_id"`
	Labels         map[string]string `json:"labels,omitempty"`
	URL            string            `json:"url,omitempty"`
	Generation     int               `json:"generation,omitempty"`
	WrapsFunction  string            `json:"wraps_function,omitempty"`
	EventarcOwned  bool              `json:"eventarc_owned,omitempty"`
	ServiceAccount string            `json:"service_account,omitempty"`
}

// GraphEdge is a directed relationship between two GraphNodes.
type GraphEdge struct {
	Source     string            `json:"source"`
	Target     string            `json:"target"`
	Type       string            `json:"type"`
	Evidence   string            `json:"evidence"`
	Confidence float64           `json:"confidence"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// GraphGroup is a swimlane container (project, region, or network).
type GraphGroup struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Label   string   `json:"label"`
	Members []string `json:"members"`
}

// ServerlessGraph is the full Phase 1 graph slice returned by
// gcp_export_serverless_graph. Errors is non-empty on partial failure.
type ServerlessGraph struct {
	Nodes  []GraphNode  `json:"nodes"`
	Edges  []GraphEdge  `json:"edges"`
	Groups []GraphGroup `json:"groups"`
	Errors []ToolError  `json:"errors,omitempty"`
}

// ExportServerlessGraphRequest is the input for gcp_export_serverless_graph.
type ExportServerlessGraphRequest struct {
	ProjectID         string `json:"project_id"`
	Region            string `json:"region,omitempty"`
	MaxNodes          int    `json:"max_nodes,omitempty"`
	IncludeReferences bool   `json:"include_references,omitempty"`
}

// PartialResult wraps a list of results alongside any non-fatal errors that
// occurred during a multi-region fan-out. Callers should treat a non-empty
// Errors slice as a partial-success signal, not a hard failure.
type PartialResult[T any] struct {
	Results []T         `json:"results"`
	Errors  []ToolError `json:"errors,omitempty"`
}
