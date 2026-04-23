package gcp

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	runpb "cloud.google.com/go/run/apiv2/runpb"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"golang.org/x/sync/errgroup"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

var (
	dbEnvPatterns  = []string{"DATABASE_URL", "DB_HOST", "POSTGRES_HOST", "MYSQL_HOST", "MONGODB_URI", "DB_NAME", "POSTGRES_DB", "MYSQL_DB"}
	pubsubPatterns = []string{"PUBSUB_TOPIC", "TOPIC_NAME", "TOPIC_ID"}
	gcsPatterns    = []string{"GCS_BUCKET", "STORAGE_BUCKET", "BUCKET_NAME", "GCS_BUCKET_NAME"}
	secretSuffixes = []string{"_SECRET", "_KEY", "_PASSWORD", "_CREDENTIALS", "_TOKEN"}
	pubsubValueRe  = regexp.MustCompile(`^projects/[^/]+/topics/[^/]+$`)
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

	var (
		edges []models.TopologyEdge
		warns []string
		mu    sync.Mutex
	)

	g, gctx := errgroup.WithContext(ctx)

	// Goroutine 1: infer relationships from service spec — no I/O, pure computation.
	g.Go(func() error {
		derived := inferFromServiceSpec(svc, rootID, req.Project)
		mu.Lock()
		nodes = append(nodes, derived.nodes...)
		edges = append(edges, derived.edges...)
		mu.Unlock()
		return nil
	})

	// Goroutine 2: scan Pub/Sub subscriptions for push endpoints matching this service URL.
	g.Go(func() error {
		if svc.Uri == "" {
			return nil
		}
		subNodes, subEdges, err := a.findPushSubscriptions(gctx, req.Project, svc.Uri, rootID)
		if err != nil {
			mu.Lock()
			warns = append(warns, "pubsub push scan: "+err.Error())
			mu.Unlock()
			return nil // non-fatal: insufficient permissions should not fail the whole call
		}
		mu.Lock()
		nodes = append(nodes, subNodes...)
		edges = append(edges, subEdges...)
		mu.Unlock()
		return nil
	})

	if err := g.Wait(); err != nil {
		return models.ServiceTopologyReport{}, wrapGCPError("topology.GetServiceTopology", err)
	}

	deduped := dedupNodes(nodes)
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
				})
				continue
			}

			// 3c. Env var name suggests a database connection.
			if containsAny(name, dbEnvPatterns) {
				nodeID := "external_db:" + value
				r.nodes = append(r.nodes, models.TopologyNode{ID: nodeID, Kind: "external_db", Name: value})
				r.edges = append(r.edges, models.TopologyEdge{
					From: rootID, To: nodeID,
					Relationship: "connects_to_db",
					Evidence:     "env_var:" + env.Name,
					Confidence:   "medium",
				})
				continue
			}

			// 3d. Env var name suggests a Pub/Sub topic.
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
				})
			}
		}
	}

	return r
}

// findPushSubscriptions scans all Pub/Sub subscriptions in the project and returns
// nodes/edges for those whose push endpoint starts with the service URL.
func (a *gcpAdapter) findPushSubscriptions(ctx context.Context, project, serviceURL, rootID string) ([]models.TopologyNode, []models.TopologyEdge, error) {
	var nodes []models.TopologyNode
	var edges []models.TopologyEdge

	it := a.pubsub.SubscriptionAdminClient.ListSubscriptions(ctx, &pubsubpb.ListSubscriptionsRequest{
		Project: "projects/" + project,
	})

	for {
		sub, err := it.Next()
		if isIteratorDone(err) {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		if sub.PushConfig == nil || sub.PushConfig.PushEndpoint == "" {
			continue
		}
		if !strings.HasPrefix(sub.PushConfig.PushEndpoint, serviceURL) {
			continue
		}

		topicNodeID := "pubsub_topic:" + sub.Topic
		nodes = append(nodes, models.TopologyNode{ID: topicNodeID, Kind: "pubsub_topic", Name: sub.Topic})
		edges = append(edges, models.TopologyEdge{
			From:         topicNodeID,
			To:           rootID,
			Relationship: "triggers",
			Evidence:     "push_subscription:" + sub.Name,
			Confidence:   "high",
		})
	}

	return nodes, edges, nil
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
