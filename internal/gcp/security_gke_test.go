package gcp

import (
	"testing"

	containerpb "cloud.google.com/go/container/apiv1/containerpb"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func TestPublicGKEServiceFactCorrelatesSelectorIdentity(t *testing.T) {
	cluster := &containerpb.Cluster{Name: "prod", Location: "us-central1"}
	service := models.GKEServiceSummary{
		Name: "api", Namespace: "payments", Type: "LoadBalancer", Selector: map[string]string{"app": "api"},
		LoadBalancerAddresses: []string{"34.1.2.3"}, Ports: []models.GKEServicePort{{Port: 443}},
	}
	workloads := []models.GKEWorkloadSummary{{Namespace: "payments", Name: "api", ServiceAccount: "api-ksa", Labels: map[string]string{"app": "api"}}}

	fact, ok := publicGKEServiceFact("project", cluster, service, workloads)
	if !ok || !fact.External {
		t.Fatalf("expected public service fact: %#v", fact)
	}
	if !containsString(fact.WorkloadIdentities, "payments/api-ksa") {
		t.Fatalf("workload identities = %v", fact.WorkloadIdentities)
	}
	if len(fact.SourceRanges) != 2 || fact.Ports[0] != 443 {
		t.Fatalf("source ranges=%v ports=%v", fact.SourceRanges, fact.Ports)
	}
}

func TestPublicGKEIngressFactUsesExternalGatewayTLS(t *testing.T) {
	cluster := &containerpb.Cluster{Name: "prod", Location: "us-central1"}
	ingress := models.GKEIngressSummary{
		Name: "route", Namespace: "payments", Kind: "HTTPRoute", ParentGateways: []string{"payments/public"},
		Hosts: []string{"api.example.com"}, Rules: []models.GKEIngressRule{{Paths: []models.GKEIngressPath{{BackendName: "api"}}}},
	}
	gateway := k8sGateway{Metadata: k8sMeta{Name: "public", Namespace: "payments"}}
	gateway.Spec.GatewayClassName = "gke-l7-global-external-managed"
	gateway.Spec.Listeners = []k8sGatewayListener{{Protocol: "HTTPS", Port: 443}}
	gateway.Status.Addresses = []k8sGatewayAddress{{Value: "34.1.2.3"}}

	fact, ok := publicGKEIngressFact("project", cluster, ingress, map[string]k8sGateway{"payments/public": gateway}, nil)
	if !ok || !fact.External || !fact.TLSEnabled {
		t.Fatalf("expected TLS public route: %#v", fact)
	}
	if !containsString(fact.BackendServices, "api") {
		t.Fatalf("backends = %v", fact.BackendServices)
	}
}

func TestBoundedSecurityResourcesMarksTruncation(t *testing.T) {
	values, status, message := boundedSecurityResources([]int{1, 2, 3}, nil, 2)
	if len(values) != 2 || status != "truncated" || message == "" {
		t.Fatalf("values=%v status=%q message=%q", values, status, message)
	}
}
