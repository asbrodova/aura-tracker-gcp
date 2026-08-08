package diagram

import (
	"fmt"
	"sort"
	"strings"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

const (
	reasonSeed             = "seed"
	reasonDependency       = "dependency"
	reasonEntrypoint       = "entrypoint"
	reasonShared           = "shared"
	reasonCrossEnvironment = "cross_environment"
)

type viewNode struct {
	models.GraphNode
	alias      string
	reason     string
	depth      int
	group      string
	groupLabel string
}

type diagramView struct {
	nodes      []viewNode
	edges      []models.GraphEdge
	scope      models.DiagramScope
	stats      models.DiagramStats
	warnings   []string
	needsScope bool
}

func buildView(graph models.ServerlessGraph, req models.GenerateArchitectureDiagramRequest) diagramView {
	view := diagramView{
		stats: models.DiagramStats{NodesDiscovered: len(graph.Nodes), EdgesDiscovered: len(graph.Edges)},
		scope: models.DiagramScope{Mode: "environment", Environment: req.Environment},
	}
	if req.WholeProject {
		view.scope.Mode = "project"
		view.scope.Environment = ""
	}

	nodeByID := make(map[string]models.GraphNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodeByID[node.ID] = node
	}
	edges := make([]models.GraphEdge, 0, len(graph.Edges))
	for _, edge := range graph.Edges {
		if edge.Confidence >= req.MinConfidence && nodeByID[edge.Source].ID != "" && nodeByID[edge.Target].ID != "" {
			edges = append(edges, edge)
		}
	}

	type inclusion struct {
		reason string
		depth  int
	}
	included := make(map[string]inclusion, len(graph.Nodes))
	otherEnvironment := make(map[string]bool)
	candidates := make(map[string]bool)

	if req.WholeProject {
		for _, node := range graph.Nodes {
			included[node.ID] = inclusion{reason: reasonShared}
		}
	} else {
		for _, node := range graph.Nodes {
			matches, conflicts := matchesEnvironment(node, req.Environment, candidates)
			if conflicts {
				otherEnvironment[node.ID] = true
			}
			if matches && !conflicts {
				included[node.ID] = inclusion{reason: reasonSeed}
			}
		}
		if len(included) == 0 {
			view.needsScope = true
			view.scope.EnvironmentCandidates = sortedKeys(candidates)
			return view
		}

		adj := make(map[string][]models.GraphEdge, len(graph.Nodes))
		for _, edge := range edges {
			adj[edge.Source] = append(adj[edge.Source], edge)
			adj[edge.Target] = append(adj[edge.Target], edge)
		}
		queue := make([]string, 0, len(included))
		for id := range included {
			queue = append(queue, id)
		}
		sort.Strings(queue)
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			currentInclusion := included[current]
			if currentInclusion.depth >= req.MaxDepth {
				continue
			}
			for _, edge := range adj[current] {
				next := edge.Target
				reason := reasonDependency
				if next == current {
					next = edge.Source
					reason = reasonEntrypoint
				}
				if _, exists := included[next]; exists {
					continue
				}
				if otherEnvironment[next] {
					included[next] = inclusion{reason: reasonCrossEnvironment, depth: currentInclusion.depth + 1}
					continue
				}
				if reason == reasonDependency && !isDirectDependency(edge.Type) {
					reason = reasonShared
				}
				included[next] = inclusion{reason: reason, depth: currentInclusion.depth + 1}
				queue = append(queue, next)
			}
		}
	}

	selected := make([]viewNode, 0, len(included))
	for id, inc := range included {
		node := nodeByID[id]
		group, label := groupForNode(node, req.GroupBy)
		selected = append(selected, viewNode{GraphNode: node, reason: inc.reason, depth: inc.depth, group: group, groupLabel: label})
	}
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].depth != selected[j].depth {
			return selected[i].depth < selected[j].depth
		}
		if reasonRank(selected[i].reason) != reasonRank(selected[j].reason) {
			return reasonRank(selected[i].reason) < reasonRank(selected[j].reason)
		}
		return selected[i].ID < selected[j].ID
	})
	if len(selected) > req.MaxNodes {
		omitted := len(selected) - req.MaxNodes
		selected = selected[:req.MaxNodes]
		view.warnings = append(view.warnings, fmt.Sprintf("Diagram truncated: %d nodes omitted by max_nodes=%d.", omitted, req.MaxNodes))
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].ID < selected[j].ID })
	selectedIDs := make(map[string]bool, len(selected))
	for i := range selected {
		selected[i].alias = fmt.Sprintf("n%04d", i+1)
		selectedIDs[selected[i].ID] = true
		switch selected[i].reason {
		case reasonSeed:
			view.scope.SeedCount++
		case reasonDependency:
			view.scope.DependencyCount++
		case reasonEntrypoint:
			view.scope.EntrypointCount++
		case reasonCrossEnvironment:
			view.scope.CrossEnvironmentCount++
		default:
			view.scope.SharedCount++
		}
	}
	if view.scope.CrossEnvironmentCount > 0 {
		view.warnings = append(view.warnings, fmt.Sprintf("%d directly connected resources are labelled as another environment.", view.scope.CrossEnvironmentCount))
	}
	for _, edge := range edges {
		if selectedIDs[edge.Source] && selectedIDs[edge.Target] {
			view.edges = append(view.edges, edge)
		}
	}
	sort.Slice(view.edges, func(i, j int) bool {
		if view.edges[i].Source != view.edges[j].Source {
			return view.edges[i].Source < view.edges[j].Source
		}
		if view.edges[i].Target != view.edges[j].Target {
			return view.edges[i].Target < view.edges[j].Target
		}
		return view.edges[i].Type < view.edges[j].Type
	})
	view.nodes = selected
	view.stats.NodesRendered = len(selected)
	view.stats.EdgesRendered = len(view.edges)
	view.stats.NodesOmitted = len(graph.Nodes) - len(selected)
	return view
}

