package diagram

import (
	"html"
	"sort"
	"strings"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

type renderGroup struct {
	id    string
	label string
	nodes []viewNode
}

func groupedNodes(nodes []viewNode) []renderGroup {
	groupsByID := make(map[string]*renderGroup)
	for _, node := range nodes {
		group := groupsByID[node.group]
		if group == nil {
			group = &renderGroup{id: node.group, label: node.groupLabel}
			groupsByID[node.group] = group
		}
		group.nodes = append(group.nodes, node)
	}
	groups := make([]renderGroup, 0, len(groupsByID))
	for _, group := range groupsByID {
		sort.Slice(group.nodes, func(i, j int) bool { return group.nodes[i].ID < group.nodes[j].ID })
		groups = append(groups, *group)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].id < groups[j].id })
	return groups
}

func nodeAliases(nodes []viewNode) map[string]string {
	aliases := make(map[string]string, len(nodes))
	for _, node := range nodes {
		aliases[node.ID] = node.alias
	}
	return aliases
}

func nodeCategory(kind string) string {
	switch kind {
	case models.KindCloudRunService, models.KindCloudRunJob, models.KindCloudFunctionV1,
		models.KindCloudFunctionV2, models.KindGKECluster, models.KindGKEDeployment,
		models.KindGKEStatefulSet, models.KindGKEDaemonSet, models.KindGKECronJob,
		models.KindGKEJob, models.KindGKEService:
		return "compute"
	case models.KindEventarcTrigger, models.KindSchedulerJob, models.KindWorkflow,
		models.KindTasksQueue, models.KindPubSubTopic, models.KindPubSubSubscription:
		return "event"
	case models.KindCloudSQLInstance, models.KindBigQueryDataset, models.KindSpannerInstance,
		models.KindAlloyDBCluster, models.KindFirestoreDatabase, models.KindMemorystoreInstance,
		models.KindSecret:
		return "data"
	case models.KindComputeLB, models.KindComputeURLMap, models.KindComputeNEG,
		models.KindAPIGateway, models.KindVPCNetwork, models.KindVPCSubnet,
		models.KindVPCConnector, models.KindPSCEndpoint, models.KindGKEIngress,
		models.KindGKEGateway:
		return "network"
	case models.KindIAMServiceAccount:
		return "identity"
	case models.KindExternalEndpoint:
		return "external"
	default:
		return "other"
	}
}

func nodeLabel(node viewNode) string {
	label := humanKind(node.Kind) + ": " + strings.TrimSpace(node.Name)
	if node.reason == reasonCrossEnvironment {
		label += " [cross-environment]"
	}
	return label
}

func humanKind(kind string) string {
	parts := strings.Split(kind, "_")
	for i, part := range parts {
		switch strings.ToLower(part) {
		case "gke":
			parts[i] = "GKE"
		case "vpc":
			parts[i] = "VPC"
		case "iam":
			parts[i] = "IAM"
		case "sql":
			parts[i] = "SQL"
		case "api":
			parts[i] = "API"
		default:
			if part != "" {
				parts[i] = strings.ToUpper(part[:1]) + part[1:]
			}
		}
	}
	return strings.Join(parts, " ")
}

func mermaidText(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, value)
	return html.EscapeString(strings.TrimSpace(value))
}

func dotText(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}
