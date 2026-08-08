package securityaudit

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func applySuppressions(findings []models.SecurityFinding, suppressions []Suppression, now time.Time) ([]models.SecurityFinding, []models.SecuritySuppressedFinding) {
	active := make([]models.SecurityFinding, 0, len(findings))
	suppressed := make([]models.SecuritySuppressedFinding, 0)
	for _, finding := range findings {
		matched := false
		for _, suppression := range suppressions {
			expires, err := parseSuppressionExpiry(suppression.ExpiresAt)
			if err != nil || !now.Before(expires) || !strings.EqualFold(strings.TrimSpace(suppression.RuleID), finding.RuleID) {
				continue
			}
			if !wildcardMatch(strings.TrimSpace(suppression.Resource), finding.Resource) {
				continue
			}
			suppressed = append(suppressed, models.SecuritySuppressedFinding{
				FindingID: finding.ID, RuleID: finding.RuleID, Resource: finding.Resource,
				Reason: suppression.Reason, Owner: suppression.Owner, ExpiresAt: expires.UTC().Format(time.RFC3339),
			})
			matched = true
			break
		}
		if !matched {
			active = append(active, finding)
		}
	}
	return active, suppressed
}

func parseSuppressionExpiry(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("must be an RFC3339 timestamp or YYYY-MM-DD date")
}

func wildcardMatch(pattern, value string) bool {
	quoted := regexp.QuoteMeta(pattern)
	quoted = strings.ReplaceAll(quoted, `\*`, `.*`)
	quoted = strings.ReplaceAll(quoted, `\?`, `.`)
	matched, err := regexp.MatchString("^"+quoted+"$", value)
	return err == nil && matched
}
