package gcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cloud.google.com/go/recommender/apiv1/recommenderpb"
	"google.golang.org/api/iterator"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

var costRecommenderIDs = []string{
	"google.cloudsql.instance.IdleRecommender",
	"google.compute.instance.IdleResourceRecommender",
	"google.compute.disk.IdleResourceRecommender",
	"google.compute.address.IdleResourceRecommender",
	"google.compute.image.IdleResourceRecommender",
	"google.compute.IdleResourceRecommender",
	"google.container.DiagnosisRecommender",
}

// ListCostRecommendations returns active, project-wide cost optimization
// recommendations. Per-recommender failures are coverage warnings so one
// unavailable product does not discard results from other products.
func (a *gcpAdapter) ListCostRecommendations(ctx context.Context, req models.ListCostRecommendationsRequest) (models.ListCostRecommendationsResponse, error) {
	const op = "cost.ListCostRecommendations"
	if !a.enableCostReasoning {
		return models.ListCostRecommendationsResponse{}, fmt.Errorf("%s: cost reasoning is not configured", op)
	}
	if a.rec == nil {
		return models.ListCostRecommendationsResponse{
			Available: false, Recommendations: []models.CostRecommendation{},
			Warnings: []string{"Recommender integration is disabled; idle-resource findings were skipped."},
		}, nil
	}
	if !costProjectIDRE.MatchString(req.ProjectID) {
		return models.ListCostRecommendationsResponse{}, fmt.Errorf("%s: invalid project ID %q", op, req.ProjectID)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	cacheKey := fmt.Sprintf("%s|limit=%d", req.ProjectID, limit)
	if a.costRecommendationCache != nil {
		if cached, ok := a.costRecommendationCache.get(cacheKey); ok {
			return cached, nil
		}
	}
	if err := a.rateWait(ctx, op); err != nil {
		return models.ListCostRecommendationsResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()
	result := models.ListCostRecommendationsResponse{Available: true, Complete: true, Recommendations: []models.CostRecommendation{}, Warnings: []string{}}
	failed := 0
	limitReached := false
	collecting := true
	for _, recommenderID := range costRecommenderIDs {
		if !collecting {
			break
		}
		it, err := a.activeRecommendations(ctx, op, req.ProjectID, "-", recommenderID, maxInventoryPageSize)
		if err != nil {
			return result, err
		}
		for scanned := 0; ; scanned++ {
			recommendation, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				var quotaErr *ports.RecommenderQuotaExhaustedError
				if errors.As(err, &quotaErr) {
					return result, err
				}
				failed++
				break
			}
			if scanned >= maxFilteredInventoryScanItems {
				failed++
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s scan reached the %d-recommendation safety cap", recommenderID, maxFilteredInventoryScanItems))
				break
			}
			if !isCostRecommendation(recommendation, recommenderID) {
				continue
			}
			resource := primaryTargetResource(recommendation)
			if resource == "" {
				continue
			}
			priority := "UNSPECIFIED"
			if recommendation.Priority != recommenderpb.Recommendation_PRIORITY_UNSPECIFIED {
				priority = recommendation.Priority.String()
			}
			result.Recommendations = append(result.Recommendations, models.CostRecommendation{
				Resource: resource, RecommenderID: recommenderID,
				Subtype: costRecommendationSubtype(recommendation, recommenderID), Service: costRecommendationService(recommenderID),
				Description: recommendation.Description, Priority: priority, MonthlySavingsUSD: extractMonthlySavings(recommendation),
			})
			if len(result.Recommendations) > limit {
				limitReached = true
				collecting = false
				break
			}
		}
	}
	if limitReached {
		result.Recommendations = result.Recommendations[:limit]
		result.Complete = false
	}
	if failed > 0 {
		result.Complete = false
		result.Warnings = append(result.Warnings, fmt.Sprintf("%d cost recommender type(s) were unavailable", failed))
	}
	if limitReached {
		result.Warnings = append(result.Warnings, "Cost recommendation result limit reached; additional idle resources may exist.")
	}
	if a.costRecommendationCache != nil && result.Complete {
		a.costRecommendationCache.set(cacheKey, result)
	}
	return result, nil
}

func isCostRecommendation(rec *recommenderpb.Recommendation, recommenderID string) bool {
	if rec == nil {
		return false
	}
	if strings.Contains(recommenderID, "container.Diagnosis") {
		text := strings.ToLower(rec.RecommenderSubtype + " " + rec.Description)
		return strings.Contains(text, "idle") || strings.Contains(text, "cost") || strings.Contains(text, "unused")
	}
	return true
}

func costRecommendationSubtype(rec *recommenderpb.Recommendation, recommenderID string) string {
	subtype := recommendationSubtype(rec, recommenderID)
	if subtype == "other" {
		return "cost_optimization"
	}
	return subtype
}

func costRecommendationService(id string) string {
	switch {
	case strings.Contains(id, "cloudsql"):
		return "Cloud SQL"
	case strings.Contains(id, "container"):
		return "Google Kubernetes Engine"
	case strings.Contains(id, "compute"):
		return "Compute Engine"
	default:
		return ""
	}
}
