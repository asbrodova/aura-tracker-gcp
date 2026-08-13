// Package securityaudit correlates read-only GCP configuration facts into a
// deterministic, evidence-backed project security posture report.
package securityaudit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

const (
	ruleVersion    = "security-posture-v2"
	defaultTimeout = 90 * time.Second
	cacheTTL       = 5 * time.Minute
)

// DataSource is the narrow secondary port required by the audit engine.
type DataSource interface {
	SearchSecurityIAMPolicies(context.Context, models.SecurityFactsRequest) (models.SecurityIAMPolicyFacts, error)
	ListServiceAccountSecurityFacts(context.Context, models.SecurityFactsRequest) (models.ServiceAccountSecurityFacts, error)
	ListSecretSecurityFacts(context.Context, models.SecurityFactsRequest) (models.SecretSecurityFacts, error)
	ListPublicServiceSecurityFacts(context.Context, models.SecurityFactsRequest) (models.PublicServiceSecurityFacts, error)
	ListFirewallSecurityFacts(context.Context, models.SecurityFactsRequest) (models.FirewallSecurityFacts, error)
	ListWorkloadIdentitySecurityFacts(context.Context, models.SecurityFactsRequest) (models.WorkloadIdentitySecurityFacts, error)
	ListSecurityRecommendations(context.Context, models.SecurityFactsRequest) (models.SecurityRecommendationFacts, error)
}

type Option func(*Engine)

// Config controls explicit, time-bounded accepted-risk suppressions.
type Config struct {
	Suppressions             []Suppression
	KubernetesAccess         string
	FleetProjectID           string
	ClusterConcurrency       int
	PerClusterTimeoutSeconds int
	MaxResourcesPerKind      int
}

type Suppression struct {
	RuleID    string
	Resource  string
	Reason    string
	Owner     string
	ExpiresAt string
}

// ValidateConfig rejects ambiguous or permanent suppressions before startup.
func ValidateConfig(cfg Config) error {
	switch cfg.KubernetesAccess {
	case "", "auto", "direct", "connect_gateway", "disabled":
	default:
		return fmt.Errorf("security_audit.kubernetes_access must be auto, direct, connect_gateway, or disabled")
	}
	if cfg.ClusterConcurrency < 0 || cfg.PerClusterTimeoutSeconds < 0 || cfg.MaxResourcesPerKind < 0 {
		return fmt.Errorf("security_audit Kubernetes limits must not be negative")
	}
	for i, suppression := range cfg.Suppressions {
		if strings.TrimSpace(suppression.RuleID) == "" {
			return fmt.Errorf("security_audit.suppressions[%d].rule_id is required", i)
		}
		if strings.TrimSpace(suppression.Resource) == "" {
			return fmt.Errorf("security_audit.suppressions[%d].resource is required", i)
		}
		if strings.TrimSpace(suppression.Reason) == "" {
			return fmt.Errorf("security_audit.suppressions[%d].reason is required", i)
		}
		if _, err := parseSuppressionExpiry(suppression.ExpiresAt); err != nil {
			return fmt.Errorf("security_audit.suppressions[%d].expires_at: %w", i, err)
		}
	}
	return nil
}

func WithConfig(cfg Config) Option {
	return func(e *Engine) { e.config = cfg }
}

