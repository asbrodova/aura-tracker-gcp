package costreasoning

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func newResponse(req models.ExplainCostRequest, w analysisWindows, facts models.BillingCostFacts, now time.Time) models.ExplainCostResponse {
	total := findFact(facts.Facts, "total", "total")
	totals := models.CostComparison{
		Currency: facts.Currency, CurrentGrossCost: total.Current.GrossCost, CurrentCredits: total.Current.Credits,
		CurrentNetCost: total.Current.NetCost, BaselineGrossCost: total.Baseline.GrossCost,
		BaselineCredits: total.Baseline.Credits, BaselineNetCost: total.Baseline.NetCost,
		Delta: total.Current.NetCost - total.Baseline.NetCost,
	}
	totals.PercentChange = percentChange(totals.CurrentNetCost, totals.BaselineNetCost)
	totals.PercentChangeDefined = math.Abs(totals.BaselineNetCost) >= 1e-9
	analysisRaw := fmt.Sprintf("%s|%s|%s|%s", req.ProjectID, w.currentStart, w.currentEnd, reasoningVersion)
	sum := sha256.Sum256([]byte(analysisRaw))
	response := models.ExplainCostResponse{
		AnalysisID: hex.EncodeToString(sum[:8]), GeneratedAt: now.Format(time.RFC3339), Status: "complete",
		ReasoningVersion: reasoningVersion,
		Scope: models.CostScope{
			ProjectID: req.ProjectID, Current: models.CostWindow{Start: w.currentStart.Format(time.RFC3339), End: w.currentEnd.Format(time.RFC3339)},
			Baseline:     models.CostWindow{Start: w.baselineStart.Format(time.RFC3339), End: w.baselineEnd.Format(time.RFC3339)},
			HistoryStart: w.historyStart.Format(time.RFC3339), Timezone: req.Timezone, Comparison: req.Comparison,
			CostBasis: "usage_net", CurrentComplete: w.complete,
		},
		Totals: totals, History: completeDailyHistory(facts.Daily, facts.ExportStart, w, req.Timezone),
		Drivers: []models.CostDriver{}, TopSpenders: []models.CostContributor{}, TopIncreases: []models.CostContributor{},
		NewResources: []models.NewCostResource{}, IdleResources: []models.IdleCostResource{}, TrafficAnomalies: []models.CostTrafficAnomaly{},
		Coverage: models.CostCoverage{Checks: []models.CostCoverageCheck{{Name: "detailed_billing_export", Status: "complete"}}, BytesProcessed: facts.BytesProcessed},
		Warnings: append([]string(nil), facts.Warnings...),
	}
	if facts.CurrencyCount > 1 {
		response.Totals.Currency = "MULTIPLE"
		response.Coverage.Checks = append(response.Coverage.Checks, models.CostCoverageCheck{
			Name: "billing_currency", Status: "partial", Message: "The selected export rows contain multiple currencies and were not converted.",
		})
		response.Warnings = append(response.Warnings, "Billing export rows contain multiple currencies; aggregated amounts were not converted and are not directly comparable.")
	} else {
		response.Coverage.Checks = append(response.Coverage.Checks, models.CostCoverageCheck{Name: "billing_currency", Status: "complete"})
	}
	return response
}

func completeDailyHistory(daily []models.HistoricalCost, exportStart string, w analysisWindows, timezone string) []models.HistoricalCost {
	if len(daily) == 0 {
		return []models.HistoricalCost{}
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return append([]models.HistoricalCost(nil), daily...)
	}
	byDate := make(map[string]models.HistoricalCost, len(daily))
	for _, point := range daily {
		byDate[point.Date] = point
	}
	start := w.historyStart.In(location)
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, location)
	if exported, parseErr := time.Parse(time.RFC3339, exportStart); parseErr == nil {
		exported = exported.In(location)
		exportDay := time.Date(exported.Year(), exported.Month(), exported.Day(), 0, 0, 0, 0, location)
		if exportDay.After(start) {
			start = exportDay
		}
	}
	end := w.currentEnd.In(location)
	end = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, location)
	result := make([]models.HistoricalCost, 0, calendarDays(start, end))
	for cursor := start; cursor.Before(end); cursor = cursor.AddDate(0, 0, 1) {
		date := cursor.Format("2006-01-02")
		point, ok := byDate[date]
		if !ok {
			point = models.HistoricalCost{Date: date}
		}
		result = append(result, point)
	}
	return result
}

