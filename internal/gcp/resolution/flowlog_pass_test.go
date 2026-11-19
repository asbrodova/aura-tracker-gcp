package resolution

import (
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func gkeNode(id, kind, name, namespace, cluster string) models.GraphNode {
	return models.GraphNode{ID: id, Kind: kind, Name: name, Namespace: namespace, ClusterName: cluster, Region: "us-central1"}
}

func flowEndpoint(workload, service, namespace string) models.FlowEndpoint {
	return models.FlowEndpoint{Cluster: "prod", Location: "us-central1", Namespace: namespace, Workload: workload, Service: service}
}

func TestPass5_FlowLogsLinkWorkloadToService(t *testing.T) {
	out := Resolve(models.ResolutionInput{
		Nodes: []models.GraphNode{
			gkeNode("web", models.KindGKEDeployment, "web", "shop", "prod"),
			gkeNode("cart-svc", models.KindGKEService, "cart", "shop", "prod"),
			gkeNode("cart", models.KindGKEDeployment, "cart", "shop", "prod"),
		},
		FlowEdges: []models.FlowLogEdge{{
			Source: flowEndpoint("web", "", "shop"), Destination: flowEndpoint("cart", "cart", "shop"),
			Port: 8080, Protocol: 6, Records: 100, Bytes: 4096,
		}},
	})
	var flow *models.GraphEdge
	for i := range out.Edges {
		if out.Edges[i].Type == models.EdgeNetworkFlow {
			flow = &out.Edges[i]
		}
	}
	if flow == nil {
		t.Fatalf("no network_flow edge in %+v", out.Edges)
	}
	if flow.Source != "web" || flow.Target != "cart-svc" {
		t.Fatalf("edge %s -> %s, want web -> cart-svc (the Service is preferred over the workload)", flow.Source, flow.Target)
	}
	if flow.Evidence != models.EvidenceVPCFlowLog || flow.Confidence < 0.599 || flow.Confidence > 0.601 {
		t.Fatalf("edge = %+v, want vpc_flow_log evidence at the 0.60 ceiling", flow)
	}
	if flow.Metadata["port"] != "8080" || flow.Metadata["protocol"] != "tcp" || flow.Metadata["flow_records"] != "100" {
		t.Fatalf("metadata = %+v", flow.Metadata)
	}
}

func TestPass5_FlowLogConfidenceStaysBelowDiagramDefault(t *testing.T) {
	out := Resolve(models.ResolutionInput{
		Nodes: []models.GraphNode{
			gkeNode("a", models.KindGKEDeployment, "a", "ns", "prod"),
			gkeNode("b", models.KindGKEDeployment, "b", "ns", "prod"),
		},
		FlowEdges: []models.FlowLogEdge{{Source: flowEndpoint("a", "", "ns"), Destination: flowEndpoint("b", "", "ns"), Records: 1}},
	})
	if len(out.Edges) != 1 || out.Edges[0].Confidence >= 0.65 || out.Edges[0].Confidence < 0.45 {
		t.Fatalf("edges = %+v, want one edge between 0.45 and the 0.65 default threshold", out.Edges)
	}
}

func TestPass5_SkipsAmbiguousUnknownAndSelfFlows(t *testing.T) {
	out := Resolve(models.ResolutionInput{
		Nodes: []models.GraphNode{
			gkeNode("a1", models.KindGKEDeployment, "a", "ns", "prod"),
			gkeNode("a2", models.KindGKEStatefulSet, "a", "ns", "prod"),
			gkeNode("b", models.KindGKEDeployment, "b", "ns", "prod"),
		},
		FlowEdges: []models.FlowLogEdge{
			{Source: flowEndpoint("a", "", "ns"), Destination: flowEndpoint("b", "", "ns"), Records: 5},       // ambiguous source
			{Source: flowEndpoint("b", "", "ns"), Destination: flowEndpoint("missing", "", "ns"), Records: 5}, // unknown target
			{Source: flowEndpoint("b", "", "ns"), Destination: flowEndpoint("b", "", "ns"), Records: 5},       // self
			{Source: flowEndpoint("b", "", "other"), Destination: flowEndpoint("b", "", "ns"), Records: 5},    // wrong namespace
		},
	})
	if len(out.Edges) != 0 {
		t.Fatalf("unexpected edges %+v", out.Edges)
	}
}
