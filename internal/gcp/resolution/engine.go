// Package resolution implements the edge-inference rules engine for the
// architecture graph. It is a pure function package: no GCP API calls,
// no global state, and fully deterministic given the same input.
//
// The engine runs up to five ordered passes over the raw resource inventory
// assembled by the architecture graph fan-out, then deduplicates and sorts the
// resulting edge set.
//
// Pass 1 — Direct config  (confidence 0.90–0.99)
// Pass 2 — IAM bindings   (confidence 0.65–0.75)
// Pass 3 — Mesh telemetry (confidence 0.85)
// Pass 4 — Cloud Trace    (confidence ≤ 0.90)
// Pass 5 — Log / flow log (confidence 0.45–0.55, opt-in)
package resolution

import (
	"sort"
	"strings"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// Resolve runs all inference passes on in and returns deduplicated, sorted
// edges plus any external endpoint nodes minted during the run.
func Resolve(in models.ResolutionInput) models.ResolutionOutput {
	// Build a fast lookup: node ID → *GraphNode and name → ID for each kind.
	nodeByID := make(map[string]*models.GraphNode, len(in.Nodes))
	for i := range in.Nodes {
		nodeByID[in.Nodes[i].ID] = &in.Nodes[i]
	}

	ctx := &resolveCtx{
		in:      in,
		byID:    nodeByID,
		byName:  buildNameIndex(in.Nodes),
		edges:   map[edgeKey]models.GraphEdge{},
		minted:  map[string]models.GraphNode{},
	}

	runPass1(ctx)
	runPass2(ctx)
	runPass3(ctx)
	runPass4(ctx)
	if in.EnableFlowLogInference {
		runPass5(ctx)
	}

	return ctx.collect()
}

// resolveCtx is the shared mutable context passed through all passes.
type resolveCtx struct {
	in     models.ResolutionInput
	byID   map[string]*models.GraphNode
	byName map[string][]string // name (lower) → []nodeID (may be multiple kinds)
	edges  map[edgeKey]models.GraphEdge
	minted map[string]models.GraphNode // minted external endpoint nodes by ID
}

// edgeKey deduplicates edges by source + target + type.
type edgeKey struct{ source, target, edgeType string }

// emit adds an edge to the set, applying corroboration boost and merging
// evidence strings when a prior edge for the same key already exists.
func (c *resolveCtx) emit(e models.GraphEdge) {
	k := edgeKey{e.Source, e.Target, e.Type}
	existing, ok := c.edges[k]
	if !ok {
		c.edges[k] = e
		return
	}
	// Corroboration boost: +0.05 per additional pass, capped at 1.0.
	boosted := existing.Confidence + 0.05
	if boosted > 1.0 {
		boosted = 1.0
	}
	// Keep higher confidence.
	if e.Confidence > boosted {
		boosted = e.Confidence
	}
	// Merge evidence strings.
	evid := existing.Evidence
	if e.Evidence != "" && e.Evidence != existing.Evidence {
		evid = existing.Evidence + "|" + e.Evidence
	}
	// Merge metadata.
	meta := existing.Metadata
	for mk, mv := range e.Metadata {
		if meta == nil {
			meta = map[string]string{}
		}
		if _, present := meta[mk]; !present {
			meta[mk] = mv
		}
	}
	c.edges[k] = models.GraphEdge{
		Source:     existing.Source,
		Target:     existing.Target,
		Type:       existing.Type,
		Evidence:   evid,
		Confidence: boosted,
		Metadata:   meta,
	}
}

// collect builds the sorted output from accumulated edges and minted nodes.
func (c *resolveCtx) collect() models.ResolutionOutput {
	edges := make([]models.GraphEdge, 0, len(c.edges))
	for _, e := range c.edges {
		edges = append(edges, e)
	}
	sort.Slice(edges, func(i, j int) bool {
		ki := edges[i].Source + ":" + edges[i].Target + ":" + edges[i].Type
		kj := edges[j].Source + ":" + edges[j].Target + ":" + edges[j].Type
		return ki < kj
	})

	minted := make([]models.GraphNode, 0, len(c.minted))
	for _, n := range c.minted {
		minted = append(minted, n)
	}
	sort.Slice(minted, func(i, j int) bool { return minted[i].ID < minted[j].ID })

	return models.ResolutionOutput{Edges: edges, MintedNodes: minted}
}

// buildNameIndex builds a map from lowercase node name → []nodeID.
// When multiple nodes share a name (e.g. same workload in different clusters),
// all IDs are returned and the caller chooses the best match by kind.
func buildNameIndex(nodes []models.GraphNode) map[string][]string {
	m := make(map[string][]string, len(nodes))
	for _, n := range nodes {
		key := strings.ToLower(n.Name)
		m[key] = append(m[key], n.ID)
	}
	return m
}

// lookupByName returns the first node ID whose name matches (case-insensitive)
// and whose kind is in preferredKinds (or any kind if preferredKinds is empty).
func (c *resolveCtx) lookupByName(name string, preferredKinds ...string) string {
	ids := c.byName[strings.ToLower(name)]
	if len(ids) == 0 {
		return ""
	}
	if len(preferredKinds) == 0 {
		return ids[0]
	}
	kindSet := make(map[string]bool, len(preferredKinds))
	for _, k := range preferredKinds {
		kindSet[k] = true
	}
	for _, id := range ids {
		if n, ok := c.byID[id]; ok && kindSet[n.Kind] {
			return id
		}
	}
	return ids[0] // fall back to any match
}
