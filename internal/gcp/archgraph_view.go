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

// nodesWithinDepth treats nodes without incoming edges as entrypoints and keeps
// nodes reachable within maxDepth. Strongly connected components with no such
// entrypoint are retained as roots so a cyclic topology is never erased.
func nodesWithinDepth(nodes []models.GraphNode, edges []models.GraphEdge, maxDepth int) map[string]bool {
	incoming := make(map[string]int, len(nodes))
	adj := make(map[string][]string, len(nodes))
	for _, node := range nodes {
		incoming[node.ID] = 0
	}
	for _, edge := range edges {
		incoming[edge.Target]++
		adj[edge.Source] = append(adj[edge.Source], edge.Target)
	}
	for source := range adj {
		sort.Strings(adj[source])
	}

	type visit struct {
		id    string
		depth int
	}
	queue := make([]visit, 0, len(nodes))
	for _, node := range nodes {
		if incoming[node.ID] == 0 {
			queue = append(queue, visit{id: node.ID})
		}
	}
	if len(queue) == 0 {
		for _, node := range nodes {
			queue = append(queue, visit{id: node.ID})
		}
	}
	seen := make(map[string]bool, len(nodes))
	for {
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			if seen[current.id] {
				continue
			}
			seen[current.id] = true
			if current.depth >= maxDepth {
				continue
			}
			for _, target := range adj[current.id] {
				queue = append(queue, visit{id: target, depth: current.depth + 1})
			}
		}
		var disconnectedRoot string
		for _, node := range nodes {
			if !seen[node.ID] {
				disconnectedRoot = node.ID
				break
			}
		}
		if disconnectedRoot == "" {
			break
		}
		queue = append(queue, visit{id: disconnectedRoot})
	}
	return seen
}