func analyzeResponse(response *models.ExplainCostResponse, facts models.BillingCostFacts, assets models.ListCreatedAssetsResponse,
	recommendations models.ListCostRecommendationsResponse, w analysisWindows, maxResults int, includeTraffic bool,
) {
	response.TopSpenders = rankContributors(facts.Facts, response.Totals.CurrentNetCost, maxResults, false)
	response.TopIncreases = rankContributors(facts.Facts, response.Totals.CurrentNetCost, maxResults, true)
	response.NewResources = findNewResources(facts, assets, w, maxResults)
	response.IdleResources = findIdleResources(facts.Facts, recommendations.Recommendations, maxResults)
	if includeTraffic {
		response.TrafficAnomalies = findTrafficAnomalies(facts.Facts, response.Totals.Delta, maxResults)
		response.Coverage.Checks = append(response.Coverage.Checks, models.CostCoverageCheck{Name: "billing_traffic", Status: "complete"})
	} else {
		response.Coverage.Checks = append(response.Coverage.Checks, models.CostCoverageCheck{Name: "billing_traffic", Status: "skipped", Message: "include_traffic=false"})
	}
	response.Drivers = buildDrivers(facts, response.NewResources, response.TrafficAnomalies, maxResults)
}

func findFact(facts []models.CostFact, dimension, key string) models.CostFact {
	for _, fact := range facts {
		if fact.Dimension == dimension && fact.Key == key {
			return fact
		}
	}
	return models.CostFact{}
}

func rankContributors(facts []models.CostFact, totalCurrent float64, maxResults int, byDelta bool) []models.CostContributor {
	dimensions := []string{"service", "sku", "resource"}
	result := make([]models.CostContributor, 0, len(dimensions)*maxResults)
	for _, dimension := range dimensions {
		var matching []models.CostFact
		for _, fact := range facts {
			if fact.Dimension != dimension {
				continue
			}
			delta := fact.Current.NetCost - fact.Baseline.NetCost
			if (byDelta && delta <= 0) || (!byDelta && fact.Current.NetCost <= 0) {
				continue
			}
			matching = append(matching, fact)
		}
		sort.SliceStable(matching, func(i, j int) bool {
			left, right := matching[i].Current.NetCost, matching[j].Current.NetCost
			if byDelta {
				left = matching[i].Current.NetCost - matching[i].Baseline.NetCost
				right = matching[j].Current.NetCost - matching[j].Baseline.NetCost
			}
			if left == right {
				return matching[i].Key < matching[j].Key
			}
			return left > right
		})
		if len(matching) > maxResults {
			matching = matching[:maxResults]
		}
		for i, fact := range matching {
			delta := fact.Current.NetCost - fact.Baseline.NetCost
			result = append(result, models.CostContributor{
				Rank: i + 1, Dimension: dimension, Key: fact.Key, Service: fact.Service, SKU: fact.SKU,
				Resource: fact.Resource, Location: fact.Location, CurrentCost: fact.Current.NetCost,
				BaselineCost: fact.Baseline.NetCost, Delta: delta,
				PercentChange:       percentChange(fact.Current.NetCost, fact.Baseline.NetCost),
				CurrentSpendPercent: safePercent(fact.Current.NetCost, totalCurrent),
			})
		}
	}
	return result
}

