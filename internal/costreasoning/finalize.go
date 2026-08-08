package costreasoning

import (
	"fmt"
	"math"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func finalizeResponse(response *models.ExplainCostResponse, facts models.BillingCostFacts, w analysisWindows, now time.Time, detailLevel string) {
	response.Coverage.DataThrough = facts.DataThrough
	response.Coverage.ExportStart = facts.ExportStart
	if dataThrough, err := time.Parse(time.RFC3339, facts.DataThrough); err == nil {
		response.Coverage.FreshnessHours = math.Max(0, now.Sub(dataThrough).Hours())
		if dataThrough.Before(w.currentEnd.Add(-6 * time.Hour)) {
			response.Warnings = append(response.Warnings, "Billing data did not reach the end of the requested current window; recent costs may be incomplete.")
		}
		if response.Coverage.FreshnessHours > 36 {
			response.Warnings = append(response.Warnings, fmt.Sprintf("Billing data is %.0f hours old; recent cost may be incomplete.", response.Coverage.FreshnessHours))
		}
	}
	if exportStart, err := time.Parse(time.RFC3339, facts.ExportStart); err == nil {
		location, locationErr := time.LoadLocation(response.Scope.Timezone)
		if locationErr == nil {
			localStart := exportStart.In(location)
			localEnd := w.currentEnd.In(location)
			startDay := time.Date(localStart.Year(), localStart.Month(), localStart.Day(), 0, 0, 0, 0, location)
			endDay := time.Date(localEnd.Year(), localEnd.Month(), localEnd.Day(), 0, 0, 0, 0, location)
			response.Coverage.HistoryDaysAvailable = calendarDays(startDay, endDay)
		} else {
			response.Coverage.HistoryDaysAvailable = int(w.currentEnd.Sub(exportStart).Hours() / 24)
		}
		if response.Coverage.HistoryDaysAvailable < 30 {
			response.Warnings = append(response.Warnings, "Less than 30 days of billing history was available; first-seen and anomaly confidence is reduced.")
			for i := range response.NewResources {
				if response.NewResources[i].Classification == "newly_billed" {
					response.NewResources[i].Confidence = "low"
				}
			}
		}
	}
	response.Coverage.ResourceAttributionPercent = math.Min(100, math.Max(0, safePercent(facts.ResourceAttributedNetCost, facts.TotalNetCostInHistory)))
	if response.Coverage.ResourceAttributionPercent < 80 && math.Abs(facts.TotalNetCostInHistory) > 0.01 {
		response.Warnings = append(response.Warnings, fmt.Sprintf("Only %.1f%% of queried net cost had resource identifiers.", response.Coverage.ResourceAttributionPercent))
	}

	partial := false
	for _, check := range response.Coverage.Checks {
		if check.Status == "partial" {
			partial = true
			break
		}
	}
	noData := len(facts.Facts) == 0 && facts.DataThrough == "" && len(facts.Daily) == 0 && len(facts.FirstSeen) == 0
	if noData {
		response.Warnings = append(response.Warnings, "No detailed billing export rows matched the project and requested history window.")
	}
	material := math.Max(0.01, math.Abs(response.Totals.BaselineNetCost)*0.01)
	switch {
	case noData:
		response.Status = "no_data"
	case partial:
		response.Status = "partial"
	case math.Abs(response.Totals.Delta) < material:
		response.Status = "no_material_change"
	default:
		response.Status = "complete"
	}
	response.Summary = buildSummary(response)
	applyDetailLevel(response, detailLevel)
}

func buildSummary(response *models.ExplainCostResponse) string {
	currency := response.Totals.Currency
	if currency == "" {
		currency = "billing currency"
	}
	direction := "increased"
	if response.Totals.Delta < 0 {
		direction = "decreased"
	}
	if response.Status == "no_material_change" {
		return fmt.Sprintf("Net usage cost was %.2f %s with no material change from the previous equal period.", response.Totals.CurrentNetCost, currency)
	}
	if response.Status == "no_data" {
		return "No detailed billing export rows matched this project and analysis window."
	}
	summary := ""
	if response.Totals.PercentChangeDefined {
		summary = fmt.Sprintf("Net usage cost %s by %.2f %s (%.1f%%), from %.2f to %.2f.", direction,
			math.Abs(response.Totals.Delta), currency, math.Abs(response.Totals.PercentChange), response.Totals.BaselineNetCost, response.Totals.CurrentNetCost)
	} else {
		summary = fmt.Sprintf("Net usage cost %s by %.2f %s, from a zero baseline to %.2f; percentage change is undefined.", direction,
			math.Abs(response.Totals.Delta), currency, response.Totals.CurrentNetCost)
	}
	if len(response.Drivers) > 0 {
		summary += " Leading measured driver: " + response.Drivers[0].Title + " (" + response.Drivers[0].Key + ")."
	}
	return summary
}

func applyDetailLevel(response *models.ExplainCostResponse, level string) {
	limit := 10
	switch level {
	case "summary":
		limit = 3
	case "detailed":
		limit = 25
	}
	response.Drivers = rollupDrivers(response.Drivers, limit, response.Totals.Delta, response.Totals.Currency)
	response.TopSpenders = capContributors(response.TopSpenders, limit)
	response.TopIncreases = capContributors(response.TopIncreases, limit)
	response.NewResources = capSlice(response.NewResources, limit)
	response.IdleResources = capSlice(response.IdleResources, limit)
	response.TrafficAnomalies = capSlice(response.TrafficAnomalies, limit)
	if level == "summary" && len(response.History) > 14 {
		response.History = response.History[len(response.History)-14:]
	}
}

func capContributors(values []models.CostContributor, limit int) []models.CostContributor {
	counts := make(map[string]int)
	result := make([]models.CostContributor, 0, len(values))
	for _, value := range values {
		if counts[value.Dimension] >= limit {
			continue
		}
		counts[value.Dimension]++
		result = append(result, value)
	}
	return result
}

func capSlice[T any](values []T, limit int) []T {
	if len(values) > limit {
		return values[:limit]
	}
	return values
}
