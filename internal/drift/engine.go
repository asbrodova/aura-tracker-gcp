// Package drift compares effective configuration snapshots for two GCP
// environments without treating either one as authoritative.
package drift

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
	engineVersion     = "environment-drift-v1"
	normalizerVersion = "configuration-normalizer-v1"
	defaultTimeout    = 90 * time.Second
	defaultMaxChanges = 250
	maxAllowedChanges = 1000
)

type Option func(*Engine)

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

type Engine struct {
	collector Collector
	log       *slog.Logger
	now       func() time.Time
	timeout   time.Duration
}

func New(collector Collector, log *slog.Logger, opts ...Option) *Engine {
	if log == nil {
		log = slog.Default()
	}
	e := &Engine{collector: collector, log: log, now: time.Now, timeout: defaultTimeout}
	for _, option := range opts {
		option(e)
	}
	return e
}

type collected struct {
	component string
	projectID string
	result    CollectionResult
	err       error
}

func (e *Engine) Compare(ctx context.Context, req models.CompareEnvironmentsRequest) (models.CompareEnvironmentsResponse, error) {
	if e.collector == nil {
		return models.CompareEnvironmentsResponse{}, errors.New("drift: collector is required")
	}
	components, err := e.normalizeRequest(&req)
	if err != nil {
		return models.CompareEnvironmentsResponse{}, err
	}
	now := e.now().UTC()
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	resp := models.CompareEnvironmentsResponse{
		ComparisonID: comparisonID(now, req), GeneratedAt: now.Format(time.RFC3339),
		EngineVersion: engineVersion, NormalizerVersion: normalizerVersion,
		EnvironmentA: req.EnvironmentA, EnvironmentB: req.EnvironmentB,
		Components: components, Result: "no_comparable_resources", CoverageStatus: "complete",
		Highlights: []models.ResourceDrift{}, Resources: []models.ResourceDrift{},
		Coverage: []models.DriftCoverage{}, Warnings: []string{},
	}

	results := make(chan collected, len(components)*2)
	sem := make(chan struct{}, 4)
	for _, component := range components {
		for _, projectID := range []string{req.EnvironmentA, req.EnvironmentB} {
			go func(component, projectID string) {
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					results <- collected{component: component, projectID: projectID, err: ctx.Err()}
					return
				}
				result, collectErr := e.collector.Collect(ctx, CollectionRequest{
					ProjectID: projectID, Component: component, ResourceNames: req.ResourceNames,
					Locations: req.Locations, Namespaces: req.Namespaces,
				})
				for i := range result.Resources {
					result.Resources[i].Config = normalizeConfig(result.Resources[i].Config, projectID)
				}
				results <- collected{component: component, projectID: projectID, result: result, err: collectErr}
			}(component, projectID)
		}
	}

	byComponent := make(map[string]map[string]collected, len(components))
	expectedResults := len(components) * 2
	receivedResults := 0
collectionLoop:
	for receivedResults < expectedResults {
		var result collected
		select {
		case result = <-results:
			receivedResults++
		case <-ctx.Done():
			break collectionLoop
		}
		if byComponent[result.component] == nil {
			byComponent[result.component] = make(map[string]collected)
		}
		byComponent[result.component][result.projectID] = result
	}
	if receivedResults < expectedResults {
		for _, component := range components {
			if byComponent[component] == nil {
				byComponent[component] = make(map[string]collected)
			}
			for _, projectID := range []string{req.EnvironmentA, req.EnvironmentB} {
				if _, ok := byComponent[component][projectID]; !ok {
					byComponent[component][projectID] = collected{component: component, projectID: projectID, err: ctx.Err()}
				}
			}
		}
	}

	for _, component := range components {
		a := byComponent[component][req.EnvironmentA]
		b := byComponent[component][req.EnvironmentB]
		e.appendCoverage(&resp, a)
		e.appendCoverage(&resp, b)
		if a.err != nil || b.err != nil {
			continue
		}
		complete := !a.result.Partial && !b.result.Partial
		componentResults := compareComponent(req.EnvironmentA, req.EnvironmentB, a.result.Resources, b.result.Resources, complete)
		resp.Resources = append(resp.Resources, componentResults...)
	}

	if ctx.Err() != nil {
		resp.CoverageStatus = "partial"
		resp.Warnings = append(resp.Warnings, "The comparison reached its time budget; results include only completed collectors.")
	}
	finalizeResponse(&resp, req)
	e.log.InfoContext(ctx, "environment drift comparison completed",
		"comparison_id", resp.ComparisonID, "environment_a", req.EnvironmentA,
		"environment_b", req.EnvironmentB, "components", len(components),
		"result", resp.Result, "coverage", resp.CoverageStatus,
		"differences", resp.Summary.DifferentResources+resp.Summary.OnlyInEnvironmentA+resp.Summary.OnlyInEnvironmentB,
	)
	return resp, nil
}

