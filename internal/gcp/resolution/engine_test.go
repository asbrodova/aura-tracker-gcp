package resolution_test

import (
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/internal/gcp/resolution"
	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func node(id, kind, name, projectID string) models.GraphNode {
	return models.GraphNode{ID: id, Kind: kind, Name: name, ProjectID: projectID}
}

func findEdge(edges []models.GraphEdge, src, tgt, edgeType string) *models.GraphEdge {
	for i := range edges {
		e := &edges[i]
		if e.Source == src && e.Target == tgt && e.Type == edgeType {
			return e
		}
	}
	return nil
}

// ─── Pass 1 ───────────────────────────────────────────────────────────────────

func TestPass1_SecretRef(t *testing.T) {
	depNode := node("urn:gcp:gke:-:proj:gke_deployment/myapp", models.KindGKEDeployment, "myapp", "proj")
	secretNode := node("urn:gcp:secretmanager:-:proj:secret/db-password", models.KindSecret, "db-password", "proj")

	out := resolution.Resolve(models.ResolutionInput{
		Nodes: []models.GraphNode{depNode, secretNode},
		Workloads: []models.GKEWorkloadSummary{
			{
				Name:       "myapp",
				Namespace:  "default",
				Kind:       models.KindGKEDeployment,
				SecretRefs: []string{"db-password"},
			},
		},
	})

	e := findEdge(out.Edges, depNode.ID, secretNode.ID, models.EdgeReadsSecret)
	if e == nil {
		t.Fatalf("expected reads_secret edge, got %v", out.Edges)
	}
	if e.Evidence != models.EvidenceK8sSpec {
		t.Errorf("expected evidence %q, got %q", models.EvidenceK8sSpec, e.Evidence)
	}
	if e.Confidence < 0.9 {
		t.Errorf("expected confidence ≥ 0.9, got %f", e.Confidence)
	}
}

func TestPass1_K8sServiceSelector(t *testing.T) {
	svcNode := node("urn:gcp:k8s:-:proj:gke_service/web-svc", models.KindGKEService, "web-svc", "proj")
	depNode := node("urn:gcp:k8s:-:proj:gke_deployment/web", models.KindGKEDeployment, "web", "proj")

	out := resolution.Resolve(models.ResolutionInput{
		Nodes: []models.GraphNode{svcNode, depNode},
		K8sServices: []models.GKEServiceSummary{
			{
				Name:      "web-svc",
				Namespace: "default",
				Selector:  map[string]string{"app": "web"},
			},
		},
		Workloads: []models.GKEWorkloadSummary{
			{
				Name:      "web",
				Namespace: "default",
				Kind:      models.KindGKEDeployment,
				Labels:    map[string]string{"app": "web", "version": "v1"},
			},
		},
	})

	e := findEdge(out.Edges, svcNode.ID, depNode.ID, models.EdgeExposes)
	if e == nil {
		t.Fatalf("expected exposes edge, got %v", out.Edges)
	}
}

func TestPass1_NEGAnnotation(t *testing.T) {
	svcNode := node("svc-id", models.KindGKEService, "api-svc", "proj")
	negNode := node("neg-id", models.KindComputeNEG, "api-neg", "proj")

	out := resolution.Resolve(models.ResolutionInput{
		Nodes: []models.GraphNode{svcNode, negNode},
		K8sServices: []models.GKEServiceSummary{
			{
				Name:          "api-svc",
				Namespace:     "default",
				NEGAnnotation: `{"ingress":true,"exposed_ports":{"80":{"name":"api-neg"}}}`,
			},
		},
	})

	e := findEdge(out.Edges, svcNode.ID, negNode.ID, models.EdgeLoadBalancedBy)
	if e == nil {
		t.Fatalf("expected load_balanced_by edge, got %v", out.Edges)
	}
}

func TestPass1_PubSubPushEndpoint(t *testing.T) {
	subNode := node("sub-id", models.KindPubSubSubscription, "orders-sub", "proj")
	svcNode := node("svc-id", models.KindCloudRunService, "order-processor", "proj")
	svcNode.URL = "https://order-processor-abc123-uc.a.run.app"

	out := resolution.Resolve(models.ResolutionInput{
		Nodes: []models.GraphNode{subNode, svcNode},
		Subscriptions: []models.SubscriptionSummary{
			{
				Name:         "orders-sub",
				PushEndpoint: "https://order-processor-abc123-uc.a.run.app/push",
			},
		},
	})

	e := findEdge(out.Edges, subNode.ID, svcNode.ID, models.EdgeTriggers)
	if e == nil {
		t.Fatalf("expected triggers edge, got %v", out.Edges)
	}
}

func TestPass1_EventDrivenEdges(t *testing.T) {
	trigger := node("trigger-id", models.KindEventarcTrigger, "orders-trigger", "proj")
	scheduler := node("scheduler-id", models.KindSchedulerJob, "nightly", "proj")
	subscription := node("subscription-id", models.KindPubSubSubscription, "orders-sub", "proj")
	topic := node("topic-id", models.KindPubSubTopic, "orders", "proj")
	deadLetter := node("dead-letter-id", models.KindPubSubTopic, "orders-dead", "proj")
	service := node("service-id", models.KindCloudRunService, "orders-api", "proj")
	service.URL = "https://orders-api.example.run.app"

	out := resolution.Resolve(models.ResolutionInput{
		Nodes: []models.GraphNode{trigger, scheduler, subscription, topic, deadLetter, service},
		Triggers: []models.TriggerSummary{{
			Name: "orders-trigger", DestinationURN: "orders-api",
			TransportTopic: "projects/proj/topics/orders",
		}},
		SchedulerJobs: []models.SchedulerJobSummary{{
			Name: "nightly", TargetKind: "http", TargetRef: "https://orders-api.example.run.app/tasks/nightly",
		}},
		Subscriptions: []models.SubscriptionSummary{{
			Name: "orders-sub", Topic: "orders", DeadLetterTopic: "projects/proj/topics/orders-dead",
		}},
	})

	for _, want := range []struct {
		source, target, edgeType string
	}{
		{trigger.ID, service.ID, models.EdgeTriggers},
		{trigger.ID, topic.ID, models.EdgeRoutesTo},
		{scheduler.ID, service.ID, models.EdgeTriggers},
		{subscription.ID, topic.ID, models.EdgeSubscribesTo},
		{subscription.ID, deadLetter.ID, models.EdgeDeadLettersTo},
	} {
		if findEdge(out.Edges, want.source, want.target, want.edgeType) == nil {
			t.Errorf("missing %s edge %s -> %s in %+v", want.edgeType, want.source, want.target, out.Edges)
		}
	}
}

// ─── Pass 2 ───────────────────────────────────────────────────────────────────

func TestPass2_IAMSpannerBinding(t *testing.T) {
	wID := "workload-id"
	spannerID := "spanner-id"

	out := resolution.Resolve(models.ResolutionInput{
		Nodes: []models.GraphNode{
			{ID: wID, Kind: models.KindGKEDeployment, Name: "api", ProjectID: "proj"},
			{ID: spannerID, Kind: models.KindSpannerInstance, Name: "main-db", ProjectID: "proj"},
		},
		IAMBindingsByResource: map[string][]models.IAMBinding{
			spannerID: {
				{Role: "roles/spanner.databaseUser", Members: []string{"serviceAccount:api-sa@proj.iam.gserviceaccount.com"}},
			},
		},
		WorkloadSAByNodeID: map[string]string{
			wID: "api-sa@proj.iam.gserviceaccount.com",
		},
	})

	e := findEdge(out.Edges, wID, spannerID, models.EdgeReadsFromDB)
	if e == nil {
		t.Fatalf("expected reads_from_db edge from IAM pass, got %v", out.Edges)
	}
	if e.Confidence > 0.75 {
		t.Errorf("IAM pass confidence should be ≤0.75, got %f", e.Confidence)
	}
	if e.Metadata["unobserved_at_runtime"] != "true" {
		t.Errorf("expected unobserved_at_runtime metadata")
	}
}

// ─── Pass 3 ───────────────────────────────────────────────────────────────────

func TestPass3_MeshEdge(t *testing.T) {
	callerNode := node("caller-id", models.KindGKEDeployment, "frontend", "proj")
	calleeNode := node("callee-id", models.KindGKEDeployment, "backend", "proj")

	out := resolution.Resolve(models.ResolutionInput{
		Nodes: []models.GraphNode{callerNode, calleeNode},
		MeshEdges: []models.GKEMeshEdge{
			{Caller: "frontend", CallerNamespace: "prod", Callee: "backend", CalleeNamespace: "prod", RequestsPerMinute: 100},
		},
	})

	e := findEdge(out.Edges, callerNode.ID, calleeNode.ID, models.EdgeMeshCalls)
	if e == nil {
		t.Fatalf("expected mesh_calls edge, got %v", out.Edges)
	}
	if e.Confidence != 0.85 {
		t.Errorf("expected confidence 0.85, got %f", e.Confidence)
	}
}

// ─── Pass 4 ───────────────────────────────────────────────────────────────────

func TestPass4_TraceEdge_HighSample(t *testing.T) {
	callerNode := node("a-id", models.KindCloudRunService, "api-gateway", "proj")
	calleeNode := node("b-id", models.KindCloudRunService, "payment-svc", "proj")

	out := resolution.Resolve(models.ResolutionInput{
		Nodes: []models.GraphNode{callerNode, calleeNode},
		TraceEdges: []models.TraceDependencyEdge{
			{Caller: "api-gateway", Callee: "payment-svc", SampleCount: 100},
		},
	})

	e := findEdge(out.Edges, callerNode.ID, calleeNode.ID, models.EdgeTraceCalls)
	if e == nil {
		t.Fatalf("expected trace_calls edge, got %v", out.Edges)
	}
	if e.Confidence != 0.90 {
		t.Errorf("expected confidence 0.90 for 100 samples, got %f", e.Confidence)
	}
}

func TestPass4_TraceEdge_LowSample(t *testing.T) {
	callerNode := node("a-id", models.KindCloudRunService, "svc-a", "proj")
	calleeNode := node("b-id", models.KindCloudRunService, "svc-b", "proj")

	out := resolution.Resolve(models.ResolutionInput{
		Nodes: []models.GraphNode{callerNode, calleeNode},
		TraceEdges: []models.TraceDependencyEdge{
			{Caller: "svc-a", Callee: "svc-b", SampleCount: 5},
		},
	})

	e := findEdge(out.Edges, callerNode.ID, calleeNode.ID, models.EdgeTraceCalls)
	if e == nil {
		t.Fatalf("expected trace_calls edge, got %v", out.Edges)
	}
	// 0.90 * 5/10 = 0.45
	if e.Confidence < 0.44 || e.Confidence > 0.46 {
		t.Errorf("expected confidence ~0.45 for 5 samples, got %f", e.Confidence)
	}
}

// ─── Corroboration boost ──────────────────────────────────────────────────────

func TestCorroborationBoost(t *testing.T) {
	callerNode := node("a-id", models.KindGKEDeployment, "svc-a", "proj")
	calleeNode := node("b-id", models.KindGKEDeployment, "svc-b", "proj")

	// Emit the same edge from both mesh (Pass 3) and trace (Pass 4).
	out := resolution.Resolve(models.ResolutionInput{
		Nodes: []models.GraphNode{callerNode, calleeNode},
		MeshEdges: []models.GKEMeshEdge{
			{Caller: "svc-a", Callee: "svc-b", RequestsPerMinute: 50},
		},
		TraceEdges: []models.TraceDependencyEdge{
			{Caller: "svc-a", Callee: "svc-b", SampleCount: 20},
		},
	})

	// Should produce mesh_calls (Pass 3) AND trace_calls (Pass 4) — different edge types.
	eMesh := findEdge(out.Edges, callerNode.ID, calleeNode.ID, models.EdgeMeshCalls)
	eTrace := findEdge(out.Edges, callerNode.ID, calleeNode.ID, models.EdgeTraceCalls)
	if eMesh == nil {
		t.Error("expected mesh_calls edge")
	}
	if eTrace == nil {
		t.Error("expected trace_calls edge")
	}
}

// ─── Determinism ─────────────────────────────────────────────────────────────

func TestDeterminism(t *testing.T) {
	input := models.ResolutionInput{
		Nodes: []models.GraphNode{
			node("c-id", models.KindGKEDeployment, "svc-c", "proj"),
			node("d-id", models.KindGKEDeployment, "svc-d", "proj"),
			node("e-id", models.KindCloudRunService, "svc-e", "proj"),
		},
		MeshEdges: []models.GKEMeshEdge{
			{Caller: "svc-c", Callee: "svc-d"},
			{Caller: "svc-d", Callee: "svc-e"},
		},
		TraceEdges: []models.TraceDependencyEdge{
			{Caller: "svc-c", Callee: "svc-e", SampleCount: 3},
		},
	}

	out1 := resolution.Resolve(input)
	out2 := resolution.Resolve(input)

	if len(out1.Edges) != len(out2.Edges) {
		t.Fatalf("non-deterministic edge count: %d vs %d", len(out1.Edges), len(out2.Edges))
	}
	for i := range out1.Edges {
		e1, e2 := out1.Edges[i], out2.Edges[i]
		if e1.Source != e2.Source || e1.Target != e2.Target || e1.Type != e2.Type {
			t.Errorf("non-deterministic edge at %d: %+v vs %+v", i, e1, e2)
		}
	}
}

// ─── External endpoint minting ────────────────────────────────────────────────

func TestExternalEndpointMinting(t *testing.T) {
	subNode := node("sub-id", models.KindPubSubSubscription, "webhook-sub", "proj")

	out := resolution.Resolve(models.ResolutionInput{
		Nodes:           []models.GraphNode{subNode},
		IncludeExternal: true,
		Subscriptions: []models.SubscriptionSummary{
			{
				Name:         "webhook-sub",
				PushEndpoint: "https://api.stripe.com/webhooks/receive",
			},
		},
	})

	if len(out.MintedNodes) == 0 {
		t.Fatal("expected external_endpoint node to be minted for stripe.com")
	}
	minted := out.MintedNodes[0]
	if minted.Kind != models.KindExternalEndpoint {
		t.Errorf("expected kind external_endpoint, got %q", minted.Kind)
	}
	if minted.Name != "Stripe" {
		t.Errorf("expected friendly name Stripe, got %q", minted.Name)
	}
}
