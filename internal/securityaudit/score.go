package securityaudit

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

var categoryWeights = map[models.SecurityCategory]int{
	models.SecurityCategoryIAM:              25,
	models.SecurityCategoryServiceAccounts:  20,
	models.SecurityCategoryPublicServices:   20,
	models.SecurityCategoryFirewall:         15,
	models.SecurityCategorySecrets:          10,
	models.SecurityCategoryWorkloadIdentity: 10,
}

func buildReport(projectID string, now time.Time, findings []models.SecurityFinding, suppressed []models.SecuritySuppressedFinding, facts collectedFacts) models.SecurityAuditReport {
	report := models.SecurityAuditReport{
		AuditID: auditID(projectID, now), ProjectID: projectID, GeneratedAt: now.Format(time.RFC3339), RuleVersion: ruleVersion,
		Findings: findings, Suppressed: suppressed, Coverage: buildCoverage(facts),
	}
	for _, finding := range findings {
		switch finding.Severity {
		case models.SecuritySeverityCritical:
			report.Counts.Critical++
		case models.SecuritySeverityHigh:
			report.Counts.High++
		case models.SecuritySeverityMedium:
			report.Counts.Medium++
		case models.SecuritySeverityLow:
			report.Counts.Low++
		}
	}
	report.CoveragePercent = coveragePercent(report.Coverage)
	report.CategoryScores, report.Score = calculateScore(findings, report.Coverage, report.CoveragePercent, facts.errors[models.SecurityCategoryIAM] != nil)
	switch {
	case report.Score == nil:
		report.ScoreStatus = "unavailable"
	case report.CoveragePercent >= 90:
		report.ScoreStatus = "final"
	default:
		report.ScoreStatus = "provisional"
	}
	report.Recommendations = buildRecommendations(findings)
	if len(report.Findings) > 200 {
		report.Findings = append([]models.SecurityFinding(nil), report.Findings[:200]...)
		report.Truncated = true
	}
	report.SummaryMarkdown = renderMarkdown(report)
	return report
}

func buildCoverage(facts collectedFacts) []models.SecurityCoverageCheck {
	items := map[models.SecurityCategory]int{
		models.SecurityCategoryIAM:              len(facts.iam.Policies),
		models.SecurityCategoryServiceAccounts:  len(facts.serviceAccounts.ServiceAccounts),
		models.SecurityCategorySecrets:          len(facts.secrets.Secrets),
		models.SecurityCategoryPublicServices:   len(facts.publicServices.Services),
		models.SecurityCategoryFirewall:         len(facts.firewalls.Firewalls),
		models.SecurityCategoryWorkloadIdentity: len(facts.workloadIdentity.Clusters),
	}
	warnings := map[models.SecurityCategory][]string{
		models.SecurityCategorySecrets:          facts.secrets.Warnings,
		models.SecurityCategoryPublicServices:   facts.publicServices.Warnings,
		models.SecurityCategoryWorkloadIdentity: facts.workloadIdentity.Warnings,
	}
	units := map[models.SecurityCategory][]models.SecurityCoverageUnit{
		models.SecurityCategoryIAM:              append([]models.SecurityCoverageUnit(nil), facts.iam.Coverage...),
		models.SecurityCategoryPublicServices:   append([]models.SecurityCoverageUnit(nil), facts.publicServices.Coverage...),
		models.SecurityCategoryFirewall:         append([]models.SecurityCoverageUnit(nil), facts.firewalls.Coverage...),
		models.SecurityCategoryWorkloadIdentity: append([]models.SecurityCoverageUnit(nil), facts.workloadIdentity.Coverage...),
	}
	if facts.iam.Truncated {
		units[models.SecurityCategoryIAM] = append(units[models.SecurityCategoryIAM], models.SecurityCoverageUnit{
			Collector: "cloud_asset_iam", ScopeType: "project", Scope: facts.iam.Hierarchy.ProjectID,
			Status: "truncated", ItemsScanned: len(facts.iam.Policies), Message: "Cloud Asset IAM policy search reached the 10,000-policy safety cap",
		})
	}
	categories := orderedCategories()
	out := make([]models.SecurityCoverageCheck, 0, len(categories)+1)
	for _, category := range categories {
		check := models.SecurityCoverageCheck{
			Category: category, Status: "complete", Weight: categoryWeights[category], ItemsScanned: items[category],
			Units: units[category], TotalUnits: len(units[category]),
		}
		for _, unit := range check.Units {
			if unit.Status == "complete" {
				check.CompletedUnits++
			}
		}
		if err := facts.errors[category]; err != nil {
			check.Status = "error"
			check.Message = err.Error()
		} else if check.TotalUnits > 0 && check.CompletedUnits < check.TotalUnits {
			check.Status = "partial"
			for _, unit := range check.Units {
				if unit.Status != "complete" {
					check.Message = firstCoverageMessage(unit)
					break
				}
			}
		} else if len(warnings[category]) > 0 {
			check.Status = "partial"
			check.Message = warnings[category][0]
		}
		out = append(out, check)
	}
	recommendations := models.SecurityCoverageCheck{
		Category: models.SecurityCategoryRecommendations, Weight: 0,
		Status: "complete", ItemsScanned: len(facts.recommendations.Recommendations),
	}
	if facts.recommendationErr != nil {
		recommendations.Status = "error"
		recommendations.Message = facts.recommendationErr.Error()
	} else if !facts.recommendations.Enabled {
		recommendations.Status = "skipped"
		recommendations.Message = "Cloud Recommender integration is disabled"
	}
	out = append(out, recommendations)
	return out
}