func (e *Engine) normalizeRequest(req *models.CompareEnvironmentsRequest) ([]string, error) {
	req.EnvironmentA = strings.TrimSpace(req.EnvironmentA)
	req.EnvironmentB = strings.TrimSpace(req.EnvironmentB)
	if req.EnvironmentA == "" || req.EnvironmentB == "" {
		return nil, errors.New("environment_a and environment_b are required")
	}
	if req.EnvironmentA == req.EnvironmentB {
		return nil, errors.New("environment_a and environment_b must resolve to different configured environments")
	}
	req.DetailLevel = strings.ToLower(strings.TrimSpace(req.DetailLevel))
	if req.DetailLevel == "" {
		req.DetailLevel = "standard"
	}
	if req.DetailLevel != "summary" && req.DetailLevel != "standard" && req.DetailLevel != "detailed" {
		return nil, errors.New("detail_level must be summary, standard, or detailed")
	}
	if req.MaxChanges == 0 {
		req.MaxChanges = defaultMaxChanges
	}
	if req.MaxChanges < 1 || req.MaxChanges > maxAllowedChanges {
		return nil, fmt.Errorf("max_changes must be between 1 and %d", maxAllowedChanges)
	}
	supported := make(map[string]bool)
	for _, component := range e.collector.SupportedComponents() {
		supported[component] = true
	}
	components := make([]string, 0, len(req.Components))
	seen := make(map[string]bool)
	if len(req.Components) == 0 {
		for component := range supported {
			components = append(components, component)
		}
	} else {
		for _, raw := range req.Components {
			component := strings.ToLower(strings.TrimSpace(raw))
			if !supported[component] {
				return nil, fmt.Errorf("unsupported component %q; supported components: %s", component, strings.Join(e.collector.SupportedComponents(), ", "))
			}
			if !seen[component] {
				seen[component], components = true, append(components, component)
			}
		}
	}
	sort.Strings(components)
	return components, nil
}

func (e *Engine) appendCoverage(resp *models.CompareEnvironmentsResponse, value collected) {
	coverage := models.DriftCoverage{Component: value.component, Environment: value.projectID, Status: "complete", Resources: len(value.result.Resources)}
	if value.err != nil {
		coverage.Status, coverage.Message = "error", value.err.Error()
		resp.CoverageStatus = "partial"
		resp.Warnings = append(resp.Warnings, fmt.Sprintf("%s collection failed in %s: %v", value.component, value.projectID, value.err))
	} else if value.result.Partial {
		coverage.Status = "partial"
		coverage.Message = strings.Join(value.result.Warnings, "; ")
		resp.CoverageStatus = "partial"
	}
	for _, warning := range value.result.Warnings {
		resp.Warnings = append(resp.Warnings, fmt.Sprintf("%s in %s: %s", value.component, value.projectID, warning))
	}
	resp.Coverage = append(resp.Coverage, coverage)
}

