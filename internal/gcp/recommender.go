package gcp

import (
	"context"
	"fmt"
	"math"
	"strings"

	"cloud.google.com/go/recommender/apiv1/recommenderpb"
	"google.golang.org/api/iterator"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

const (
	recommenderIDCloudRunIdle    = "google.run.service.IdentifyIdleService"
	recommenderIDCloudSQLIdle    = "google.cloudsql.instance.IdleRecommender"
	recommenderIDCloudSQLOverpro = "google.cloudsql.instance.OverprovisionedInstanceRecommender"
)

// recommenderInsight holds the extracted fields from a single active Recommender API recommendation.
type recommenderInsight struct {
	subtype        string // "idle" | "overprovisioned"
	description    string
	monthlySavings float64 // USD — positive means money saved
}

type recommendationIterator interface {
	Next() (*recommenderpb.Recommendation, error)
}

type quotaAwareRecommendationIterator struct {
	adapter       *gcpAdapter
	inner         recommendationIterator
	op            string
	recommenderID string
	generation    uint64
}

// activeRecommendations is the only production entry point to
// Recommender.ListRecommendations. It applies the shared quota gate before the
// request and maps iterator-time quota failures consistently for every caller.
func (a *gcpAdapter) activeRecommendations(
	ctx context.Context,
	op, projectID, location, recommenderID string,
	pageSize int32,
) (*quotaAwareRecommendationIterator, error) {
	generation, err := a.checkRecommenderQuota(op, recommenderID)
	if err != nil {
		return nil, err
	}
	parent := fmt.Sprintf("projects/%s/locations/%s/recommenders/%s", projectID, location, recommenderID)
	inner := a.rec.ListRecommendations(ctx, &recommenderpb.ListRecommendationsRequest{
		Parent:   parent,
		Filter:   `stateInfo.state = "ACTIVE"`,
		PageSize: pageSize,
	})
	return &quotaAwareRecommendationIterator{
		adapter:       a,
		inner:         inner,
		op:            op,
		recommenderID: recommenderID,
		generation:    generation,
	}, nil
}

func (it *quotaAwareRecommendationIterator) Next() (*recommenderpb.Recommendation, error) {
	// Check before every Next: generated iterators fetch pages lazily, and do
	// not expose whether the next item is buffered or requires another request.
	// This conservative gate guarantees a known block never reaches Google.
	generation, err := it.adapter.checkRecommenderQuota(it.op, it.recommenderID)
	if err != nil {
		return nil, err
	}
	it.generation = generation
	recommendation, err := it.inner.Next()
	if err == nil || err == iterator.Done {
		// A successful page (including an empty final page) proves that this
		// request's observed quota generation is usable. Do not clear a newer
		// block installed concurrently by another iterator.
		it.adapter.recommenderQuota.succeed(it.recommenderID, it.generation)
		return recommendation, err
	}
	if isRecommenderQuotaError(err) {
		return nil, it.adapter.tripRecommenderQuota(it.op, it.recommenderID, err)
	}
	return nil, wrapGCPError(it.op, err)
}

// fetchRecommenderInsights returns active recommendations that target a specific resource.
// It is intentionally non-fatal: if the caller lacks the recommender.*.list permission
// or the API returns any error, nil insights are returned so the Aura Score can still
// be computed from Cloud Monitoring signals alone.
//
// location is the GCP region (e.g. "us-central1").
// recommenderID is the product-specific recommender name (use the constants above).
// resourceSuffix is a trailing substring of the GCP resource URN used to identify
// the specific resource within the recommendation's TargetResources list
// (e.g. "/services/my-svc" for Cloud Run, "/instances/my-db" for Cloud SQL).
func (a *gcpAdapter) fetchRecommenderInsights(
	ctx context.Context,
	projectID, location, recommenderID, resourceSuffix string,
) ([]recommenderInsight, error) {
	const op = "recommender.fetchInsights"
	if a.rec == nil {
		return nil, nil
	}

	// Cache hit: return stored results without burning any API quota.
	cacheKey := projectID + "|" + location + "|" + recommenderID + "|" + resourceSuffix
	if a.recommenderCache != nil {
		if cached, ok := a.recommenderCache.get(cacheKey); ok {
			return cached, nil
		}
	}

	it, err := a.activeRecommendations(ctx, op, projectID, location, recommenderID, maxInventoryPageSize)
	if err != nil {
		return nil, err
	}

	var insights []recommenderInsight
	for scanned := 0; ; scanned++ {
		rec, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		if scanned >= maxUnpagedInventoryItems {
			return nil, fmt.Errorf("recommender.fetchInsights: %w at %d recommendations", errInventoryLimitReached, maxUnpagedInventoryItems)
		}
		if !recommenderTargetsResource(rec.GetContent(), resourceSuffix) {
			continue
		}
		insights = append(insights, recommenderInsight{
			subtype:        recommendationCategory(rec, recommenderID),
			description:    rec.Description,
			monthlySavings: extractMonthlySavings(rec),
		})
	}

	if a.recommenderCache != nil {
		a.recommenderCache.set(cacheKey, insights)
	}
	return insights, nil
}

// recommenderTargetsResource checks whether any operation in the recommendation's
// content targets a resource whose URN ends with the given suffix.
// Cloud Run URNs look like: //run.googleapis.com/.../services/my-svc
// Cloud SQL URNs look like: //sqladmin.googleapis.com/.../instances/my-db
func recommenderTargetsResource(content *recommenderpb.RecommendationContent, suffix string) bool {
	if content == nil {
		return false
	}
	for _, og := range content.OperationGroups {
		for _, op := range og.Operations {
			if strings.HasSuffix(op.Resource, suffix) {
				return true
			}
		}
	}
	return false
}

// extractMonthlySavings converts the recommendation's primaryImpact cost projection
// into a positive monthly USD savings figure. Returns 0 if no cost data is present.
func extractMonthlySavings(rec *recommenderpb.Recommendation) float64 {
	if rec == nil || rec.PrimaryImpact == nil {
		return 0
	}
	cp := rec.PrimaryImpact.GetCostProjection()
	if cp == nil {
		return 0
	}
	cost := cp.GetCost()
	if cost == nil {
		return 0
	}
	// The API represents savings as a negative cost: units < 0 means money saved.
	savings := -float64(cost.Units) - float64(cost.Nanos)/1e9
	if savings <= 0 {
		return 0
	}
	return math.Round(savings*100) / 100
}

// classifyRecommenderID maps a recommender ID to a human-friendly subtype string
// used as the signal name suffix (e.g. "idle", "overprovisioned").
func classifyRecommenderID(id string) string {
	normalized := strings.ToLower(id)
	if strings.Contains(normalized, "idle") || strings.Contains(normalized, "identifyidle") {
		return "idle"
	}
	if strings.Contains(normalized, "overprovision") || strings.Contains(normalized, "rightsiz") {
		return "overprovisioned"
	}
	return "other"
}

// recommendationSubtype preserves the API's recommendation-level subtype.
// A recommender can emit multiple subtypes, so the recommender ID alone is not
// a sufficient substitute. The ID-derived value is only a compatibility
// fallback for older responses that omit recommender_subtype.
func recommendationSubtype(rec *recommenderpb.Recommendation, recommenderID string) string {
	if rec != nil {
		if subtype := strings.TrimSpace(rec.GetRecommenderSubtype()); subtype != "" {
			return strings.ToLower(subtype)
		}
	}
	return classifyRecommenderID(recommenderID)
}

func recommendationCategory(rec *recommenderpb.Recommendation, recommenderID string) string {
	subtype := recommendationSubtype(rec, recommenderID)
	switch {
	case strings.Contains(subtype, "idle"), strings.Contains(subtype, "unused"):
		return "idle"
	case strings.Contains(subtype, "overprovision"), strings.Contains(subtype, "rightsiz"):
		return "overprovisioned"
	default:
		return classifyRecommenderID(recommenderID)
	}
}

// recommenderSignal converts a recommenderInsight into an AuraHealthSignal that can be
// appended to the signals slice and interpreted by weightedScores / buildReasons.
// The Value field carries the estimated monthly USD savings.
func recommenderSignal(ins recommenderInsight) models.AuraHealthSignal {
	var score int
	switch ins.subtype {
	case "idle":
		score = 20 // idle = very low efficiency
	case "other":
		score = 70
	default:
		score = 45 // overprovisioned penalty
	}
	return models.AuraHealthSignal{
		Name:  "recommender_" + ins.subtype,
		Value: ins.monthlySavings,
		Score: score,
		Label: "Warning",
	}
}
