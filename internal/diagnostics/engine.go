// Package diagnostics correlates read-only operational signals into an
// evidence-backed incident diagnosis. It is an application-layer package: it
// depends on domain models, not the GCP SDK or MCP transport.
package diagnostics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

const (
	scoringVersion = "incident-heuristics-v1"
	defaultTimeout = 45 * time.Second
)

// DataSource is the narrow secondary port used by the diagnosis engine.
// ports.GCPService satisfies it, while tests can provide small deterministic
// fakes without importing the GCP adapter.
type DataSource interface {
	ListServices(context.Context, models.ListServicesRequest) (models.ListServicesResponse, error)
	GetServiceDetails(context.Context, models.GetServiceDetailsRequest) (models.ServiceDetails, error)
	ListRevisions(context.Context, models.ListRevisionsRequest) (models.ListRevisionsResponse, error)
	QueryRecentLogs(context.Context, models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error)
	GetMetrics(context.Context, models.GetMetricsRequest) (models.GetMetricsResponse, error)
	GetServiceTopology(context.Context, models.GetServiceTopologyRequest) (models.ServiceTopologyReport, error)
	ListSQLInstances(context.Context, models.ListSQLInstancesRequest) (models.ListSQLInstancesResponse, error)
	ListVPCConnectors(context.Context, models.ListVPCConnectorsRequest) (models.ListVPCConnectorsResponse, error)
	InspectTopicHealth(context.Context, models.InspectTopicHealthRequest) (models.TopicHealthReport, error)
}

// Option configures an Engine.
type Option func(*Engine)

// WithClock replaces wall-clock time, primarily for deterministic tests.
func WithClock(now func() time.Time) Option {
	return func(e *Engine) {
		if now != nil {
			e.now = now
		}
	}
}

// WithTimeout bounds the complete diagnosis. Individual signal failures are
// reported through coverage and do not discard evidence from other signals.
func WithTimeout(timeout time.Duration) Option {
	return func(e *Engine) {
		if timeout > 0 {
			e.timeout = timeout
		}
	}
}

type Engine struct {
	source  DataSource
	log     *slog.Logger
	now     func() time.Time
	timeout time.Duration
}

func New(source DataSource, log *slog.Logger, opts ...Option) *Engine {
	if log == nil {
		log = slog.Default()
	}
	e := &Engine{source: source, log: log, now: time.Now, timeout: defaultTimeout}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func (e *Engine) Diagnose(ctx context.Context, req models.DiagnoseIncidentRequest) (models.DiagnoseIncidentResponse, error) {
	if e.source == nil {
		return models.DiagnoseIncidentResponse{}, errors.New("diagnostics: data source is required")
	}
	if err := normalizeRequest(&req); err != nil {
		return models.DiagnoseIncidentResponse{}, err
	}

	now := e.now().UTC()
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	scope, warning, err := e.resolveScope(ctx, req)
	if err != nil {
		return models.DiagnoseIncidentResponse{}, fmt.Errorf("resolve incident scope: %w", err)
	}
	resp := models.DiagnoseIncidentResponse{
		DiagnosisID:        diagnosisID(now, req),
		GeneratedAt:        now.Format(time.RFC3339),
		ScoringVersion:     scoringVersion,
		Scope:              scope,
		Symptoms:           []models.IncidentServiceSymptoms{},
		Dependencies:       []models.IncidentDependencyObservation{},
		PossibleRootCauses: []models.IncidentRootCause{},
		Timeline:           []models.IncidentTimelineEvent{},
		Coverage:           models.IncidentCoverage{Checks: []models.IncidentCoverageCheck{}},
		Warnings:           []string{},
	}
	if warning != "" {
		resp.Warnings = append(resp.Warnings, warning)
	}
	if len(scope.Targets) == 0 {
		resp.Status = "needs_scope"
		resp.Summary = "No production target could be inferred safely; select a candidate service or add an environment label."
		return resp, nil
	}
	e.log.InfoContext(ctx, "incident diagnosis started", "project", req.ProjectID, "targets", len(scope.Targets), "lookback_minutes", req.LookbackMinutes, "baseline_minutes", req.BaselineMinutes)

	data := make([]serviceData, len(scope.Targets))
	results := make(chan indexedServiceData, len(scope.Targets))
	sem := make(chan struct{}, 4)
	for i, target := range scope.Targets {
		go func(index int, target models.IncidentTarget) {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results <- indexedServiceData{index: index, data: serviceData{target: target, warnings: []string{ctx.Err().Error()}}}
				return
			}
			results <- indexedServiceData{index: index, data: e.collectService(ctx, req, target, now)}
		}(i, target)
	}
	for range scope.Targets {
		result := <-results
		data[result.index] = result.data
	}

	for i := range data {
		d := &data[i]
		resp.Coverage.Checks = append(resp.Coverage.Checks, d.coverage...)
		resp.Warnings = append(resp.Warnings, d.warnings...)
		resp.Dependencies = append(resp.Dependencies, d.dependencies...)
		symptom := analyzeSymptoms(d, now, req)
		d.symptoms = symptom
		resp.Symptoms = append(resp.Symptoms, symptom)
		resp.Timeline = append(resp.Timeline, buildTimeline(d, now, req)...)
	}

	resp.PossibleRootCauses = rankRootCauses(data, req, now)
	for i := range resp.PossibleRootCauses {
		resp.PossibleRootCauses[i].Rank = i + 1
	}
	sort.SliceStable(resp.Timeline, func(i, j int) bool {
		return resp.Timeline[i].Timestamp < resp.Timeline[j].Timestamp
	})
	if len(resp.Timeline) > 50 {
		resp.Timeline = resp.Timeline[len(resp.Timeline)-50:]
	}
	finalizeCoverage(&resp.Coverage)
	resp.Status, resp.Summary = summarize(data, resp.PossibleRootCauses)
	if ctx.Err() != nil {
		resp.Warnings = appendUnique(resp.Warnings, "Diagnosis reached its time budget; findings are based on completed collectors.")
	}
	sort.Strings(resp.Warnings)
	applyDetailLevel(&resp, req.DetailLevel)
	e.log.InfoContext(ctx, "incident diagnosis completed", "diagnosis_id", resp.DiagnosisID, "status", resp.Status, "causes", len(resp.PossibleRootCauses), "coverage_partial", resp.Coverage.Partial)
	return resp, nil
}

