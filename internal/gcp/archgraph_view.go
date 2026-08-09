package gcp

import (
	"fmt"
	"sort"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// applyArchitectureGraphView applies presentation-only filters to an immutable
// cached graph and preserves referential integrity between nodes, edges, and
// groups. Collection-affecting options belong in archGraphCacheKey instead.
func applyArchitectureGraphView(graph models.ServerlessGraph, req models.ExportArchitectureGraphRequest) models.ServerlessGraph {
	regionSet := make(map[string]bool, len(req.Regions))
	for _, region := range req.Regions {
		if region != "" && region != "-" {
			regionSet[region] = true
		}
	}

	nodes := make([]models.GraphNode, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if !req.IncludeExternal && node.Kind == models.KindExternalEndpoint {
			continue
		}
		if len(regionSet) > 0 && node.Region != "" && node.Region != "-" && !regionSet[node.Region] {
			continue
		}
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	allowed := nodeIDSet(nodes)
	edges := filterGraphEdges(graph.Edges, allowed)
	if req.MaxDepth > 0 {
		allowed = nodesWithinDepth(nodes, edges, req.MaxDepth)
		filtered := nodes[:0]
		for _, node := range nodes {
			if allowed[node.ID] {
				filtered = append(filtered, node)
			}
		}
		nodes = filtered
		edges = filterGraphEdges(edges, allowed)
	}

	truncated := false
	if req.MaxNodes > 0 && len(nodes) > req.MaxNodes {
		nodes = nodes[:req.MaxNodes]
		allowed = nodeIDSet(nodes)
		edges = filterGraphEdges(edges, allowed)
		truncated = true
	}

	graph.Nodes = nodes
	graph.Edges = edges
	graph.Groups = buildArchGroups(graph.ProjectID, nodes)
	graph.Truncated = truncated
	if truncated {
		graph.Errors = append(graph.Errors, models.ToolError{
			FailingAPI: "gcp_export_architecture_graph",
			Message:    fmt.Sprintf("result truncated: max_nodes=%d reached", req.MaxNodes),
		})
	}
	if len(req.Regions) > 0 {
		graph.RegionsScanned = append([]string(nil), req.Regions...)
		sort.Strings(graph.RegionsScanned)
	}
	return graph
}

func nodeIDSet(nodes []models.GraphNode) map[string]bool {
	ids := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		ids[node.ID] = true
	}
	return ids
}

func filterGraphEdges(edges []models.GraphEdge, allowed map[string]bool) []models.GraphEdge {
	filtered := make([]models.GraphEdge, 0, len(edges))
	for _, edge := range edges {
		if allowed[edge.Source] && allowed[edge.Target] {
			filtered = append(filtered, edge)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Source != filtered[j].Source {
			return filtered[i].Source < filtered[j].Source
		}
		if filtered[i].Target != filtered[j].Target {
			return filtered[i].Target < filtered[j].Target
		}
		return filtered[i].Type < filtered[j].Type
	})
	return filtered
}

// nodesWithinDepth applies the depth cap independently to each weakly connected
// component. Nodes without incoming edges are entrypoints; a component made
// entirely of cycles gets one deterministic synthetic root. Nodes beyond the
// cap are not promoted to new roots.
func nodesWithinDepth(nodes []models.GraphNode, edges []models.GraphEdge, maxDepth int) map[string]bool {
	incoming := make(map[string]int, len(nodes))
	adj := make(map[string][]string, len(nodes))
	undirected := make(map[string][]string, len(nodes))
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		incoming[node.ID] = 0
		ids = append(ids, node.ID)
	}
	for _, edge := range edges {
		incoming[edge.Target]++
		adj[edge.Source] = append(adj[edge.Source], edge.Target)
		undirected[edge.Source] = append(undirected[edge.Source], edge.Target)
		undirected[edge.Target] = append(undirected[edge.Target], edge.Source)
	}
	sort.Strings(ids)
	for source := range adj {
		sort.Strings(adj[source])
	}
	for nodeID := range undirected {
		sort.Strings(undirected[nodeID])
	}

	type visit struct {
		id    string
		depth int
	}
	allowed := make(map[string]bool, len(nodes))
	componentSeen := make(map[string]bool, len(nodes))
	for _, seed := range ids {
		if componentSeen[seed] {
			continue
		}
		component := []string{seed}
		componentSeen[seed] = true
		for i := 0; i < len(component); i++ {
			for _, neighbor := range undirected[component[i]] {
				if !componentSeen[neighbor] {
					componentSeen[neighbor] = true
					component = append(component, neighbor)
				}
			}
		}
		sort.Strings(component)

		queue := make([]visit, 0, len(component))
		distance := make(map[string]int, len(component))
		for _, nodeID := range component {
			if incoming[nodeID] == 0 {
				queue = append(queue, visit{id: nodeID})
				distance[nodeID] = 0
			}
		}
		if len(queue) == 0 {
			queue = append(queue, visit{id: component[0]})
			distance[component[0]] = 0
		}
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			allowed[current.id] = true
			if current.depth >= maxDepth {
				continue
			}
			for _, target := range adj[current.id] {
				if _, seen := distance[target]; seen {
					continue
				}
				distance[target] = current.depth + 1
				queue = append(queue, visit{id: target, depth: current.depth + 1})
			}
		}
	}
	return allowed
}
