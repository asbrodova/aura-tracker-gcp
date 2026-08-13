package gcp

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	runpb "cloud.google.com/go/run/apiv2/runpb"
	"golang.org/x/sync/errgroup"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

var (
	dbEnvPatterns              = []string{"DATABASE_URL", "DB_HOST", "POSTGRES_HOST", "MYSQL_HOST", "MONGODB_URI", "DB_NAME", "POSTGRES_DB", "MYSQL_DB", "DB_CONNECTION_STRING"}
	pubsubPatterns             = []string{"PUBSUB_TOPIC", "TOPIC_NAME", "TOPIC_ID"}
	pubsubSubscriptionPatterns = []string{"PUBSUB_SUBSCRIPTION", "SUBSCRIPTION_ID", "PUBSUB_SUB"}
	gcsPatterns                = []string{"GCS_BUCKET", "STORAGE_BUCKET", "BUCKET_NAME", "GCS_BUCKET_NAME"}
	cachePatterns              = []string{"REDIS_HOST", "REDIS_URL", "CACHE_URL", "CACHE_HOST"}
	secretSuffixes             = []string{"_SECRET", "_KEY", "_PASSWORD", "_CREDENTIALS", "_TOKEN"}
	pubsubValueRe              = regexp.MustCompile(`^projects/[^/]+/topics/[^/]+$`)
	topologyEnvNameRe          = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
)

