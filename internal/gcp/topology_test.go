package gcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	runpb "cloud.google.com/go/run/apiv2/runpb"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func TestInferFromServiceSpecRedactsEveryEndpointPattern(t *testing.T) {
	tests := []struct {
		envName      string
		kind         string
		label        string
		relationship string
	}{
		{envName: "DATABASE_URL", kind: "external_db", label: "database endpoint", relationship: "connects_to_db"},
		{envName: "DB_HOST", kind: "external_db", label: "database endpoint", relationship: "connects_to_db"},
		{envName: "POSTGRES_HOST", kind: "external_db", label: "database endpoint", relationship: "connects_to_db"},
		{envName: "MYSQL_HOST", kind: "external_db", label: "database endpoint", relationship: "connects_to_db"},
		{envName: "MONGODB_URI", kind: "external_db", label: "database endpoint", relationship: "connects_to_db"},
		{envName: "DB_NAME", kind: "external_db", label: "database endpoint", relationship: "connects_to_db"},
		{envName: "POSTGRES_DB", kind: "external_db", label: "database endpoint", relationship: "connects_to_db"},
		{envName: "MYSQL_DB", kind: "external_db", label: "database endpoint", relationship: "connects_to_db"},
		{envName: "DB_CONNECTION_STRING", kind: "external_db", label: "database endpoint", relationship: "connects_to_db"},
		{envName: "REDIS_HOST", kind: "redis_cache", label: "cache endpoint", relationship: "reads_writes_cache"},
		{envName: "REDIS_URL", kind: "redis_cache", label: "cache endpoint", relationship: "reads_writes_cache"},
		{envName: "CACHE_URL", kind: "redis_cache", label: "cache endpoint", relationship: "reads_writes_cache"},
		{envName: "CACHE_HOST", kind: "redis_cache", label: "cache endpoint", relationship: "reads_writes_cache"},
		{envName: "PAYMENTS_DATABASE_URL", kind: "external_db", label: "database endpoint", relationship: "connects_to_db"},
	}

	const value = "postgres://user:topology-secret@db.internal:5432/app?token=query-secret#fragment-secret"
	for _, tt := range tests {
		t.Run(tt.envName, func(t *testing.T) {
			derived := inferSensitiveEnv(t, "cloudrun:api", tt.envName, value)
			if len(derived.nodes) != 1 || len(derived.edges) != 1 {
				t.Fatalf("unexpected inference result: %+v", derived)
			}
			wantID := tt.kind + ":cloudrun:api:env:" + tt.envName
			wantName := tt.label + " configured via " + tt.envName + " (value withheld)"
			if got := derived.nodes[0]; got.ID != wantID || got.Kind != tt.kind || got.Name != wantName {
				t.Fatalf("unexpected node: %+v", got)
			}
			if got := derived.edges[0]; got.From != "cloudrun:api" || got.To != wantID || got.Relationship != tt.relationship || got.Evidence != "env_var:"+tt.envName || !got.Inferred {
				t.Fatalf("unexpected edge: %+v", got)
			}
			assertTopologyReportOmits(t, derived, value, "topology-secret", "query-secret", "fragment-secret")
		})
	}
}

func TestInferFromServiceSpecRedactsDangerousEndpointFormats(t *testing.T) {
	values := []string{
		"postgres://alice:p%40ss@db.internal:5432/prod?password=querysecret#fragment",
		"mysql://root:secret@tcp(db.internal:3306)/app",
		"mongodb+srv://user:secret@cluster.example/app?authSource=admin",
		"redis://:secret@cache.internal:6379/0",
		"jdbc:postgresql://db:5432/app?user=alice&password=secret",
		"Server=db;User ID=alice;Password=hunter2;Database=prod",
		`{"type":"service_account","private_key":"private-secret"}`,
		"bare-host-or-token",
		"/cloudsql/project:region:instance",
		"[2001:db8::1]:5432",
		"quotes-'\"-backslash-\\-newline-\n-control-\x01-secret",
		strings.Repeat("very-long-secret", 1024),
	}

	for _, envName := range []string{"DATABASE_URL", "REDIS_URL", "DB_HOST", "CACHE_HOST"} {
		for i, value := range values {
			t.Run(envName+"/case", func(t *testing.T) {
				derived := inferSensitiveEnv(t, "cloudrun:api", envName, value)
				assertTopologyReportOmits(t, derived, value)
				if got := derived.nodes[0].Name; !strings.Contains(got, "value withheld") {
					t.Fatalf("node name does not disclose redaction: %q (case %d)", got, i)
				}
			})
		}
	}
}