func findNewResources(facts models.BillingCostFacts, assets models.ListCreatedAssetsResponse, w analysisWindows, maxResults int) []models.NewCostResource {
	result := make([]models.NewCostResource, 0)
	for _, observed := range facts.FirstSeen {
		first, err := time.Parse(time.RFC3339, observed.FirstSeen)
		if err != nil || first.Before(w.currentStart) || !first.Before(w.currentEnd) || observed.CurrentCost <= 0 {
			continue
		}
		classification, confidence, createdAt := "newly_billed", "medium", ""
		for _, asset := range assets.Assets {
			if resourceMatch(observed.Resource, asset.Name) > 0 {
				created, err := time.Parse(time.RFC3339, asset.CreateTime)
				if err == nil && !created.Before(w.currentStart) && created.Before(w.currentEnd) {
					classification, confidence, createdAt = "confirmed_new", "high", asset.CreateTime
					break
				}
			}
		}
		result = append(result, models.NewCostResource{
			Resource: observed.Resource, Service: observed.Service, FirstBilledAt: observed.FirstSeen,
			CreatedAt: createdAt, Classification: classification, CurrentCost: observed.CurrentCost, Confidence: confidence,
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].CurrentCost == result[j].CurrentCost {
			return result[i].Resource < result[j].Resource
		}
		return result[i].CurrentCost > result[j].CurrentCost
	})
	if len(result) > maxResults {
		result = result[:maxResults]
	}
	return result
}