func finalizeResponse(resp *models.CompareEnvironmentsResponse, req models.CompareEnvironmentsRequest) {
	sortResourceDrifts(resp.Resources)
	for _, resource := range resp.Resources {
		switch resource.Status {
		case "equivalent":
			resp.Summary.EquivalentResources++
		case "different":
			resp.Summary.DifferentResources++
		case "missing_in_environment":
			if resource.MissingIn == resp.EnvironmentA {
				resp.Summary.OnlyInEnvironmentB++
			} else {
				resp.Summary.OnlyInEnvironmentA++
			}
		case "unknown_due_to_coverage":
			resp.Summary.UnknownDueToCoverage++
		}
		if resource.Status == "equivalent" || resource.Status == "different" {
			resp.Summary.ResourcesCompared++
		}
		resp.Summary.FieldDifferences += len(resource.FieldDifferences)
	}
	differenceCount := resp.Summary.DifferentResources + resp.Summary.OnlyInEnvironmentA + resp.Summary.OnlyInEnvironmentB
	resp.Summary.ResourcesOnlyIn = []models.DriftEnvironmentCount{
		{Environment: resp.EnvironmentA, Resources: resp.Summary.OnlyInEnvironmentA},
		{Environment: resp.EnvironmentB, Resources: resp.Summary.OnlyInEnvironmentB},
	}
	if resp.Summary.ResourcesCompared > 0 || differenceCount > 0 {
		if differenceCount > 0 {
			resp.Result = "differences_found"
		} else if resp.CoverageStatus == "complete" {
			resp.Result = "parity"
		} else {
			resp.Result = "no_differences_observed"
		}
	}
	resp.SummaryText = fmt.Sprintf(
		"Compared %s and %s: %d configured differently, %d only in %s, %d only in %s, and %d equivalent. Coverage: %s.",
		resp.EnvironmentA, resp.EnvironmentB, resp.Summary.DifferentResources,
		resp.Summary.OnlyInEnvironmentA, resp.EnvironmentA,
		resp.Summary.OnlyInEnvironmentB, resp.EnvironmentB,
		resp.Summary.EquivalentResources, resp.CoverageStatus,
	)
	for _, resource := range resp.Resources {
		if resource.Status != "equivalent" {
			resp.Highlights = append(resp.Highlights, trimResourceDetails(resource, detailLimit(req.DetailLevel)))
		}
		if len(resp.Highlights) >= 10 {
			break
		}
	}
	if !req.IncludeUnchanged {
		filtered := resp.Resources[:0]
		for _, resource := range resp.Resources {
			if resource.Status != "equivalent" {
				filtered = append(filtered, resource)
			}
		}
		resp.Resources = filtered
	}
	if req.DetailLevel == "summary" {
		resp.Resources = nil
	}
	if req.DetailLevel != "detailed" {
		for i := range resp.Resources {
			resp.Resources[i] = trimResourceDetails(resp.Resources[i], detailLimit(req.DetailLevel))
		}
	}
	if len(resp.Resources) > req.MaxChanges {
		resp.Resources, resp.Truncated = resp.Resources[:req.MaxChanges], true
	}
	sort.SliceStable(resp.Coverage, func(i, j int) bool {
		if resp.Coverage[i].Component != resp.Coverage[j].Component {
			return resp.Coverage[i].Component < resp.Coverage[j].Component
		}
		return resp.Coverage[i].Environment < resp.Coverage[j].Environment
	})
	sort.Strings(resp.Warnings)
}

func detailLimit(level string) int {
	if level == "detailed" {
		return -1
	}
	if level == "summary" {
		return 3
	}
	return 10
}

func trimResourceDetails(resource models.ResourceDrift, limit int) models.ResourceDrift {
	if limit >= 0 && len(resource.FieldDifferences) > limit {
		resource.FieldDifferences = resource.FieldDifferences[:limit]
	}
	return resource
}

func comparisonID(now time.Time, req models.CompareEnvironmentsRequest) string {
	encoded := fmt.Sprintf("%s|%s|%s|%s", now.Format(time.RFC3339Nano), req.EnvironmentA, req.EnvironmentB, strings.Join(req.Components, ","))
	digest := sha256.Sum256([]byte(encoded))
	return "drift-" + hex.EncodeToString(digest[:8])
}