func TestSensitiveTopologyOutputIsIndependentOfLiteral(t *testing.T) {
	first := inferSensitiveEnv(t, "cloudrun:api", "DATABASE_URL", "postgres://user:first-secret@one.example/db")
	second := inferSensitiveEnv(t, "cloudrun:api", "DATABASE_URL", "host=two.example password=second-secret")
	firstJSON := marshalTopologyResult(t, first)
	secondJSON := marshalTopologyResult(t, second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("serialized topology depends on sensitive literal:\n%s\n%s", firstJSON, secondJSON)
	}

	sum := sha256.Sum256([]byte("postgres://user:first-secret@one.example/db"))
	if strings.Contains(string(firstJSON), hex.EncodeToString(sum[:])) {
		t.Fatal("public topology contains a raw-value fingerprint")
	}
}

func TestRedactedEnvDependencyIdentityIsScopedAndDeduplicates(t *testing.T) {
	first := redactedEnvDependency("external_db", "database endpoint", "cloudrun:one", "DATABASE_URL")
	same := redactedEnvDependency("external_db", "database endpoint", "cloudrun:one", "DATABASE_URL")
	otherRoot := redactedEnvDependency("external_db", "database endpoint", "cloudrun:two", "DATABASE_URL")
	otherName := redactedEnvDependency("external_db", "database endpoint", "cloudrun:one", "DB_HOST")
	otherKind := redactedEnvDependency("redis_cache", "cache endpoint", "cloudrun:one", "DATABASE_URL")
	if first.ID != same.ID {
		t.Fatal("same metadata produced different node IDs")
	}
	for _, node := range []models.TopologyNode{otherRoot, otherName, otherKind} {
		if first.ID == node.ID {
			t.Fatalf("node identity collision: %q", first.ID)
		}
	}
	if got := dedupNodes([]models.TopologyNode{first, same}); len(got) != 1 {
		t.Fatalf("duplicate nodes were not deduplicated: %+v", got)
	}
}

func TestInferFromServiceSpecHandlesNilAndUnrelatedEnv(t *testing.T) {
	if got := inferFromServiceSpec(&runpb.Service{}, "cloudrun:api", "project"); len(got.nodes) != 0 || len(got.edges) != 0 {
		t.Fatalf("nil template produced topology: %+v", got)
	}
	svc := &runpb.Service{Template: &runpb.RevisionTemplate{Containers: []*runpb.Container{{
		Env: []*runpb.EnvVar{nil, {Name: "DATABASE_URL", Values: &runpb.EnvVar_Value{Value: ""}}, {Name: "NORMAL_VALUE", Values: &runpb.EnvVar_Value{Value: "not-sensitive"}}},
	}}}}
	if got := inferFromServiceSpec(svc, "cloudrun:api", "project"); len(got.nodes) != 0 || len(got.edges) != 0 {
		t.Fatalf("empty or unrelated env produced topology: %+v", got)
	}
}

func TestExpandPubSubSecondHopFindsSubscriptionsAndPushConsumers(t *testing.T) {
	topic := "projects/project/topics/orders"
	direct := []models.TopologyNode{
		{ID: "cloudrun:api", Kind: "cloudrun_service", Name: "api"},
		{ID: "pubsub_topic:" + topic, Kind: "pubsub_topic", Name: topic},
	}
	subscriptions := []*pubsubpb.Subscription{
		{Name: "projects/project/subscriptions/worker", Topic: topic, PushConfig: &pubsubpb.PushConfig{PushEndpoint: "https://worker.example/tasks"}},
		{Name: "projects/project/subscriptions/pull", Topic: topic},
		{Name: "projects/project/subscriptions/unrelated", Topic: "projects/project/topics/other"},
	}
	services := []models.ServiceSummary{{Name: "worker", Region: "us-central1", URL: "https://worker.example"}}

	got := expandPubSubSecondHop(direct, subscriptions, services, "cloudrun:api")
	nodes := topologyNodeSet(got.nodes)
	for _, id := range []string{
		"pubsub_topic:" + topic,
		"pubsub_subscription:projects/project/subscriptions/worker",
		"pubsub_subscription:projects/project/subscriptions/pull",
		"cloudrun:worker",
	} {
		if !nodes[id] {
			t.Errorf("depth-two expansion missing node %q: %+v", id, got.nodes)
		}
	}
	if nodes["pubsub_subscription:projects/project/subscriptions/unrelated"] {
		t.Fatalf("unrelated third-party topic was traversed: %+v", got.nodes)
	}
	if !hasTopologyEdge(got.edges, "pubsub_topic:"+topic, "cloudrun:worker", "triggers") {
		t.Fatalf("push consumer edge missing: %+v", got.edges)
	}
}

