package models

const (
	DiagramFormatMermaid  = "mermaid"
	DiagramFormatGraphviz = "graphviz"
	DiagramFormatSVG      = "svg"

	DiagramStatusComplete   = "complete"
	DiagramStatusPartial    = "partial"
	DiagramStatusNeedsScope = "needs_scope"
	DiagramStatusEmpty      = "empty"
)

// GenerateArchitectureDiagramRequest controls project graph scoping and
// presentation. Zero values select a production-scoped Mermaid diagram.
type GenerateArchitectureDiagramRequest struct {
	ProjectID       string   `json:"project_id"`
	Environment     string   `json:"environment,omitempty"`
	WholeProject    bool     `json:"whole_project,omitempty"`
	Format          string   `json:"format,omitempty"`
	Regions         []string `json:"regions,omitempty"`
	Direction       string   `json:"direction,omitempty"`
	GroupBy         string   `json:"group_by,omitempty"`
	MaxDepth        int      `json:"max_depth,omitempty"`
	MaxNodes        int      `json:"max_nodes,omitempty"`
	MinConfidence   float64  `json:"min_confidence,omitempty"`
	IncludeExternal bool     `json:"include_external,omitempty"`
	LookbackHours   int      `json:"lookback_hours,omitempty"`
}

type DiagramScope struct {
	Mode                  string   `json:"mode"`
	Environment           string   `json:"environment,omitempty"`
	SeedCount             int      `json:"seed_count"`
	DependencyCount       int      `json:"dependency_count"`
	EntrypointCount       int      `json:"entrypoint_count"`
	SharedCount           int      `json:"shared_count"`
	CrossEnvironmentCount int      `json:"cross_environment_count"`
	EnvironmentCandidates []string `json:"environment_candidates,omitempty"`
}

type DiagramStats struct {
	NodesDiscovered int `json:"nodes_discovered"`
	EdgesDiscovered int `json:"edges_discovered"`
	NodesRendered   int `json:"nodes_rendered"`
	EdgesRendered   int `json:"edges_rendered"`
	NodesOmitted    int `json:"nodes_omitted"`
}

// ArchitectureDiagramResponse is the structured result returned alongside a
// display-ready Mermaid, Graphviz, or SVG content block.
type ArchitectureDiagramResponse struct {
	Status           string       `json:"status"`
	Format           string       `json:"format"`
	MIMEType         string       `json:"mime_type"`
	Source           string       `json:"source,omitempty"`
	Scope            DiagramScope `json:"scope"`
	Stats            DiagramStats `json:"stats"`
	Warnings         []string     `json:"warnings,omitempty"`
	CollectionErrors []ToolError  `json:"collection_errors,omitempty"`
	GeneratedAt      string       `json:"generated_at,omitempty"`
}