func matchesEnvironment(node models.GraphNode, environment string, candidates map[string]bool) (matches, conflicts bool) {
	wanted := environmentAliases(environment)
	foundEnvironmentLabel := false
	for key, value := range node.Labels {
		key = strings.ToLower(strings.TrimSpace(key))
		if key != "env" && key != "environment" && key != "stage" {
			continue
		}
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		foundEnvironmentLabel = true
		candidates[value] = true
		if wanted[value] {
			matches = true
		} else {
			conflicts = true
		}
	}
	if node.Namespace != "" {
		namespace := strings.ToLower(strings.TrimSpace(node.Namespace))
		candidates[namespace] = true
		if !foundEnvironmentLabel && wanted[namespace] {
			matches = true
		}
	}
	return matches, conflicts
}

func environmentAliases(environment string) map[string]bool {
	if environment == "production" || environment == "prod" {
		return map[string]bool{"production": true, "prod": true}
	}
	return map[string]bool{environment: true}
}

func isDirectDependency(edgeType string) bool {
	switch edgeType {
	case models.EdgeReadsSecret, models.EdgeReadsFrom, models.EdgeWritesTo,
		models.EdgeReadsFromDB, models.EdgePublishesTo, models.EdgeConnectedViaVPC,
		models.EdgeBoundToSA, models.EdgeInNetwork, models.EdgeTraceCalls, models.EdgeMeshCalls:
		return true
	default:
		return false
	}
}

func reasonRank(reason string) int {
	switch reason {
	case reasonSeed:
		return 0
	case reasonEntrypoint:
		return 1
	case reasonDependency:
		return 2
	case reasonShared:
		return 3
	default:
		return 4
	}
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func groupForNode(node models.GraphNode, groupBy string) (string, string) {
	switch groupBy {
	case "none":
		return "", ""
	case "namespace":
		if node.Namespace != "" {
			return "namespace:" + node.ClusterName + "/" + node.Namespace, node.ClusterName + " / " + node.Namespace
		}
	case "cluster":
		if node.ClusterName != "" {
			return "cluster:" + node.ClusterName, node.ClusterName
		}
	case "region":
		if node.Region != "" && node.Region != "-" {
			return "region:" + node.Region, node.Region
		}
	case "auto":
		if node.Namespace != "" {
			return "namespace:" + node.ClusterName + "/" + node.Namespace, node.ClusterName + " / " + node.Namespace
		}
		if node.ClusterName != "" {
			return "cluster:" + node.ClusterName, node.ClusterName
		}
		if node.Region != "" && node.Region != "-" {
			return "region:" + node.Region, node.Region
		}
	}
	return "global", "Global"
}