func TestExpandPubSubSecondHopDoesNotTraverseSiblingsOfDiscoveredTopic(t *testing.T) {
	directSub := "projects/project/subscriptions/direct"
	topic := "projects/project/topics/orders"
	direct := []models.TopologyNode{{ID: "pubsub_subscription:" + directSub, Kind: "pubsub_subscription", Name: directSub}}
	subscriptions := []*pubsubpb.Subscription{
		{Name: directSub, Topic: topic},
		{Name: "projects/project/subscriptions/sibling", Topic: topic},
	}

	got := expandPubSubSecondHop(direct, subscriptions, nil, "cloudrun:api")
	nodes := topologyNodeSet(got.nodes)
	if !nodes["pubsub_topic:"+topic] || !nodes["pubsub_subscription:"+directSub] {
		t.Fatalf("direct subscription did not expand to its topic: %+v", got.nodes)
	}
	if nodes["pubsub_subscription:projects/project/subscriptions/sibling"] {
		t.Fatalf("depth-two traversal expanded a third-hop sibling: %+v", got.nodes)
	}
}

func TestTopologyEndpointMatchingRequiresURLBoundary(t *testing.T) {
	for _, endpoint := range []string{
		"https://service.example",
		"https://service.example/path",
		"https://service.example?token=opaque",
	} {
		if !topologyEndpointMatchesService(endpoint, "https://service.example/") {
			t.Errorf("valid endpoint %q did not match", endpoint)
		}
	}
	if topologyEndpointMatchesService("https://service.example.evil.test/path", "https://service.example") {
		t.Fatal("host-prefix lookalike matched the service URL")
	}

	incoming := inferIncomingPushTopics([]*pubsubpb.Subscription{{
		Name: "projects/project/subscriptions/evil", Topic: "projects/project/topics/other",
		PushConfig: &pubsubpb.PushConfig{PushEndpoint: "https://service.example.evil.test/path"},
	}}, "https://service.example", "cloudrun:api")
	if len(incoming.nodes) != 0 || len(incoming.edges) != 0 {
		t.Fatalf("lookalike push endpoint produced an incoming edge: %+v", incoming)
	}
}

func TestDedupTopologyEdgesPreservesDistinctEvidence(t *testing.T) {
	edge := models.TopologyEdge{From: "a", To: "b", Relationship: "triggers", Evidence: "one"}
	other := edge
	other.Evidence = "two"
	got := dedupTopologyEdges([]models.TopologyEdge{edge, edge, other})
	if len(got) != 2 {
		t.Fatalf("deduplicated edges = %+v, want two distinct evidence paths", got)
	}
}

func topologyNodeSet(nodes []models.TopologyNode) map[string]bool {
	set := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		set[node.ID] = true
	}
	return set
}

func hasTopologyEdge(edges []models.TopologyEdge, from, to, relationship string) bool {
	for _, edge := range edges {
		if edge.From == from && edge.To == to && edge.Relationship == relationship {
			return true
		}
	}
	return false
}

func inferSensitiveEnv(t *testing.T, rootID, envName, value string) inferResult {
	t.Helper()
	svc := &runpb.Service{Template: &runpb.RevisionTemplate{Containers: []*runpb.Container{{
		Env: []*runpb.EnvVar{{Name: envName, Values: &runpb.EnvVar_Value{Value: value}}},
	}}}}
	return inferFromServiceSpec(svc, rootID, "project")
}

func assertTopologyReportOmits(t *testing.T, derived inferResult, forbidden ...string) {
	t.Helper()
	encoded := marshalTopologyResult(t, derived)
	for _, value := range forbidden {
		if value != "" && strings.Contains(string(encoded), value) {
			t.Fatalf("sensitive value %q leaked into report: %s", value, encoded)
		}
	}
}

func marshalTopologyResult(t *testing.T, derived inferResult) []byte {
	t.Helper()
	report := models.ServiceTopologyReport{
		Nodes:         derived.nodes,
		Edges:         derived.edges,
		Relationships: renderRelationships(derived.nodes, derived.edges),
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	return encoded
}
