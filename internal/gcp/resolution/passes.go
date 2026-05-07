package resolution

import (
	"math"
	"strings"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// ─── Pass 1: Direct config ────────────────────────────────────────────────────

func runPass1(c *resolveCtx) {
	pass1Workloads(c)
	pass1K8sServices(c)
	pass1Ingresses(c)
	pass1PubSubPush(c)
	pass1PSCEndpoints(c)
}

// pass1Workloads processes GKE workload summaries:
//   - secret refs → reads_secret edges
//   - env var patterns → reads_from_db edges
func pass1Workloads(c *resolveCtx) {
	for _, w := range c.in.Workloads {
		srcID := c.lookupByName(w.Name, w.Kind)
		if srcID == "" {
			continue
		}

		// Secret refs → reads_secret.
		for _, secretName := range w.SecretRefs {
			tgtID := c.lookupByName(secretName, models.KindSecret)
			if tgtID == "" {
				continue
			}
			c.emit(models.GraphEdge{
				Source:     srcID,
				Target:     tgtID,
				Type:       models.EdgeReadsSecret,
				Evidence:   models.EvidenceK8sSpec,
				Confidence: 0.95,
			})
		}
	}
}

// pass1K8sServices processes K8s Service summaries:
//   - NEG annotation → load_balanced_by edge to a compute_neg node
//   - selector match against workload labels → exposes edge
func pass1K8sServices(c *resolveCtx) {
	for _, svc := range c.in.K8sServices {
		svcID := c.lookupByName(svc.Name, models.KindGKEService)
		if svcID == "" {
			continue
		}

		// NEG annotation: the annotation value contains the NEG name.
		if svc.NEGAnnotation != "" {
			negName := extractNEGName(svc.NEGAnnotation)
			if negName != "" {
				negID := c.lookupByName(negName, models.KindComputeNEG)
				if negID != "" {
					c.emit(models.GraphEdge{
						Source:     svcID,
						Target:     negID,
						Type:       models.EdgeLoadBalancedBy,
						Evidence:   models.EvidenceNEGAnnotation,
						Confidence: 0.90,
					})
				}
			}
		}

		// Selector matching: find workloads whose labels are a superset of the selector.
		if len(svc.Selector) > 0 {
			for _, w := range c.in.Workloads {
				if w.Namespace != svc.Namespace {
					continue
				}
				if labelsMatch(svc.Selector, w.Labels) {
					wID := c.lookupByName(w.Name, w.Kind)
					if wID != "" {
						c.emit(models.GraphEdge{
							Source:     svcID,
							Target:     wID,
							Type:       models.EdgeExposes,
							Evidence:   models.EvidenceK8sSelector,
							Confidence: 0.95,
						})
					}
				}
			}
		}
	}
}

// pass1Ingresses processes K8s Ingress summaries:
//   - GCPLBName → routes_to edge from ingress node to compute_lb node
func pass1Ingresses(c *resolveCtx) {
	for _, ing := range c.in.Ingresses {
		ingID := c.lookupByName(ing.Name, models.KindGKEIngress, models.KindGKEGateway)
		if ingID == "" || ing.GCPLBName == "" {
			continue
		}
		lbID := c.lookupByName(ing.GCPLBName, models.KindComputeLB)
		if lbID == "" {
			continue
		}
		c.emit(models.GraphEdge{
			Source:     ingID,
			Target:     lbID,
			Type:       models.EdgeRoutesTo,
			Evidence:   models.EvidenceComputeBackend,
			Confidence: 0.90,
		})
	}
}

// pass1PubSubPush matches Pub/Sub push subscription endpoints to Cloud Run
// service URLs, minting triggers edges.
func pass1PubSubPush(c *resolveCtx) {
	for _, sub := range c.in.Subscriptions {
		if sub.PushEndpoint == "" {
			continue
		}
		subID := c.lookupByName(sub.Name, models.KindPubSubSubscription)
		if subID == "" {
			continue
		}
		// Match the push endpoint hostname against Cloud Run service URLs.
		for _, n := range c.in.Nodes {
			if n.Kind != models.KindCloudRunService || n.URL == "" {
				continue
			}
			if urlHostMatches(sub.PushEndpoint, n.URL) {
				c.emit(models.GraphEdge{
					Source:     subID,
					Target:     n.ID,
					Type:       models.EdgeTriggers,
					Evidence:   models.EvidenceTopicPushEndpoint,
					Confidence: 0.90,
				})
			}
		}

		// If the push endpoint points outside GCP, mint an external endpoint node.
		if c.in.IncludeExternal && !isGCPManaged(sub.PushEndpoint) {
			extNode := mintExternalEndpoint(sub.PushEndpoint, c.in.Nodes)
			c.minted[extNode.ID] = extNode
			c.emit(models.GraphEdge{
				Source:     subID,
				Target:     extNode.ID,
				Type:       models.EdgeTriggers,
				Evidence:   models.EvidenceTopicPushEndpoint,
				Confidence: 0.90,
			})
		}
	}
}

// pass1PSCEndpoints wires PSC forwarding rules to their target service nodes.
func pass1PSCEndpoints(c *resolveCtx) {
	for _, n := range c.in.Nodes {
		if n.Kind != models.KindPSCEndpoint {
			continue
		}
		// The PSC target service name is stored in Attributes["target_service"].
		targetName, ok := n.Attributes["target_service"]
		if !ok || targetName == "" {
			continue
		}
		tgtID := c.lookupByName(targetName)
		if tgtID == "" {
			continue
		}
		c.emit(models.GraphEdge{
			Source:     n.ID,
			Target:     tgtID,
			Type:       models.EdgePSCConnectsTo,
			Evidence:   models.EvidenceComputeBackend,
			Confidence: 0.95,
		})
	}
}

// ─── Pass 2: IAM bindings ─────────────────────────────────────────────────────

func runPass2(c *resolveCtx) {
	if len(c.in.IAMBindingsByResource) == 0 || len(c.in.WorkloadSAByNodeID) == 0 {
		return
	}

	// Build a reverse map: SA email → []workload node IDs.
	saToWorkloads := make(map[string][]string)
	for nodeID, saEmail := range c.in.WorkloadSAByNodeID {
		saToWorkloads[saEmail] = append(saToWorkloads[saEmail], nodeID)
	}

	for resourceURN, bindings := range c.in.IAMBindingsByResource {
		tgtNode, ok := c.byID[resourceURN]
		if !ok {
			continue
		}
		for _, binding := range bindings {
			for _, member := range binding.Members {
				// member format: "serviceAccount:foo@project.iam.gserviceaccount.com"
				email := strings.TrimPrefix(member, "serviceAccount:")
				workloads, found := saToWorkloads[email]
				if !found {
					continue
				}
				edgeType, confidence := iamEdgeType(binding.Role, tgtNode.Kind)
				if edgeType == "" {
					continue
				}
				for _, wID := range workloads {
					// Only emit if no Pass 1 edge already covers this (source, target, type) pair.
					k := edgeKey{wID, resourceURN, edgeType}
					if _, exists := c.edges[k]; exists {
						continue
					}
					c.emit(models.GraphEdge{
						Source:     wID,
						Target:     resourceURN,
						Type:       edgeType,
						Evidence:   models.EvidenceIAMBindingScoped,
						Confidence: confidence,
						Metadata:   map[string]string{"unobserved_at_runtime": "true"},
					})
				}
			}
		}
	}
}

// iamEdgeType maps a GCP IAM role + target node kind to an edge type and confidence.
func iamEdgeType(role, targetKind string) (string, float64) {
	switch {
	case strings.Contains(role, "spanner.databaseUser") || strings.Contains(role, "spanner.viewer"):
		if targetKind == models.KindSpannerInstance {
			return models.EdgeReadsFromDB, 0.70
		}
	case strings.Contains(role, "redis."):
		if targetKind == models.KindMemorystoreInstance {
			return models.EdgeReadsFromDB, 0.70
		}
	case strings.Contains(role, "datastore.") || strings.Contains(role, "firestore."):
		if targetKind == models.KindFirestoreDatabase {
			return models.EdgeReadsFromDB, 0.70
		}
	case strings.Contains(role, "alloydb."):
		if targetKind == models.KindAlloyDBCluster {
			return models.EdgeReadsFromDB, 0.70
		}
	case strings.Contains(role, "cloudsql."):
		if targetKind == models.KindCloudSQLInstance {
			return models.EdgeReadsFromDB, 0.70
		}
	case strings.Contains(role, "pubsub.publisher"):
		if targetKind == models.KindPubSubTopic {
			return models.EdgePublishesTo, 0.70
		}
	case strings.Contains(role, "secretmanager.secretAccessor") || strings.Contains(role, "secretmanager.viewer"):
		if targetKind == models.KindSecret {
			return models.EdgeReadsSecret, 0.65
		}
	}
	return "", 0
}

// ─── Pass 3: Service mesh telemetry ──────────────────────────────────────────

func runPass3(c *resolveCtx) {
	for _, me := range c.in.MeshEdges {
		callerID := c.lookupByName(me.Caller)
		calleeID := c.lookupByName(me.Callee)
		if callerID == "" || calleeID == "" {
			continue
		}
		meta := map[string]string{}
		if me.CallerNamespace != "" {
			meta["caller_namespace"] = me.CallerNamespace
		}
		if me.CalleeNamespace != "" {
			meta["callee_namespace"] = me.CalleeNamespace
		}
		c.emit(models.GraphEdge{
			Source:     callerID,
			Target:     calleeID,
			Type:       models.EdgeMeshCalls,
			Evidence:   models.EvidenceMeshTelemetry,
			Confidence: 0.85,
			Metadata:   meta,
		})
	}
}

// ─── Pass 4: Cloud Trace spans ────────────────────────────────────────────────

func runPass4(c *resolveCtx) {
	for _, te := range c.in.TraceEdges {
		callerID := c.lookupByName(te.Caller)
		calleeID := c.lookupByName(te.Callee)
		if callerID == "" || calleeID == "" {
			continue
		}
		// confidence = 0.90 × min(1, sampleCount/10)
		conf := 0.90 * math.Min(1.0, float64(te.SampleCount)/10.0)
		if conf < 0.05 {
			conf = 0.05 // floor so single-sample edges still appear
		}
		c.emit(models.GraphEdge{
			Source:     callerID,
			Target:     calleeID,
			Type:       models.EdgeTraceCalls,
			Evidence:   models.EvidenceTraceSpan,
			Confidence: conf,
		})
	}
}

// ─── Pass 5: Log / VPC flow log (opt-in, low confidence) ─────────────────────

func runPass5(_ *resolveCtx) {
	// Stub: VPC Flow Log and structured HTTP log inference require log entries
	// not currently collected in ResolutionInput. Populated in a future PR when
	// log-based inference is added to the architecture graph fan-out.
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// labelsMatch returns true if all selector key/value pairs are present in labels.
func labelsMatch(selector, labels map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// urlHostMatches returns true if pushEndpoint and serviceURL share the same host.
func urlHostMatches(pushEndpoint, serviceURL string) bool {
	return extractHost(pushEndpoint) == extractHost(serviceURL)
}

func extractHost(u string) string {
	// Strip scheme.
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	// Strip path.
	if i := strings.Index(u, "/"); i >= 0 {
		u = u[:i]
	}
	return strings.ToLower(u)
}

// extractNEGName parses the NEG name from the cloud.google.com/neg annotation
// value. The annotation is a JSON object like:
//
//	{"ingress":true,"exposed_ports":{"80":{"name":"my-neg"}}}
//
// For simplicity we look for a "name" key in the JSON string.
func extractNEGName(annotation string) string {
	const nameKey = `"name":"`
	i := strings.Index(annotation, nameKey)
	if i < 0 {
		return ""
	}
	start := i + len(nameKey)
	end := strings.Index(annotation[start:], `"`)
	if end < 0 {
		return ""
	}
	return annotation[start : start+end]
}