func (a *gcpAdapter) GetServiceTopology(ctx context.Context, req models.GetServiceTopologyRequest) (models.ServiceTopologyReport, error) {
	if err := a.rateWait(ctx, "topology.GetServiceTopology"); err != nil {
		return models.ServiceTopologyReport{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	depth := req.Depth
	if depth < 1 {
		depth = 1
	}
	if depth > 2 {
		depth = 2
	}

	rootID := "cloudrun:" + req.ServiceName
	svcName := fmt.Sprintf("projects/%s/locations/%s/services/%s", req.Project, req.Region, req.ServiceName)

	svc, err := a.runSvc.GetService(ctx, &runpb.GetServiceRequest{Name: svcName})
	if err != nil {
		return models.ServiceTopologyReport{}, wrapGCPError("topology.GetServiceTopology", err)
	}

	nodes := []models.TopologyNode{{
		ID:     rootID,
		Kind:   "cloudrun_service",
		Name:   req.ServiceName,
		Region: req.Region,
		URL:    svc.Uri,
	}}

	direct := inferFromServiceSpec(svc, rootID, req.Project)
	nodes = append(nodes, direct.nodes...)
	edges := append([]models.TopologyEdge(nil), direct.edges...)
	var warns []string

	g, gctx := errgroup.WithContext(ctx)
	var subscriptions []*pubsubpb.Subscription
	var subscriptionsErr error
	g.Go(func() error {
		subscriptions, subscriptionsErr = a.listTopologySubscriptions(gctx, req.Project)
		return nil
	})

	var serviceInventory models.ListServicesResponse
	var serviceInventoryErr error
	if depth == 2 {
		g.Go(func() error {
			serviceInventory, serviceInventoryErr = a.ListServices(gctx, models.ListServicesRequest{ProjectID: req.Project})
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return models.ServiceTopologyReport{}, wrapGCPError("topology.GetServiceTopology", err)
	}
	if err := ctx.Err(); err != nil {
		return models.ServiceTopologyReport{}, wrapGCPError("topology.GetServiceTopology", err)
	}
	if subscriptionsErr != nil {
		warns = append(warns, "pubsub subscription scan: "+subscriptionsErr.Error())
	}
	if serviceInventoryErr != nil {
		warns = append(warns, "depth-2 Cloud Run service discovery: "+serviceInventoryErr.Error())
	}
	if serviceInventory.Truncated {
		warns = append(warns, "depth-2 Cloud Run service discovery was truncated; some push targets may be unresolved")
	}

	incoming := inferIncomingPushTopics(subscriptions, svc.Uri, rootID)
	nodes = append(nodes, incoming.nodes...)
	edges = append(edges, incoming.edges...)
	if depth == 2 {
		expanded := expandPubSubSecondHop(nodes, subscriptions, serviceInventory.Services, rootID)
		nodes = append(nodes, expanded.nodes...)
		edges = append(edges, expanded.edges...)
	}

	deduped := dedupNodes(nodes)
	edges = dedupTopologyEdges(edges)
	report := models.ServiceTopologyReport{
		RootService:   req.ServiceName,
		Project:       req.Project,
		Depth:         depth,
		Nodes:         deduped,
		Edges:         edges,
		Relationships: renderRelationships(deduped, edges),
		Warnings:      warns,
	}
	return report, nil
}

// inferResult holds nodes and edges derived from a Cloud Run service spec without I/O.
type inferResult struct {
	nodes []models.TopologyNode
	edges []models.TopologyEdge
}

// inferFromServiceSpec extracts topology relationships from the Cloud Run service definition.
// It is a pure function with no I/O — all logic is testable without a GCP connection.
func inferFromServiceSpec(svc *runpb.Service, rootID, project string) inferResult {
	var r inferResult
	if svc.Template == nil {
		return r
	}

	// 1. Cloud SQL instances annotation (explicit, high confidence).
	if annot := svc.Template.Annotations["run.googleapis.com/cloudsql-instances"]; annot != "" {
		for _, conn := range strings.Split(annot, ",") {
			conn = strings.TrimSpace(conn)
			if conn == "" {
				continue
			}
			nodeID := "cloudsql:" + conn
			r.nodes = append(r.nodes, models.TopologyNode{ID: nodeID, Kind: "cloudsql_instance", Name: conn})
			r.edges = append(r.edges, models.TopologyEdge{
				From: rootID, To: nodeID,
				Relationship: "connects_to_db",
				Evidence:     "cloud_sql_annotation",
				Confidence:   "high",
			})
		}
	}

	// 2. VPC connector (explicit network topology, high confidence).
	if svc.Template.VpcAccess != nil && svc.Template.VpcAccess.Connector != "" {
		nodeID := "vpc_connector:" + svc.Template.VpcAccess.Connector
		r.nodes = append(r.nodes, models.TopologyNode{ID: nodeID, Kind: "vpc_connector", Name: svc.Template.VpcAccess.Connector})
		r.edges = append(r.edges, models.TopologyEdge{
			From: rootID, To: nodeID,
			Relationship: "network_via_vpc",
			Evidence:     "vpc_connector",
			Confidence:   "high",
		})
	}

	// 3. Environment variables — infer dependencies from naming conventions and values.
	if len(svc.Template.Containers) > 0 {
		for _, env := range svc.Template.Containers[0].Env {
			if env == nil {
				continue
			}
			// 3a. Secret Manager reference (ValueSource) — explicit, high confidence.
			if vs := env.GetValueSource(); vs != nil {
				if ref := vs.GetSecretKeyRef(); ref != nil && ref.Secret != "" {
					nodeID := "secret:" + ref.Secret
					r.nodes = append(r.nodes, models.TopologyNode{ID: nodeID, Kind: "secret", Name: ref.Secret})
					r.edges = append(r.edges, models.TopologyEdge{
						From: rootID, To: nodeID,
						Relationship: "reads_secret",
						Evidence:     "secret_ref:" + env.Name,
						Confidence:   "high",
					})
					continue
				}
			}

			name, value := strings.ToUpper(env.Name), env.GetValue()
			if value == "" {
				continue
			}

			// 3b. Value is a full Pub/Sub topic resource path.
			if pubsubValueRe.MatchString(value) {
				nodeID := "pubsub_topic:" + value
				r.nodes = append(r.nodes, models.TopologyNode{ID: nodeID, Kind: "pubsub_topic", Name: value})
				r.edges = append(r.edges, models.TopologyEdge{
					From: rootID, To: nodeID,
					Relationship: "publishes_to",
					Evidence:     "env_var:" + env.Name,
					Confidence:   "medium",
					Inferred:     true,
				})
				continue
			}

			// 3c. Env var name suggests a database connection.
			if containsAny(name, dbEnvPatterns) {
				node := redactedEnvDependency("external_db", "database endpoint", rootID, env.Name)
				r.nodes = append(r.nodes, node)
				r.edges = append(r.edges, models.TopologyEdge{
					From: rootID, To: node.ID,
					Relationship: "connects_to_db",
					Evidence:     "env_var:" + safeTopologyEnvName(env.Name),
					Confidence:   "medium",
					Inferred:     true,
				})
				continue
			}

			// 3d. Env var name suggests a Pub/Sub topic (publisher).
			if containsAny(name, pubsubPatterns) {
				topicPath := value
				if !strings.HasPrefix(topicPath, "projects/") {
					topicPath = fmt.Sprintf("projects/%s/topics/%s", project, value)
				}
				nodeID := "pubsub_topic:" + topicPath
				r.nodes = append(r.nodes, models.TopologyNode{ID: nodeID, Kind: "pubsub_topic", Name: topicPath})
				r.edges = append(r.edges, models.TopologyEdge{
					From: rootID, To: nodeID,
					Relationship: "publishes_to",
					Evidence:     "env_var:" + env.Name,
					Confidence:   "medium",
					Inferred:     true,
				})
				continue
			}

			// 3d-bis. Env var name suggests a Pub/Sub subscription (consumer).
			if containsAny(name, pubsubSubscriptionPatterns) {
				subPath := value
				if !strings.HasPrefix(subPath, "projects/") {
					subPath = fmt.Sprintf("projects/%s/subscriptions/%s", project, value)
				}
				nodeID := "pubsub_subscription:" + subPath
				r.nodes = append(r.nodes, models.TopologyNode{ID: nodeID, Kind: "pubsub_subscription", Name: subPath})
				r.edges = append(r.edges, models.TopologyEdge{
					From: rootID, To: nodeID,
					Relationship: "subscribes_to",
					Evidence:     "env_var:" + env.Name,
					Confidence:   "medium",
					Inferred:     true,
				})
				continue
			}

			// 3e. Env var name suggests a GCS bucket.
			if containsAny(name, gcsPatterns) {
				nodeID := "gcs_bucket:" + value
				r.nodes = append(r.nodes, models.TopologyNode{ID: nodeID, Kind: "gcs_bucket", Name: value})
				r.edges = append(r.edges, models.TopologyEdge{
					From: rootID, To: nodeID,
					Relationship: "reads_writes_storage",
					Evidence:     "env_var:" + env.Name,
					Confidence:   "medium",
					Inferred:     true,
				})
				continue
			}

			// 3e-bis. Env var name suggests a Redis/cache endpoint.
			if containsAny(name, cachePatterns) {
				node := redactedEnvDependency("redis_cache", "cache endpoint", rootID, env.Name)
				r.nodes = append(r.nodes, node)
				r.edges = append(r.edges, models.TopologyEdge{
					From: rootID, To: node.ID,
					Relationship: "reads_writes_cache",
					Evidence:     "env_var:" + safeTopologyEnvName(env.Name),
					Confidence:   "medium",
					Inferred:     true,
				})
				continue
			}

			// 3f. Env var name suffix suggests a secret/credential (low confidence).
			if hasSuffix(name, secretSuffixes) {
				nodeID := "secret:" + env.Name
				r.nodes = append(r.nodes, models.TopologyNode{ID: nodeID, Kind: "secret", Name: env.Name})
				r.edges = append(r.edges, models.TopologyEdge{
					From: rootID, To: nodeID,
					Relationship: "reads_secret",
					Evidence:     "env_var:" + env.Name,
					Confidence:   "low",
					Inferred:     true,
				})
			}
		}
	}

	return r
}

// redactedEnvDependency deliberately does not accept the environment value.
// Endpoint-shaped values are untrusted strings and may contain credentials even
// when they resemble valid hosts. The public graph preserves only classification
// and env-name evidence; the root keeps otherwise-identical env names distinct
// when multiple services are traversed in one report.
func redactedEnvDependency(kind, label, rootID, envName string) models.TopologyNode {
	envName = safeTopologyEnvName(envName)
	return models.TopologyNode{
		ID:   kind + ":" + rootID + ":env:" + envName,
		Kind: kind,
		Name: label + " configured via " + envName + " (value withheld)",
	}
}

func safeTopologyEnvName(name string) string {
	name = strings.ToUpper(strings.TrimSpace(name))
	if !topologyEnvNameRe.MatchString(name) {
		return "CONFIGURED_ENDPOINT"
	}
	return name
}

// listTopologySubscriptions performs one bounded project-wide scan that is
// reused for both direct incoming edges and depth-two Pub/Sub expansion.
func (a *gcpAdapter) listTopologySubscriptions(ctx context.Context, project string) ([]*pubsubpb.Subscription, error) {
	if a.pubsub == nil || a.pubsub.SubscriptionAdminClient == nil {
		return nil, fmt.Errorf("pubsub client is not initialized")
	}
	it := a.pubsub.SubscriptionAdminClient.ListSubscriptions(ctx, &pubsubpb.ListSubscriptionsRequest{
		Project: "projects/" + project, PageSize: maxUnpagedInventoryItems,
	})
	var subscriptions []*pubsubpb.Subscription
	for scanned := 0; ; scanned++ {
		sub, err := it.Next()
		if isIteratorDone(err) {
			break
		}
		if err != nil {
			return subscriptions, err
		}
		if scanned >= maxUnpagedInventoryItems {
			return subscriptions, fmt.Errorf("%w at %d subscriptions", errInventoryLimitReached, maxUnpagedInventoryItems)
		}
		subscriptions = append(subscriptions, sub)
	}
	return subscriptions, nil
}

func inferIncomingPushTopics(subscriptions []*pubsubpb.Subscription, serviceURL, rootID string) inferResult {
	var result inferResult
	if serviceURL == "" {
		return result
	}
	for _, sub := range subscriptions {
		if sub == nil || sub.PushConfig == nil || !topologyEndpointMatchesService(sub.PushConfig.PushEndpoint, serviceURL) {
			continue
		}
		topicNodeID := "pubsub_topic:" + sub.Topic
		result.nodes = append(result.nodes, models.TopologyNode{ID: topicNodeID, Kind: "pubsub_topic", Name: sub.Topic})
		result.edges = append(result.edges, models.TopologyEdge{
			From:         topicNodeID,
			To:           rootID,
			Relationship: "triggers",
			Evidence:     "push_subscription:" + sub.Name,
			Confidence:   "high",
		})
	}
	return result
}

// expandPubSubSecondHop follows only resources that were direct neighbors of
// the root. Direct topics expand to their subscriptions and known Cloud Run
// push consumers. A directly referenced subscription expands only to its own
// topic, preventing accidental third-hop sibling expansion.
func expandPubSubSecondHop(directNodes []models.TopologyNode, subscriptions []*pubsubpb.Subscription, services []models.ServiceSummary, rootID string) inferResult {
	directTopics := make(map[string]bool)
	directSubscriptions := make(map[string]bool)
	for _, node := range directNodes {
		switch node.Kind {
		case "pubsub_topic":
			directTopics[node.Name] = true
		case "pubsub_subscription":
			directSubscriptions[node.Name] = true
		}
	}

	var result inferResult
	for _, sub := range subscriptions {
		if sub == nil {
			continue
		}
		topicIsDirect := directTopics[sub.Topic]
		subscriptionIsDirect := directSubscriptions[sub.Name]
		if !topicIsDirect && !subscriptionIsDirect {
			continue
		}

		topicID := "pubsub_topic:" + sub.Topic
		subscriptionID := "pubsub_subscription:" + sub.Name
		result.nodes = append(result.nodes,
			models.TopologyNode{ID: topicID, Kind: "pubsub_topic", Name: sub.Topic},
			models.TopologyNode{ID: subscriptionID, Kind: "pubsub_subscription", Name: sub.Name},
		)
		result.edges = append(result.edges, models.TopologyEdge{
			From: topicID, To: subscriptionID, Relationship: "has_subscription",
			Evidence: "subscription_metadata", Confidence: "high",
		})

		if sub.PushConfig == nil || sub.PushConfig.PushEndpoint == "" {
			continue
		}
		service, ok := matchTopologyCloudRunService(sub.PushConfig.PushEndpoint, services)
		if !ok {
			continue
		}
		serviceID := "cloudrun:" + service.Name
		if serviceID == "cloudrun:" {
			continue
		}
		result.nodes = append(result.nodes, models.TopologyNode{
			ID: serviceID, Kind: "cloudrun_service", Name: service.Name, Region: service.Region, URL: service.URL,
		})
		if topicIsDirect {
			result.edges = append(result.edges, models.TopologyEdge{
				From: topicID, To: serviceID, Relationship: "triggers",
				Evidence: "push_subscription:" + sub.Name, Confidence: "high",
			})
		} else if subscriptionIsDirect && serviceID != rootID {
			result.edges = append(result.edges, models.TopologyEdge{
				From: subscriptionID, To: serviceID, Relationship: "pushes_to",
				Evidence: "push_subscription:" + sub.Name, Confidence: "high",
			})
		}
	}
	return result
}

func matchTopologyCloudRunService(endpoint string, services []models.ServiceSummary) (models.ServiceSummary, bool) {
	ordered := append([]models.ServiceSummary(nil), services...)
	sort.SliceStable(ordered, func(i, j int) bool { return len(ordered[i].URL) > len(ordered[j].URL) })
	for _, service := range ordered {
		if service.URL != "" && topologyEndpointMatchesService(endpoint, service.URL) {
			return service, true
		}
	}
	return models.ServiceSummary{}, false
}

func topologyEndpointMatchesService(endpoint, serviceURL string) bool {
	serviceURL = strings.TrimRight(strings.TrimSpace(serviceURL), "/")
	endpoint = strings.TrimSpace(endpoint)
	if serviceURL == "" || !strings.HasPrefix(endpoint, serviceURL) {
		return false
	}
	if len(endpoint) == len(serviceURL) {
		return true
	}
	switch endpoint[len(serviceURL)] {
	case '/', '?', '#':
		return true
	default:
		return false
	}
}

// renderRelationships converts nodes and edges into flat human-readable statements
// that an LLM can reason over without parsing JSON graph structures.
func renderRelationships(nodes []models.TopologyNode, edges []models.TopologyEdge) []string {
	byID := make(map[string]models.TopologyNode, len(nodes))
	for _, n := range nodes {
		byID[n.ID] = n
	}

	stmts := make([]string, 0, len(edges))
	for _, e := range edges {
		from, to := e.From, e.To
		if n, ok := byID[e.From]; ok {
			from = n.Kind + ":" + n.Name
		}
		if n, ok := byID[e.To]; ok {
			to = n.Kind + ":" + n.Name
		}
		stmts = append(stmts, fmt.Sprintf(
			"%s -[%s]-> %s (evidence: %s, confidence: %s)",
			from, e.Relationship, to, e.Evidence, e.Confidence,
		))
	}
	return stmts
}

// dedupNodes removes duplicate TopologyNodes by ID, preserving first-seen order.
func dedupNodes(nodes []models.TopologyNode) []models.TopologyNode {
	seen := make(map[string]bool, len(nodes))
	out := make([]models.TopologyNode, 0, len(nodes))
	for _, n := range nodes {
		if !seen[n.ID] {
			seen[n.ID] = true
			out = append(out, n)
		}
	}
	return out
}

func dedupTopologyEdges(edges []models.TopologyEdge) []models.TopologyEdge {
	seen := make(map[string]bool, len(edges))
	out := make([]models.TopologyEdge, 0, len(edges))
	for _, edge := range edges {
		key := edge.From + "\x00" + edge.To + "\x00" + edge.Relationship + "\x00" + edge.Evidence
		if !seen[key] {
			seen[key] = true
			out = append(out, edge)
		}
	}
	return out
}

func containsAny(name string, patterns []string) bool {
	for _, p := range patterns {
		if strings.Contains(name, p) {
			return true
		}
	}
	return false
}

func hasSuffix(name string, suffixes []string) bool {
	for _, s := range suffixes {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	return false
}
