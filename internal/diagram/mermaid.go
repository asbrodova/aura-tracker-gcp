package diagram

import (
	"fmt"
	"strings"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func renderMermaid(view diagramView, req models.GenerateArchitectureDiagramRequest) string {
	var out strings.Builder
	fmt.Fprintf(&out, "flowchart %s\n", req.Direction)
	groups := groupedNodes(view.nodes)
	for i, group := range groups {
		if group.id != "" {
			fmt.Fprintf(&out, "  subgraph g%04d[\"%s\"]\n", i+1, mermaidText(group.label))
		}
		indent := "  "
		if group.id != "" {
			indent = "    "
		}
		for _, node := range group.nodes {
			fmt.Fprintf(&out, "%s%s[\"%s\"]\n", indent, node.alias, mermaidText(nodeLabel(node)))
		}
		if group.id != "" {
			out.WriteString("  end\n")
		}
	}

	aliases := nodeAliases(view.nodes)
	var dashed []int
	for i, edge := range view.edges {
		fmt.Fprintf(&out, "  %s -->|\"%s\"| %s\n", aliases[edge.Source], mermaidText(humanKind(edge.Type)), aliases[edge.Target])
		if edge.Confidence < 0.80 {
			dashed = append(dashed, i)
		}
	}
	out.WriteString("  classDef compute fill:#dbeafe,stroke:#2563eb,color:#172554\n")
	out.WriteString("  classDef event fill:#ffedd5,stroke:#ea580c,color:#431407\n")
	out.WriteString("  classDef data fill:#dcfce7,stroke:#16a34a,color:#052e16\n")
	out.WriteString("  classDef network fill:#f3e8ff,stroke:#9333ea,color:#3b0764\n")
	out.WriteString("  classDef identity fill:#f1f5f9,stroke:#64748b,color:#0f172a\n")
	out.WriteString("  classDef external fill:#fee2e2,stroke:#dc2626,color:#450a0a\n")
	out.WriteString("  classDef other fill:#f8fafc,stroke:#94a3b8,color:#0f172a\n")
	for _, node := range view.nodes {
		fmt.Fprintf(&out, "  class %s %s\n", node.alias, nodeCategory(node.Kind))
	}
	for _, index := range dashed {
		fmt.Fprintf(&out, "  linkStyle %d stroke-dasharray: 5 5\n", index)
	}
	return strings.TrimSpace(out.String())
}