func normalizeRequest(req *models.DiagnoseIncidentRequest) error {
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.Environment = strings.TrimSpace(req.Environment)
	req.ServiceName = strings.TrimSpace(req.ServiceName)
	req.Region = strings.TrimSpace(req.Region)
	req.DetailLevel = strings.ToLower(strings.TrimSpace(req.DetailLevel))
	if req.ProjectID == "" {
		return errors.New("project_id is required")
	}
	if req.Environment == "" {
		req.Environment = "production"
	}
	if req.LookbackMinutes == 0 {
		req.LookbackMinutes = 60
	}
	if req.BaselineMinutes == 0 {
		req.BaselineMinutes = 240
	}
	if req.MaxServices == 0 {
		req.MaxServices = 10
	}
	if req.MaxDependencies == 0 {
		req.MaxDependencies = 10
	}
	if req.DetailLevel == "" {
		req.DetailLevel = "standard"
	}
	if req.LookbackMinutes < 5 || req.LookbackMinutes > 720 {
		return errors.New("lookback_minutes must be between 5 and 720")
	}
	if req.BaselineMinutes < 5 || req.BaselineMinutes > 1440 {
		return errors.New("baseline_minutes must be between 5 and 1440")
	}
	if req.LookbackMinutes+req.BaselineMinutes > 1440 {
		return errors.New("lookback_minutes plus baseline_minutes must not exceed 1440")
	}
	if req.MaxServices < 1 || req.MaxServices > 25 {
		return errors.New("max_services must be between 1 and 25")
	}
	if req.MaxDependencies < 1 || req.MaxDependencies > 25 {
		return errors.New("max_dependencies must be between 1 and 25")
	}
	switch req.DetailLevel {
	case "summary", "standard", "detailed":
	default:
		return errors.New("detail_level must be summary, standard, or detailed")
	}
	return nil
}

