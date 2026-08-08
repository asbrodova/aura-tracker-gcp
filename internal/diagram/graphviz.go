package diagram

import (
	"fmt"
	"strings"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

var dotCategoryStyles = map[string]string{
	"compute":  `shape="box",style="rounded,filled",fillcolor="#dbeafe",color="#2563eb"`,
	"event":    `shape="hexagon",style="filled",fillcolor="#ffedd5",color="#ea580c"`,
	"data":     `shape="cylinder",style="filled",fillcolor="#dcfce7",color="#16a34a"`,
	"network":  `shape="diamond",style="filled",fillcolor="#f3e8ff",color="#9333ea"`,
	"identity": `shape="ellipse",style="filled",fillcolor="#f1f5f9",color="#64748b"`,
	"external": `shape="box",style="dashed,filled",fillcolor="#fee2e2",color="#dc2626"`,
	"other":    `shape="box",style="filled",fillcolor="#f8fafc",color="#94a3b8"`,
}

func renderGraphviz(view diagramView, req models.GenerateArchitectureDiagramRequest) string {
	var out strings.Builder
	out.WriteString("digraph architecture {\n")
	fmt.Fprintf(&out, "  graph [rankdir=%s,compound=true,bgcolor=\"transparent\"];\n", req.Direction)
	out.WriteString("  node [fontname=\"Arial\",fontsize=10];\n")
	out.WriteString("  edge [fontname=\"Arial\",fontsize=9,color=\"#64748b\"];\n")
	groups := groupedNodes(view.nodes)
	for i, group := range groups {
		indent := "  "
		if group.id != "" {
			fmt.Fprintf(&out, "  subgraph cluster_g%04d {\n", i+1)
			fmt.Fprintf(&out, "    label=\"%s\"; color=\"#cbd5e1\";\n", dotText(group.label))
			indent = "    "
		}
		for _, node := range group.nodes {
			style := dotCategoryStyles[nodeCategory(node.Kind)]
			fmt.Fprintf(&out, "%s%s [label=\"%s\",%s];\n", indent, node.alias, dotText(nodeLabel(node)), style)
		}
		if group.id != "" {
			out.WriteString("  }\n")
		}
	}
	aliases := nodeAliases(view.nodes)
	for _, edge := range view.edges {
		style := "solid"
		if edge.Confidence < 0.80 {
			style = "dashed"
		}
		fmt.Fprintf(&out, "  %s -> %s [label=\"%s\",style=\"%s\"];\n", aliases[edge.Source], aliases[edge.Target], dotText(humanKind(edge.Type)), style)
	}
	out.WriteString("}\n")
	return strings.TrimSpace(out.String())
}