func WithClock(now func() time.Time) Option {
	return func(e *Engine) {
		if now != nil {
			e.now = now
		}
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(e *Engine) {
		if timeout > 0 {
			e.timeout = timeout
		}
	}
}

type cachedReport struct {
	report    models.SecurityAuditReport
	expiresAt time.Time
}

type Engine struct {
	source  DataSource
	log     *slog.Logger
	now     func() time.Time
	timeout time.Duration
	config  Config
	mu      sync.Mutex
	cache   map[string]cachedReport
}

func New(source DataSource, log *slog.Logger, opts ...Option) *Engine {
	if log == nil {
		log = slog.Default()
	}
	e := &Engine{source: source, log: log, now: time.Now, timeout: defaultTimeout, cache: make(map[string]cachedReport)}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

type collectedFacts struct {
	iam               models.SecurityIAMPolicyFacts
	serviceAccounts   models.ServiceAccountSecurityFacts
	secrets           models.SecretSecurityFacts
	publicServices    models.PublicServiceSecurityFacts
	firewalls         models.FirewallSecurityFacts
	workloadIdentity  models.WorkloadIdentitySecurityFacts
	recommendations   models.SecurityRecommendationFacts
	errors            map[models.SecurityCategory]error
	recommendationErr error
}

func (e *Engine) Audit(ctx context.Context, req models.SecurityAuditRequest) (models.SecurityAuditReport, error) {
	if e.source == nil {
		return models.SecurityAuditReport{}, errors.New("securityaudit: data source is required")
	}
	if req.ProjectID == "" {
		return models.SecurityAuditReport{}, errors.New("securityaudit: project_id is required")
	}
	now := e.now().UTC()
	if !req.Refresh {
		e.mu.Lock()
		cached, ok := e.cache[req.ProjectID]
		e.mu.Unlock()
		if ok && now.Before(cached.expiresAt) {
			return cached.report, nil
		}
	}

	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	e.log.InfoContext(ctx, "security posture audit started", "project", req.ProjectID)
	facts := e.collect(ctx, models.SecurityFactsRequest{ProjectID: req.ProjectID})
	correlateWorkloadIdentity(&facts.workloadIdentity, facts.iam)
	findings := evaluateRules(facts, now, req.ProjectID)
	findings, suppressed := applySuppressions(findings, e.config.Suppressions, now)
	report := buildReport(req.ProjectID, now, findings, suppressed, facts)

	expiresAt := now.Add(cacheTTL)
	if quotaErr, ok := recommenderQuotaError(facts.recommendationErr); ok && !quotaErr.RetryAt.IsZero() && quotaErr.RetryAt.Before(expiresAt) {
		expiresAt = quotaErr.RetryAt
	}
	e.mu.Lock()
	if now.Before(expiresAt) {
		e.cache[req.ProjectID] = cachedReport{report: report, expiresAt: expiresAt}
	} else {
		delete(e.cache, req.ProjectID)
	}
	e.mu.Unlock()
	e.log.InfoContext(ctx, "security posture audit completed",
		"project", req.ProjectID, "audit_id", report.AuditID, "coverage", report.CoveragePercent,
		"critical", report.Counts.Critical, "high", report.Counts.High, "medium", report.Counts.Medium, "low", report.Counts.Low,
		"suppressed", len(report.Suppressed))
	return report, nil
}

// recommenderQuotaError identifies quota-specific degradation while leaving
// all other collector failures on their existing error path. A zero RetryAt is
// still a quota error, but cannot safely shorten the report cache.
func recommenderQuotaError(err error) (*ports.RecommenderQuotaExhaustedError, bool) {
	var quotaErr *ports.RecommenderQuotaExhaustedError
	if !errors.As(err, &quotaErr) || quotaErr == nil {
		return nil, false
	}
	return quotaErr, true
}

func (e *Engine) collect(ctx context.Context, req models.SecurityFactsRequest) collectedFacts {
	out := collectedFacts{errors: make(map[models.SecurityCategory]error)}
	var mu sync.Mutex
	var wg sync.WaitGroup
	run := func(category models.SecurityCategory, fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				mu.Lock()
				out.errors[category] = err
				mu.Unlock()
			}
		}()
	}
	run(models.SecurityCategoryIAM, func() error {
		value, err := e.source.SearchSecurityIAMPolicies(ctx, req)
		if err == nil {
			out.iam = value
		}
		return err
	})
	run(models.SecurityCategoryServiceAccounts, func() error {
		value, err := e.source.ListServiceAccountSecurityFacts(ctx, req)
		if err == nil {
			out.serviceAccounts = value
		}
		return err
	})
	run(models.SecurityCategorySecrets, func() error {
		value, err := e.source.ListSecretSecurityFacts(ctx, req)
		if err == nil {
			out.secrets = value
		}
		return err
	})
	run(models.SecurityCategoryPublicServices, func() error {
		value, err := e.source.ListPublicServiceSecurityFacts(ctx, req)
		if err == nil {
			out.publicServices = value
		}
		return err
	})
	run(models.SecurityCategoryFirewall, func() error {
		value, err := e.source.ListFirewallSecurityFacts(ctx, req)
		if err == nil {
			out.firewalls = value
		}
		return err
	})
	run(models.SecurityCategoryWorkloadIdentity, func() error {
		value, err := e.source.ListWorkloadIdentitySecurityFacts(ctx, req)
		if err == nil {
			out.workloadIdentity = value
		}
		return err
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		value, err := e.source.ListSecurityRecommendations(ctx, req)
		if err == nil {
			out.recommendations = value
		} else {
			out.recommendationErr = err
		}
	}()
	wg.Wait()
	return out
}

func auditID(project string, now time.Time) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s", project, now.Format(time.RFC3339), ruleVersion)))
	return "sec-" + hex.EncodeToString(sum[:6])
}
