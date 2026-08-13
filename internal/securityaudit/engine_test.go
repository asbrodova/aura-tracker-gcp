package securityaudit

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/internal/testutil"
	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

func TestAuditBuildsSeverityRankedReportAndScore(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	projectResource := "//cloudresourcemanager.googleapis.com/projects/test-project"
	fake := &testutil.FakeGCPService{
		SearchSecurityIAMPoliciesFunc: func(context.Context, models.SecurityFactsRequest) (models.SecurityIAMPolicyFacts, error) {
			return models.SecurityIAMPolicyFacts{Policies: []models.SecurityIAMPolicyFact{{
				Resource: projectResource,
				Bindings: []models.SecurityIAMBindingFact{{Role: "roles/owner", Members: []string{"allUsers"}}},
			}}}, nil
		},
		ListServiceAccountSecurityFactsFunc: func(context.Context, models.SecurityFactsRequest) (models.ServiceAccountSecurityFacts, error) {
			return models.ServiceAccountSecurityFacts{ServiceAccounts: []models.ServiceAccountSecurityFact{{
				Name:  "projects/test-project/serviceAccounts/runtime@test-project.iam.gserviceaccount.com",
				Email: "runtime@test-project.iam.gserviceaccount.com",
				Keys:  []models.ServiceAccountKeyFact{{Name: "projects/test-project/serviceAccounts/runtime/keys/key-1", ID: "key-1", ValidAfterTime: now.AddDate(0, -6, 0).Format(time.RFC3339)}},
			}}}, nil
		},
		ListSecretSecurityFactsFunc: func(context.Context, models.SecurityFactsRequest) (models.SecretSecurityFacts, error) {
			return models.SecretSecurityFacts{Secrets: []models.SecretSecurityFact{{
				Name: "api-key", ResourceName: "//secretmanager.googleapis.com/projects/test-project/secrets/api-key",
				ReferencedBy: []string{"api"}, Versions: []models.SecretVersionSecurityFact{{Name: "1", State: "ENABLED", CreateTime: now.AddDate(0, -4, 0).Format(time.RFC3339)}},
			}}}, nil
		},
		ListPublicServiceSecurityFactsFunc: func(context.Context, models.SecurityFactsRequest) (models.PublicServiceSecurityFacts, error) {
			return models.PublicServiceSecurityFacts{Services: []models.PublicServiceSecurityFact{{
				ResourceName: "//run.googleapis.com/projects/test-project/locations/us-central1/services/api",
				Name:         "api", Kind: "cloud_run", Region: "us-central1", InvokerIAMDisabled: true,
			}}, Warnings: []string{"GKE exposure not assessed"}}, nil
		},
		ListFirewallSecurityFactsFunc: func(context.Context, models.SecurityFactsRequest) (models.FirewallSecurityFacts, error) {
			return models.FirewallSecurityFacts{Firewalls: []models.FirewallSecurityFact{{
				Name: "ssh", ResourceName: "//compute.googleapis.com/projects/test-project/global/firewalls/ssh",
				Direction: "INGRESS", SourceRanges: []string{"0.0.0.0/0"},
				Allowed: []models.FirewallProtocolFact{{Protocol: "tcp", Ports: []string{"22"}}},
			}}}, nil
		},
		ListWorkloadIdentitySecurityFactsFunc: func(context.Context, models.SecurityFactsRequest) (models.WorkloadIdentitySecurityFacts, error) {
			return models.WorkloadIdentitySecurityFacts{Clusters: []models.WorkloadIdentitySecurityFact{{
				ResourceName: "//container.googleapis.com/projects/test-project/locations/us-central1/clusters/legacy",
				ClusterName:  "legacy", Location: "us-central1", NodePools: []models.NodePoolIdentityFact{{Name: "default-pool", ServiceAccount: "default"}},
			}}, Warnings: []string{"KSA mappings not assessed"}}, nil
		},
		ListSecurityRecommendationsFunc: func(context.Context, models.SecurityFactsRequest) (models.SecurityRecommendationFacts, error) {
			return models.SecurityRecommendationFacts{Enabled: false, Recommendations: []models.SecurityRecommendationFact{}}, nil
		},
	}

	report, err := New(fake, slog.Default(), WithClock(func() time.Time { return now })).Audit(context.Background(), models.SecurityAuditRequest{ProjectID: "test-project"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Score == nil || *report.Score > 49 {
		t.Fatalf("critical finding must cap score at 49: %#v", report.Score)
	}
	if report.Counts.Critical == 0 || report.Counts.High == 0 || report.Counts.Medium == 0 || report.Counts.Low == 0 {
		t.Fatalf("expected all severity bands, got %+v", report.Counts)
	}
	if report.CoveragePercent != 93 {
		t.Fatalf("coverage = %d, want 93", report.CoveragePercent)
	}
	for _, heading := range []string{"## Critical", "## High", "## Medium", "## Low", "## Recommendations", "## Coverage gaps"} {
		if !strings.Contains(report.SummaryMarkdown, heading) {
			t.Errorf("summary missing %q", heading)
		}
	}
	if report.Findings[0].Severity != models.SecuritySeverityCritical {
		t.Fatalf("findings not severity sorted: %+v", report.Findings[0])
	}
}

func TestAuditWithMissingIAMIsUnscored(t *testing.T) {
	fake := &testutil.FakeGCPService{
		SearchSecurityIAMPoliciesFunc: func(context.Context, models.SecurityFactsRequest) (models.SecurityIAMPolicyFacts, error) {
			return models.SecurityIAMPolicyFacts{}, errors.New("permission denied: cloudasset.assets.searchAllIamPolicies")
		},
	}
	report, err := New(fake, slog.Default()).Audit(context.Background(), models.SecurityAuditRequest{ProjectID: "test-project"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Score != nil || report.ScoreStatus != "unavailable" {
		t.Fatalf("expected unavailable score, got score=%v status=%s", report.Score, report.ScoreStatus)
	}
	if !strings.Contains(report.SummaryMarkdown, "cloudasset.assets.searchAllIamPolicies") {
		t.Fatal("coverage error was not rendered")
	}
}

func TestAuditCacheAndRefresh(t *testing.T) {
	var calls atomic.Int32
	fake := &testutil.FakeGCPService{
		SearchSecurityIAMPoliciesFunc: func(context.Context, models.SecurityFactsRequest) (models.SecurityIAMPolicyFacts, error) {
			calls.Add(1)
			return models.SecurityIAMPolicyFacts{}, nil
		},
	}
	engine := New(fake, slog.Default())
	for _, refresh := range []bool{false, false, true} {
		if _, err := engine.Audit(context.Background(), models.SecurityAuditRequest{ProjectID: "test-project", Refresh: refresh}); err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("IAM collector calls = %d, want 2", got)
	}
}

func TestAuditQuotaDegradationIsPartialAndCachedOnlyUntilRetry(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	retryAt := now.Add(2 * time.Minute)
	var recommendationCalls atomic.Int32
	fake := &testutil.FakeGCPService{
		ListSecurityRecommendationsFunc: func(context.Context, models.SecurityFactsRequest) (models.SecurityRecommendationFacts, error) {
			recommendationCalls.Add(1)
			return models.SecurityRecommendationFacts{}, &ports.RecommenderQuotaExhaustedError{Op: "security recommendations", RetryAt: retryAt}
		},
	}
	engine := New(fake, slog.Default(), WithClock(func() time.Time { return now }))
	request := models.SecurityAuditRequest{ProjectID: "test-project"}

	report, err := engine.Audit(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	wantRetry := retryAt.Format(time.RFC3339)
	foundPartial := false
	for _, check := range report.Coverage {
		if check.Category == models.SecurityCategoryRecommendations && check.Status == "partial" && strings.Contains(check.Message, wantRetry) {
			foundPartial = true
		}
	}
	if !foundPartial {
		t.Fatalf("recommendation coverage does not describe quota retry: %+v", report.Coverage)
	}
	engine.mu.Lock()
	entry, ok := engine.cache[request.ProjectID]
	engine.mu.Unlock()
	if !ok || !entry.expiresAt.Equal(retryAt) {
		t.Fatalf("cache entry = %+v, present=%v; want expiry %s", entry, ok, retryAt)
	}

	now = retryAt.Add(-time.Second)
	if _, err := engine.Audit(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if calls := recommendationCalls.Load(); calls != 1 {
		t.Fatalf("recommendation calls before retry = %d, want 1", calls)
	}

	now = retryAt
	if _, err := engine.Audit(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if calls := recommendationCalls.Load(); calls != 2 {
		t.Fatalf("recommendation calls at retry = %d, want 2", calls)
	}
}

func TestFirewallPortHelpers(t *testing.T) {
	for _, test := range []struct {
		ranges []string
		port   int
		want   bool
	}{
		{nil, 22, true},
		{[]string{"20-23"}, 22, true},
		{[]string{"443"}, 22, false},
		{[]string{"bad"}, 22, false},
	} {
		if got := portsContain(test.ranges, test.port); got != test.want {
			t.Errorf("portsContain(%v, %d) = %v, want %v", test.ranges, test.port, got, test.want)
		}
	}
}

func TestInternalOnlyServerlessIngressIsNotReportedAsPublic(t *testing.T) {
	facts := models.PublicServiceSecurityFacts{Services: []models.PublicServiceSecurityFact{
		{ResourceName: "//run.googleapis.com/projects/p/locations/r/services/internal", Kind: "cloud_run", Ingress: "INGRESS_TRAFFIC_INTERNAL_ONLY", InvokerIAMDisabled: true},
		{ResourceName: "//cloudfunctions.googleapis.com/projects/p/locations/r/functions/internal", Kind: "cloud_function_gen1", Ingress: "ALLOW_INTERNAL_ONLY", External: true},
	}}
	if findings := evaluatePublicServices(facts, models.SecurityIAMPolicyFacts{}); len(findings) != 0 {
		t.Fatalf("internal-only services must not be reported as internet exposure: %+v", findings)
	}
}

func TestCategoryScoreCarriesCollectorCoverageStatus(t *testing.T) {
	checks := []models.SecurityCoverageCheck{
		{Category: models.SecurityCategoryIAM, Status: "complete", Weight: 25},
		{Category: models.SecurityCategoryServiceAccounts, Status: "error", Weight: 20},
		{Category: models.SecurityCategoryPublicServices, Status: "complete", Weight: 20},
		{Category: models.SecurityCategoryFirewall, Status: "complete", Weight: 15},
		{Category: models.SecurityCategorySecrets, Status: "complete", Weight: 10},
		{Category: models.SecurityCategoryWorkloadIdentity, Status: "complete", Weight: 10},
	}
	scores, score := calculateScore(nil, checks, 80, false)
	if score == nil {
		t.Fatal("sufficient non-IAM coverage should produce a provisional score")
	}
	for _, category := range scores {
		if category.Category == models.SecurityCategoryServiceAccounts && category.CoverageStatus != "error" {
			t.Fatalf("failed category status = %q, want error", category.CoverageStatus)
		}
	}
}

func TestTimeBoundedSuppressionRemainsVisibleAndExpires(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	finding := newFinding("PUB-001", models.SecuritySeverityMedium, models.SecurityCategoryPublicServices,
		"Public service", "//run.googleapis.com/projects/p/locations/r/services/api", "risk", "fix", docsCloudRun, "public")
	suppression := Suppression{
		RuleID: "PUB-001", Resource: "//run.googleapis.com/projects/*/services/api", Reason: "approved public API",
		Owner: "security@example.com", ExpiresAt: "2026-09-01T00:00:00Z",
	}
	active, suppressed := applySuppressions([]models.SecurityFinding{finding}, []Suppression{suppression}, now)
	if len(active) != 0 || len(suppressed) != 1 || suppressed[0].Reason != suppression.Reason {
		t.Fatalf("active=%+v suppressed=%+v", active, suppressed)
	}
	active, suppressed = applySuppressions([]models.SecurityFinding{finding}, []Suppression{suppression}, time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC))
	if len(active) != 1 || len(suppressed) != 0 {
		t.Fatalf("expired suppression still applied: active=%+v suppressed=%+v", active, suppressed)
	}
}

func TestValidateConfigRequiresAuditableSuppression(t *testing.T) {
	valid := Config{Suppressions: []Suppression{{
		RuleID: "FW-003", Resource: "*", Reason: "approved public web tier", ExpiresAt: "2026-12-01",
	}}}
	if err := ValidateConfig(valid); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	invalid := valid
	invalid.Suppressions[0].Reason = ""
	if err := ValidateConfig(invalid); err == nil {
		t.Fatal("suppression without a reason was accepted")
	}
}
