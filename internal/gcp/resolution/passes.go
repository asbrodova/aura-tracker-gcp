package resolution

import (
	"math"
	"strings"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// ─── Pass 1: Direct config ────────────────────────────────────────────────────

func runPass1(c *resolveCtx) {
	pass1K8sServices(c)
	pass1Ingresses(c)
	pass1Eventarc(c)
	pass1Scheduler(c)
	pass1PubSubTopics(c)
	pass1PubSubPush(c)
	pass1PSCEndpoints(c)
}

// pass1Eventarc wires triggers to their configured destination and transport
// topic. DestinationURN is a legacy field name; listers currently populate it
// with either a bare resource name or a GCP resource path.
func pass1Eventarc(c *resolveCtx) {
	for _, trigger := range c.in.Triggers {
		sourceID := c.lookupByName(trigger.Name, models.KindEventarcTrigger)
		if sourceID == "" {
			continue
		}
		if destination := lastResourceSegment(trigger.DestinationURN); destination != "" {
			if targetID := c.lookupByName(destination); targetID != "" {
				c.emit(models.GraphEdge{
					Source: sourceID, Target: targetID, Type: models.EdgeTriggers,
					Evidence: models.EvidenceEventarcDestination, Confidence: 0.95,
				})
			}
		}
		if topic := lastResourceSegment(trigger.TransportTopic); topic != "" {
			if targetID := c.lookupByName(topic, models.KindPubSubTopic); targetID != "" {
				c.emit(models.GraphEdge{
					Source: sourceID, Target: targetID, Type: models.EdgeRoutesTo,
					Evidence: models.EvidenceEventarcDestination, Confidence: 0.90,
				})
			}
		}
	}
}

func pass1Scheduler(c *resolveCtx) {
	for _, job := range c.in.SchedulerJobs {
		sourceID := c.lookupByName(job.Name, models.KindSchedulerJob)
		if sourceID == "" || job.TargetRef == "" {
			continue
		}
		switch job.TargetKind {
		case "pubsub":
			if targetID := c.lookupByName(lastResourceSegment(job.TargetRef), models.KindPubSubTopic); targetID != "" {
				c.emit(models.GraphEdge{Source: sourceID, Target: targetID, Type: models.EdgeTriggers, Evidence: models.EvidenceSchedulerTarget, Confidence: 0.95})
			}
		case "http":
			for _, node := range c.in.Nodes {
				if node.URL != "" && urlHostMatches(job.TargetRef, node.URL) {
					c.emit(models.GraphEdge{Source: sourceID, Target: node.ID, Type: models.EdgeTriggers, Evidence: models.EvidenceSchedulerTarget, Confidence: 0.85})
					break
				}
			}
		}
	}
}

func pass1PubSubTopics(c *resolveCtx) {
	for _, subscription := range c.in.Subscriptions {
		sourceID := c.lookupByName(subscription.Name, models.KindPubSubSubscription)
		if sourceID == "" {
			continue
		}
		if topicID := c.lookupByName(lastResourceSegment(subscription.Topic), models.KindPubSubTopic); topicID != "" {
			c.emit(models.GraphEdge{Source: sourceID, Target: topicID, Type: models.EdgeSubscribesTo, Evidence: models.EvidenceTopicPushEndpoint, Confidence: 0.95})
		}
		if deadLetterID := c.lookupByName(lastResourceSegment(subscription.DeadLetterTopic), models.KindPubSubTopic); deadLetterID != "" {
			c.emit(models.GraphEdge{Source: sourceID, Target: deadLetterID, Type: models.EdgeDeadLettersTo, Evidence: models.EvidenceTopicPushEndpoint, Confidence: 0.95})
		}
	}
}

// pass1K8sServices processes K8s Service summaries:
//   - NEG annotation → load_balanced_by edge to a compute_neg node
//   - selector match against workload labels → exposes edge
func pass1K8sServices(c *resolveCtx) {
	for _, svc := range c.in.K8sServices {
		svcID := c.summaryNodeID(svc.GraphNodeID, svc.Name, svc.Namespace, svc.ClusterName, svc.ClusterLocation, models.KindGKEService)
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
				if w.Namespace != svc.Namespace ||
					(svc.ClusterName != "" && w.ClusterName != svc.ClusterName) ||
					(svc.ClusterLocation != "" && w.ClusterLocation != svc.ClusterLocation) {
					continue
				}
				if labelsMatch(svc.Selector, w.Labels) {
					wID := c.summaryNodeID(w.GraphNodeID, w.Name, w.Namespace, w.ClusterName, w.ClusterLocation, w.Kind)
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
		ingID := c.summaryNodeID(ing.GraphNodeID, ing.Name, ing.Namespace, ing.ClusterName, ing.ClusterLocation, models.KindGKEIngress, models.KindGKEGateway)
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
		callerID := c.lookupScoped(me.Caller, me.CallerNamespace, me.ClusterName, me.ClusterLocation, gkeCallableKinds...)
		calleeID := c.lookupScoped(me.Callee, me.CalleeNamespace, me.ClusterName, me.ClusterLocation, gkeCallableKinds...)
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

// ─── Helpers ──────────────────────────────────────────────────────────────────

// labelsMatch returns true if all selector key/value pairs are present in labels.
var gkeCallableKinds = []string{
	models.KindGKEDeployment,
	models.KindGKEStatefulSet,
	models.KindGKEDaemonSet,
	models.KindGKECronJob,
	models.KindGKEJob,
	models.KindGKEService,
}

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

func lastResourceSegment(value string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return ""
	}
	if i := strings.LastIndex(value, "/"); i >= 0 {
		return value[i+1:]
	}
	return value
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