func (e *Engine) resolveScope(ctx context.Context, req models.DiagnoseIncidentRequest) (models.IncidentScope, string, error) {
	scope := models.IncidentScope{
		ProjectID: req.ProjectID, Environment: req.Environment,
		Targets: []models.IncidentTarget{}, Candidates: []models.IncidentTarget{},
		Confidence: "high",
	}
	if req.ServiceName != "" && req.Region != "" {
		scope.Targets = append(scope.Targets, models.IncidentTarget{ServiceName: req.ServiceName, Region: req.Region})
		return scope, "", nil
	}

	services, err := e.source.ListServices(ctx, models.ListServicesRequest{ProjectID: req.ProjectID, Region: req.Region})
	if err != nil {
		return scope, "", err
	}
	sort.SliceStable(services.Services, func(i, j int) bool {
		if services.Services[i].Name == services.Services[j].Name {
			return services.Services[i].Region < services.Services[j].Region
		}
		return services.Services[i].Name < services.Services[j].Name
	})
	for _, service := range services.Services {
		target := models.IncidentTarget{ServiceName: service.Name, Region: service.Region, Labels: service.Labels}
		if req.ServiceName != "" {
			if service.Name == req.ServiceName {
				scope.Targets = append(scope.Targets, target)
			}
			continue
		}
		scope.Candidates = append(scope.Candidates, target)
		if environmentMatches(service.Labels, req.Environment) {
			scope.Targets = append(scope.Targets, target)
		}
	}
	if len(scope.Targets) > req.MaxServices {
		scope.Targets = scope.Targets[:req.MaxServices]
	}
	if len(scope.Candidates) > req.MaxServices {
		scope.Candidates = scope.Candidates[:req.MaxServices]
	}
	if req.ServiceName != "" {
		if len(scope.Targets) == 0 {
			return scope, "", fmt.Errorf("cloud run service %q was not found", req.ServiceName)
		}
		scope.Inferred = req.Region == ""
		if len(scope.Targets) > 1 {
			scope.Confidence = "medium"
			return scope, "Service name exists in multiple regions; all matches within the service limit were analyzed.", nil
		}
		return scope, "", nil
	}

	scope.Inferred = true
	if len(scope.Targets) == 0 {
		scope.Confidence = "low"
		return scope, "Production scope was not inferred because no service had a matching env/environment/stage label.", nil
	}
	scope.Candidates = nil
	if len(scope.Targets) == req.MaxServices {
		scope.Confidence = "medium"
		return scope, "Production scope reached max_services; results may omit additional matching services.", nil
	}
	return scope, "", nil
}

func applyDetailLevel(resp *models.DiagnoseIncidentResponse, level string) {
	maxCauses, maxEvidence, maxContradictions, maxInvestigations, maxTimeline, maxPatterns := 5, 5, 3, 3, 30, 3
	switch level {
	case "summary":
		maxCauses, maxEvidence, maxContradictions, maxInvestigations, maxTimeline, maxPatterns = 3, 3, 2, 2, 15, 1
	case "detailed":
		maxCauses, maxEvidence, maxContradictions, maxInvestigations, maxTimeline, maxPatterns = 10, 10, 10, 10, 50, 5
	}
	if len(resp.PossibleRootCauses) > maxCauses {
		resp.PossibleRootCauses = resp.PossibleRootCauses[:maxCauses]
	}
	for i := range resp.PossibleRootCauses {
		cause := &resp.PossibleRootCauses[i]
		if len(cause.Evidence) > maxEvidence {
			cause.Evidence = cause.Evidence[:maxEvidence]
		}
		if len(cause.ContradictingEvidence) > maxContradictions {
			cause.ContradictingEvidence = cause.ContradictingEvidence[:maxContradictions]
		}
		if len(cause.SuggestedInvestigation) > maxInvestigations {
			cause.SuggestedInvestigation = cause.SuggestedInvestigation[:maxInvestigations]
		}
	}
	if len(resp.Timeline) > maxTimeline {
		resp.Timeline = resp.Timeline[len(resp.Timeline)-maxTimeline:]
	}
	for i := range resp.Symptoms {
		if len(resp.Symptoms[i].ErrorPatterns) > maxPatterns {
			resp.Symptoms[i].ErrorPatterns = resp.Symptoms[i].ErrorPatterns[:maxPatterns]
		}
	}
}

func environmentMatches(labels map[string]string, wanted string) bool {
	wanted = strings.ToLower(strings.TrimSpace(wanted))
	for _, key := range []string{"env", "environment", "stage"} {
		value := strings.ToLower(strings.TrimSpace(labels[key]))
		if value == wanted || (wanted == "production" && value == "prod") || (wanted == "prod" && value == "production") {
			return true
		}
	}
	return false
}

func diagnosisID(now time.Time, req models.DiagnoseIncidentRequest) string {
	input := fmt.Sprintf("%s|%s|%s|%s|%s", now.Format(time.RFC3339Nano), req.ProjectID, req.ServiceName, req.Region, req.Environment)
	sum := sha256.Sum256([]byte(input))
	return "diag-" + hex.EncodeToString(sum[:6])
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
