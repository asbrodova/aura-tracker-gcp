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
	subtype        string  // "idle" | "overprovisioned"
	description    string
	monthlySavings float64 // USD — positive means money saved
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
	if a.rec == nil {
		return nil, nil
	}

	parent := fmt.Sprintf("projects/%s/locations/%s/recommenders/%s", projectID, location, recommenderID)
	req := &recommenderpb.ListRecommendationsRequest{
		Parent: parent,
		Filter: `stateInfo.state = "ACTIVE"`,
	}

	var insights []recommenderInsight
	it := a.rec.ListRecommendations(ctx, req)
	for {
		rec, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		if !recommenderTargetsResource(rec.GetContent(), resourceSuffix) {
			continue
		}
		insights = append(insights, recommenderInsight{
			subtype:        classifyRecommenderID(recommenderID),
			description:    rec.Description,
			monthlySavings: extractMonthlySavings(rec),
		})
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
	if strings.Contains(id, "Idle") || strings.Contains(id, "IdentifyIdle") {
		return "idle"
	}
	return "overprovisioned"
}

// recommenderSignal converts a recommenderInsight into an AuraHealthSignal that can be
// appended to the signals slice and interpreted by weightedScores / buildReasons.
// The Value field carries the estimated monthly USD savings.
func recommenderSignal(ins recommenderInsight) models.AuraHealthSignal {
	score := 45 // overprovisioned penalty
	if ins.subtype == "idle" {
		score = 20 // idle = very low efficiency
	}
	return models.AuraHealthSignal{
		Name:  "recommender_" + ins.subtype,
		Value: ins.monthlySavings,
		Score: score,
		Label: "Warning",
	}
}