func findIdleResources(facts []models.CostFact, recommendations []models.CostRecommendation, maxResults int) []models.IdleCostResource {
	resources := make([]models.CostFact, 0)
	for _, fact := range facts {
		if fact.Dimension == "resource" {
			resources = append(resources, fact)
		}
	}
	result := make([]models.IdleCostResource, 0)
	for _, recommendation := range recommendations {
		if strings.TrimSpace(recommendation.Resource) == "" {
			continue
		}
		currentCost, confidence, bestMatch := 0.0, "low", 0
		for _, fact := range resources {
			match := resourceMatch(recommendation.Resource, fact.Resource)
			if match > bestMatch {
				bestMatch, currentCost = match, fact.Current.NetCost
			}
		}
		switch bestMatch {
		case 2:
			confidence = "high"
		case 1:
			confidence = "medium"
		}
		result = append(result, models.IdleCostResource{
			Resource: recommendation.Resource, Service: recommendation.Service, Subtype: recommendation.Subtype,
			Description: recommendation.Description, Priority: recommendation.Priority, CurrentCost: currentCost,
			MonthlySavingsUSD: recommendation.MonthlySavingsUSD, Confidence: confidence,
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		left := result[i].MonthlySavingsUSD
		right := result[j].MonthlySavingsUSD
		if left == right {
			return result[i].CurrentCost > result[j].CurrentCost
		}
		return left > right
	})
	if len(result) > maxResults {
		result = result[:maxResults]
	}
	return result
}

func buildDrivers(facts models.BillingCostFacts, newResources []models.NewCostResource, traffic []models.CostTrafficAnomaly, maxResults int) []models.CostDriver {
	total := findFact(facts.Facts, "total", "total")
	totalDelta := total.Current.NetCost - total.Baseline.NetCost
	materiality := math.Max(0.01, math.Abs(totalDelta)*0.01)
	newByResource := make(map[string]models.NewCostResource)
	for _, resource := range newResources {
		newByResource[resource.Resource] = resource
	}
	trafficByResourceSKU := make(map[string]models.CostTrafficAnomaly)
	for _, anomaly := range traffic {
		trafficByResourceSKU[anomaly.Resource+"|"+anomaly.SKU] = anomaly
	}
	children := make(map[string][]models.CostFact)
	resources := make([]models.CostFact, 0)
	services := make(map[string]models.CostFact)
	for _, fact := range facts.Facts {
		switch fact.Dimension {
		case "resource_sku":
			children[fact.Resource] = append(children[fact.Resource], fact)
		case "resource":
			resources = append(resources, fact)
		case "service":
			services[fact.Service] = fact
		}
	}
	drivers := make([]models.CostDriver, 0)
	resourceDeltaByService := make(map[string]float64)
	for _, resource := range resources {
		delta := resource.Current.NetCost - resource.Baseline.NetCost
		resourceDeltaByService[resource.Service] += delta
		if math.Abs(delta) < materiality {
			continue
		}
		driver := models.CostDriver{
			Dimension: "resource", Key: resource.Resource, CurrentCost: resource.Current.NetCost,
			BaselineCost: resource.Baseline.NetCost, Delta: delta, Category: "unattributed",
			Title: "Resource cost changed", Confidence: "medium",
			Evidence: []models.CostEvidence{costEvidence("billing_export", "resource_cost", "Resource net cost changed between equal periods", resource.Current.NetCost, resource.Baseline.NetCost, facts.Currency)},
		}
		if newResource, ok := newByResource[resource.Resource]; ok {
			driver.Category = newResource.Classification
			driver.Title = "New resource began generating cost"
			driver.Confidence = newResource.Confidence
			driver.Evidence = append(driver.Evidence, models.CostEvidence{Source: "billing_export", Signal: "first_billed", Summary: "Resource was first billed during the current period", Attributes: map[string]any{"first_billed_at": newResource.FirstBilledAt, "created_at": newResource.CreatedAt}})
		} else if child, ok := dominantChild(children[resource.Resource]); ok {
			classifyUsageDriver(&driver, child, trafficByResourceSKU[child.Resource+"|"+child.SKU], facts.Currency)
		}
		drivers = append(drivers, driver)
	}
	for service, serviceFact := range services {
		serviceDelta := serviceFact.Current.NetCost - serviceFact.Baseline.NetCost
		residual := serviceDelta - resourceDeltaByService[service]
		if math.Abs(residual) < materiality {
			continue
		}
		drivers = append(drivers, models.CostDriver{
			Dimension: "service", Key: service + " (unattributed resources)", CurrentCost: serviceFact.Current.NetCost,
			BaselineCost: serviceFact.Baseline.NetCost, Delta: residual, Category: "unattributed",
			Title: "Service-level cost could not be attributed to a resource", Confidence: "low",
			Evidence: []models.CostEvidence{costEvidence("billing_export", "service_residual", "Detailed export did not attach all service cost to resources", serviceFact.Current.NetCost, serviceFact.Baseline.NetCost, facts.Currency)},
		})
	}
	accounted := 0.0
	for _, driver := range drivers {
		accounted += driver.Delta
	}
	if residual := totalDelta - accounted; math.Abs(residual) > 1e-9 {
		drivers = append(drivers, models.CostDriver{
			Dimension: "project", Key: "other costs", Delta: residual, Category: "unattributed",
			Title: "Remaining project cost change", Confidence: "low",
			Evidence: []models.CostEvidence{costEvidence("billing_export", "reconciliation_residual", "Residual retained so ranked drivers reconcile to the project delta", residual, 0, facts.Currency)},
		})
	}
	sortDrivers(drivers, totalDelta)
	return rollupDrivers(drivers, maxResults, totalDelta, facts.Currency)
}

func rollupDrivers(drivers []models.CostDriver, limit int, totalDelta float64, currency string) []models.CostDriver {
	if len(drivers) > limit {
		keep := limit - 1
		if keep < 0 {
			keep = 0
		}
		rolled := append([]models.CostDriver(nil), drivers[:keep]...)
		other := models.CostDriver{
			Category: "unattributed", Title: "Other measured changes", Dimension: "project", Key: "other measured changes",
			Confidence: "low", Evidence: []models.CostEvidence{{
				Source: "billing_export", Signal: "ranked_driver_rollup",
				Summary: "Lower-ranked measured changes were combined to preserve reconciliation.",
				Unit:    currency, Attributes: map[string]any{"finding_count": len(drivers) - keep},
			}},
		}
		for _, driver := range drivers[keep:] {
			other.CurrentCost += driver.CurrentCost
			other.BaselineCost += driver.BaselineCost
			other.Delta += driver.Delta
			other.UsageEffect += driver.UsageEffect
			other.RateEffect += driver.RateEffect
		}
		drivers = append(rolled, other)
	} else {
		drivers = append([]models.CostDriver(nil), drivers...)
	}
	rankDrivers(drivers)
	return drivers
}

func rankDrivers(drivers []models.CostDriver) {
	positive := 0.0
	for _, driver := range drivers {
		if driver.Delta > 0 {
			positive += driver.Delta
		}
	}
	for i := range drivers {
		drivers[i].Rank = i + 1
		if drivers[i].Delta > 0 {
			drivers[i].ContributionPercent = safePercent(drivers[i].Delta, positive)
		}
	}
}

func classifyUsageDriver(driver *models.CostDriver, fact models.CostFact, anomaly models.CostTrafficAnomaly, currency string) {
	currentUsage, baselineUsage := fact.Current.Usage, fact.Baseline.Usage
	if anomaly.Resource != "" || anomaly.SKU != "" {
		driver.Category, driver.Title, driver.Confidence = "traffic_spike", "Traffic-like usage increased", anomaly.Confidence
	} else if baselineUsage <= 0 && currentUsage > 0 {
		driver.Category, driver.Title, driver.Confidence = "sku_mix_shift", "A new billed SKU appeared on the resource", "medium"
	} else if baselineUsage > 0 && currentUsage > 0 {
		baselineRate := fact.Baseline.NetCost / baselineUsage
		currentRate := fact.Current.NetCost / currentUsage
		driver.UsageEffect = (currentUsage - baselineUsage) * baselineRate
		driver.RateEffect = currentUsage * (currentRate - baselineRate)
		creditChange := fact.Current.Credits - fact.Baseline.Credits
		switch {
		case creditChange > 0 && math.Abs(creditChange) >= math.Abs(driver.Delta)*0.5:
			driver.Category, driver.Title = "commitment_or_credit_change", "Fewer credits or discounts increased net cost"
		case math.Abs(driver.UsageEffect) >= math.Abs(driver.RateEffect):
			driver.Category, driver.Title = "usage_growth", "Higher usage increased cost"
		default:
			driver.Category, driver.Title = "price_or_discount_change", "Effective unit cost increased"
		}
	}
	driver.Evidence = append(driver.Evidence, costEvidence("billing_export", "sku_usage", "Dominant SKU usage and cost comparison", currentUsage, baselineUsage, fact.UsageUnit))
	driver.Evidence = append(driver.Evidence, models.CostEvidence{Source: "billing_export", Signal: "dominant_sku", Summary: fact.SKU, Attributes: map[string]any{"currency": currency}})
}

func dominantChild(children []models.CostFact) (models.CostFact, bool) {
	if len(children) == 0 {
		return models.CostFact{}, false
	}
	sort.SliceStable(children, func(i, j int) bool {
		left := math.Abs(children[i].Current.NetCost - children[i].Baseline.NetCost)
		right := math.Abs(children[j].Current.NetCost - children[j].Baseline.NetCost)
		if left == right {
			return children[i].Key < children[j].Key
		}
		return left > right
	})
	return children[0], true
}

func sortDrivers(drivers []models.CostDriver, totalDelta float64) {
	sort.SliceStable(drivers, func(i, j int) bool {
		if drivers[i].Delta == drivers[j].Delta {
			return drivers[i].Key < drivers[j].Key
		}
		if totalDelta < 0 {
			return drivers[i].Delta < drivers[j].Delta
		}
		return drivers[i].Delta > drivers[j].Delta
	})
}

func costEvidence(source, signal, summary string, current, baseline float64, unit string) models.CostEvidence {
	return models.CostEvidence{Source: source, Signal: signal, Summary: summary, Current: floatPtr(current), Baseline: floatPtr(baseline), Unit: unit}
}

func floatPtr(value float64) *float64 { return &value }

func resourceMatch(left, right string) int {
	left = strings.TrimSpace(strings.ToLower(left))
	right = strings.TrimSpace(strings.ToLower(right))
	if left == "" || right == "" {
		return 0
	}
	if left == right {
		return 2
	}
	leftSuffix := resourceSuffix(left)
	rightSuffix := resourceSuffix(right)
	if leftSuffix != "" && leftSuffix == rightSuffix {
		return 1
	}
	return 0
}

func resourceSuffix(value string) string {
	parts := strings.Split(strings.Trim(value, "/"), "/")
	if len(parts) < 2 {
		return value
	}
	return strings.Join(parts[len(parts)-2:], "/")
}

func percentChange(current, baseline float64) float64 {
	if math.Abs(baseline) < 1e-9 {
		return 0
	}
	return (current - baseline) / math.Abs(baseline) * 100
}

func safePercent(value, total float64) float64 {
	if math.Abs(total) < 1e-9 {
		return 0
	}
	return value / math.Abs(total) * 100
}