func coveragePercent(coverage []models.SecurityCoverageCheck) int {
	covered := 0.0
	total := 0
	for _, check := range coverage {
		total += check.Weight
		covered += float64(check.Weight) * coverageFactor(check)
	}
	if total == 0 {
		return 0
	}
	return int(math.Round(covered * 100 / float64(total)))
}

func calculateScore(findings []models.SecurityFinding, coverageChecks []models.SecurityCoverageCheck, coverage int, iamMissing bool) ([]models.SecurityCategoryScore, *int) {
	byCategory := make(map[models.SecurityCategory][]models.SecurityFinding)
	for _, finding := range findings {
		byCategory[finding.Category] = append(byCategory[finding.Category], finding)
	}
	scores := make([]models.SecurityCategoryScore, 0, len(categoryWeights))
	coverageByCategory := make(map[models.SecurityCategory]models.SecurityCoverageCheck, len(coverageChecks))
	for _, check := range coverageChecks {
		coverageByCategory[check.Category] = check
	}
	weighted := 0.0
	effectiveWeight := 0.0
	for _, category := range orderedCategories() {
		penalty := 0.0
		seenRule := make(map[string]int)
		for _, finding := range byCategory[category] {
			base := severityPenalty(finding.Severity)
			count := seenRule[finding.RuleID]
			multiplier := 1.0
			if count == 1 {
				multiplier = 0.5
			} else if count >= 2 {
				multiplier = 0.25
			}
			penalty += float64(base) * multiplier
			seenRule[finding.RuleID]++
		}
		score := 100 - int(math.Round(math.Min(100, penalty)))
		coverageCheck := coverageByCategory[category]
		status := coverageCheck.Status
		scores = append(scores, models.SecurityCategoryScore{
			Category: category, Weight: categoryWeights[category], Score: score,
			Findings: len(byCategory[category]), CoverageStatus: status,
		})
		factor := coverageFactor(coverageCheck)
		weight := float64(categoryWeights[category]) * factor
		weighted += float64(score) * weight
		effectiveWeight += weight
	}
	if coverage < 60 || iamMissing || effectiveWeight == 0 {
		return scores, nil
	}
	overall := int(math.Round(weighted / effectiveWeight))
	for _, finding := range findings {
		switch finding.Severity {
		case models.SecuritySeverityCritical:
			if overall > 49 {
				overall = 49
			}
		case models.SecuritySeverityHigh:
			if overall > 79 {
				overall = 79
			}
		case models.SecuritySeverityMedium:
			if overall > 89 {
				overall = 89
			}
		case models.SecuritySeverityLow:
			if overall > 99 {
				overall = 99
			}
		}
	}
	return scores, &overall
}

func coverageFactor(check models.SecurityCoverageCheck) float64 {
	switch check.Status {
	case "complete":
		return 1
	case "partial":
		if check.TotalUnits > 0 {
			return float64(check.CompletedUnits) / float64(check.TotalUnits)
		}
		return 0.75
	default:
		return 0
	}
}

func firstCoverageMessage(unit models.SecurityCoverageUnit) string {
	if unit.Message != "" {
		return unit.Collector + " for " + unit.Scope + ": " + unit.Message
	}
	return unit.Collector + " for " + unit.Scope + " is " + unit.Status
}

func severityPenalty(severity models.SecuritySeverity) int {
	switch severity {
	case models.SecuritySeverityCritical:
		return 40
	case models.SecuritySeverityHigh:
		return 20
	case models.SecuritySeverityMedium:
		return 8
	default:
		return 3
	}
}

func buildRecommendations(findings []models.SecurityFinding) []models.SecurityRecommendation {
	type group struct {
		severity models.SecuritySeverity
		title    string
		action   string
		ids      []string
	}
	groups := make(map[string]*group)
	for _, finding := range findings {
		value := groups[finding.RuleID]
		if value == nil {
			value = &group{severity: finding.Severity, title: finding.Title, action: finding.Recommendation}
			groups[finding.RuleID] = value
		}
		if severityRank(finding.Severity) < severityRank(value.severity) {
			value.severity = finding.Severity
		}
		value.ids = append(value.ids, finding.ID)
	}
	out := make([]models.SecurityRecommendation, 0, len(groups))
	for _, value := range groups {
		sort.Strings(value.ids)
		out = append(out, models.SecurityRecommendation{
			Severity: value.severity, Title: value.title, Action: value.action,
			AffectedCount: len(value.ids), FindingIDs: value.ids,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := severityRank(out[i].Severity), severityRank(out[j].Severity)
		if left != right {
			return left < right
		}
		if out[i].AffectedCount != out[j].AffectedCount {
			return out[i].AffectedCount > out[j].AffectedCount
		}
		return out[i].Title < out[j].Title
	})
	if len(out) > 10 {
		out = out[:10]
	}
	for i := range out {
		out[i].Priority = i + 1
	}
	return out
}

func orderedCategories() []models.SecurityCategory {
	return []models.SecurityCategory{
		models.SecurityCategoryIAM,
		models.SecurityCategoryServiceAccounts,
		models.SecurityCategoryPublicServices,
		models.SecurityCategoryFirewall,
		models.SecurityCategorySecrets,
		models.SecurityCategoryWorkloadIdentity,
	}
}

func scoreString(score *int) string {
	if score == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%d/100", *score)
}
