package securityaudit

import (
	"fmt"
	"strings"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func renderMarkdown(report models.SecurityAuditReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Security posture: %s\n\n", scoreString(report.Score))
	fmt.Fprintf(&b, "Coverage: %d%% — %s score  \n", report.CoveragePercent, report.ScoreStatus)
	fmt.Fprintf(&b, "Critical: %d · High: %d · Medium: %d · Low: %d\n", report.Counts.Critical, report.Counts.High, report.Counts.Medium, report.Counts.Low)

	sections := []struct {
		severity models.SecuritySeverity
		title    string
	}{
		{models.SecuritySeverityCritical, "Critical"},
		{models.SecuritySeverityHigh, "High"},
		{models.SecuritySeverityMedium, "Medium"},
		{models.SecuritySeverityLow, "Low"},
	}
	for _, section := range sections {
		fmt.Fprintf(&b, "\n## %s\n\n", section.title)
		count := 0
		for _, finding := range report.Findings {
			if finding.Severity != section.severity {
				continue
			}
			count++
			fmt.Fprintf(&b, "### [%s] %s\n\n", clean(finding.ID), clean(finding.Title))
			fmt.Fprintf(&b, "Resource: `%s`  \n", clean(finding.Resource))
			if len(finding.Evidence) > 0 {
				fmt.Fprintf(&b, "Evidence: %s  \n", clean(strings.Join(finding.Evidence, "; ")))
			}
			fmt.Fprintf(&b, "Risk: %s  \n", clean(finding.Risk))
			fmt.Fprintf(&b, "Recommendation: %s\n\n", clean(finding.Recommendation))
		}
		if count == 0 {
			b.WriteString("No findings observed at this severity.\n")
		}
	}

	b.WriteString("\n## Recommendations\n\n")
	if len(report.Recommendations) == 0 {
		b.WriteString("No remediation recommendations were generated from the completed checks.\n")
	} else {
		for _, recommendation := range report.Recommendations {
			fmt.Fprintf(&b, "%d. **%s** — %s (%d affected)\n", recommendation.Priority,
				clean(recommendation.Title), clean(recommendation.Action), recommendation.AffectedCount)
		}
	}

	b.WriteString("\n## Suppressed accepted risks\n\n")
	if len(report.Suppressed) == 0 {
		b.WriteString("No active suppressions matched this report.\n")
	} else {
		for _, suppression := range report.Suppressed {
			fmt.Fprintf(&b, "- **%s** on `%s` — %s (expires %s", clean(suppression.RuleID), clean(suppression.Resource), clean(suppression.Reason), clean(suppression.ExpiresAt))
			if suppression.Owner != "" {
				fmt.Fprintf(&b, "; owner %s", clean(suppression.Owner))
			}
			b.WriteString(")\n")
		}
	}

	b.WriteString("\n## Coverage gaps\n\n")
	gaps := 0
	for _, coverage := range report.Coverage {
		if coverage.Status == "complete" {
			continue
		}
		gaps++
		fmt.Fprintf(&b, "- **%s**: %s", clean(string(coverage.Category)), clean(coverage.Status))
		if coverage.TotalUnits > 0 {
			fmt.Fprintf(&b, " (%d/%d scope units complete)", coverage.CompletedUnits, coverage.TotalUnits)
		}
		if coverage.Message != "" {
			fmt.Fprintf(&b, " — %s", clean(coverage.Message))
		}
		b.WriteByte('\n')
		shown := 0
		for _, unit := range coverage.Units {
			if unit.Status == "complete" || shown >= 10 {
				continue
			}
			fmt.Fprintf(&b, "  - `%s` `%s`: %s", clean(unit.Collector), clean(unit.Scope), clean(unit.Status))
			if unit.Message != "" {
				fmt.Fprintf(&b, " — %s", clean(unit.Message))
			}
			b.WriteByte('\n')
			shown++
		}
		if shown == 10 {
			b.WriteString("  - Additional incomplete scope units are available in the structured report.\n")
		}
	}
	if gaps == 0 {
		b.WriteString("- None. All configured collectors and scope units completed.\n")
	}
	if report.Truncated {
		b.WriteString("\nThe detailed finding list was truncated; severity counts include all findings.\n")
	}
	return b.String()
}

func clean(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "`", "'")
	for strings.Contains(value, "  ") {
		value = strings.ReplaceAll(value, "  ", " ")
	}
	return strings.TrimSpace(value)
}
