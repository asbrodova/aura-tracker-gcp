// Package costreasoning converts normalized Cloud Billing facts and optional
// operational signals into deterministic, evidence-backed cost explanations.
package costreasoning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

const (
	reasoningVersion = "cost-reasoning-v1"
	defaultTimeout   = 45 * time.Second
	cacheTTL         = 15 * time.Minute
	maxCacheEntries  = 256
)

type DataSource interface {
	CollectCostFacts(context.Context, models.CollectCostFactsRequest) (models.BillingCostFacts, error)
	ListCostRecommendations(context.Context, models.ListCostRecommendationsRequest) (models.ListCostRecommendationsResponse, error)
	ListCreatedAssets(context.Context, models.ListCreatedAssetsRequest) (models.ListCreatedAssetsResponse, error)
	GetMetrics(context.Context, models.GetMetricsRequest) (models.GetMetricsResponse, error)
}

type Config struct {
	Timezone    string
	HistoryDays int
	Timeout     time.Duration
}

type cacheEntry struct {
	response  models.ExplainCostResponse
	expiresAt time.Time
}

type Engine struct {
	source      DataSource
	log         *slog.Logger
	now         func() time.Time
	timezone    string
	historyDays int
	timeout     time.Duration
	cacheMu     sync.RWMutex
	cache       map[string]cacheEntry
	flight      singleflight.Group
}

type Option func(*Engine)

func WithClock(now func() time.Time) Option {
	return func(e *Engine) {
		if now != nil {
			e.now = now
		}
	}
}

func New(source DataSource, log *slog.Logger, cfg Config, opts ...Option) *Engine {
	if log == nil {
		log = slog.Default()
	}
	if strings.TrimSpace(cfg.Timezone) == "" {
		cfg.Timezone = "UTC"
	}
	if cfg.HistoryDays <= 0 {
		cfg.HistoryDays = 90
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	e := &Engine{
		source: source, log: log, now: time.Now, timezone: cfg.Timezone,
		historyDays: cfg.HistoryDays, timeout: cfg.Timeout, cache: make(map[string]cacheEntry),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func (e *Engine) Explain(ctx context.Context, req models.ExplainCostRequest) (models.ExplainCostResponse, error) {
	if e.source == nil {
		return models.ExplainCostResponse{}, errors.New("cost reasoning: data source is required")
	}
	if err := normalizeRequest(&req, e.timezone); err != nil {
		return models.ExplainCostResponse{}, err
	}
	now := e.now().UTC()
	windows, err := resolveWindows(req, now, e.historyDays)
	if err != nil {
		return models.ExplainCostResponse{}, err
	}
	cacheKey := responseCacheKey(req, windows)
	if cached, ok := e.getCached(cacheKey, now); ok {
		cached.Coverage.CacheHit = true
		return cached, nil
	}
	value, err, _ := e.flight.Do(cacheKey, func() (any, error) {
		if cached, ok := e.getCached(cacheKey, now); ok {
			cached.Coverage.CacheHit = true
			return cached, nil
		}
		return e.explainUncached(ctx, req, windows, now, cacheKey)
	})
	if err != nil {
		return models.ExplainCostResponse{}, err
	}
	return value.(models.ExplainCostResponse), nil
}

func (e *Engine) explainUncached(ctx context.Context, req models.ExplainCostRequest, windows analysisWindows, now time.Time, cacheKey string) (models.ExplainCostResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	facts, err := e.source.CollectCostFacts(ctx, models.CollectCostFactsRequest{
		ProjectID: req.ProjectID, CurrentStart: windows.currentStart.Format(time.RFC3339),
		CurrentEnd: windows.currentEnd.Format(time.RFC3339), BaselineStart: windows.baselineStart.Format(time.RFC3339),
		BaselineEnd: windows.baselineEnd.Format(time.RFC3339), HistoryStart: windows.historyStart.Format(time.RFC3339),
		Timezone: req.Timezone, MaxResults: req.MaxResults,
	})
	if err != nil {
		return models.ExplainCostResponse{}, fmt.Errorf("collect billing cost facts: %w", err)
	}
	if facts.CurrencyCount > 1 {
		facts.Currency = "MULTIPLE"
	}

	response := newResponse(req, windows, facts, now)
	type assetResult struct {
		response models.ListCreatedAssetsResponse
		err      error
	}
	type recommendationResult struct {
		response models.ListCostRecommendationsResponse
		err      error
	}
	assetResults := make(chan assetResult, 1)
	needsAssetEvidence := len(facts.FirstSeen) > 0
	if needsAssetEvidence {
		go func() {
			assets, assetErr := e.source.ListCreatedAssets(ctx, models.ListCreatedAssetsRequest{
				ProjectID: req.ProjectID, StartTime: windows.currentStart.Format(time.RFC3339),
				EndTime: windows.currentEnd.Format(time.RFC3339), Limit: req.MaxResults * 10,
			})
			assetResults <- assetResult{response: assets, err: assetErr}
		}()
	}
	includeIdle := boolValue(req.IncludeIdle, true)
	recommendationResults := make(chan recommendationResult, 1)
	if includeIdle {
		go func() {
			recommendations, recommendationErr := e.source.ListCostRecommendations(ctx, models.ListCostRecommendationsRequest{ProjectID: req.ProjectID, Limit: req.MaxResults * 10})
			recommendationResults <- recommendationResult{response: recommendations, err: recommendationErr}
		}()
	}

	assetCollection := assetResult{}
	if needsAssetEvidence {
		select {
		case assetCollection = <-assetResults:
		case <-ctx.Done():
			assetCollection.err = ctx.Err()
		}
	}
	assets, assetErr := assetCollection.response, assetCollection.err
	if !needsAssetEvidence {
		response.Coverage.Checks = append(response.Coverage.Checks, models.CostCoverageCheck{Name: "asset_inventory", Status: "skipped", Message: "No newly billed candidates required creation-time confirmation."})
	} else if assetErr != nil {
		response.Coverage.Checks = append(response.Coverage.Checks, models.CostCoverageCheck{Name: "asset_inventory", Status: "partial", Message: assetErr.Error()})
		response.Warnings = append(response.Warnings, "Cloud Asset Inventory was unavailable; newly billed resources could not be confirmed as newly created.")
	} else if assets.Truncated {
		response.Coverage.Checks = append(response.Coverage.Checks, models.CostCoverageCheck{Name: "asset_inventory", Status: "partial", Message: "created-resource result limit reached"})
		response.Warnings = append(response.Warnings, "Cloud Asset Inventory results were truncated; some new resources might be unconfirmed.")
	} else {
		response.Coverage.Checks = append(response.Coverage.Checks, models.CostCoverageCheck{Name: "asset_inventory", Status: "complete"})
	}

	recommendations := models.ListCostRecommendationsResponse{}
	var recommenderRetryAt time.Time
	if includeIdle {
		recommendationCollection := recommendationResult{}
		select {
		case recommendationCollection = <-recommendationResults:
		case <-ctx.Done():
			recommendationCollection.err = ctx.Err()
		}
		recommendations, err = recommendationCollection.response, recommendationCollection.err
		if err != nil {
			message := err.Error()
			if quotaErr, quotaExhausted := costRecommenderQuotaError(err); quotaExhausted {
				retryAt := quotaErr.RetryAt.UTC()
				recommenderRetryAt = retryAt
				message = "Cloud Recommender quota exhausted"
				if quotaErr.Window != "" {
					message += fmt.Sprintf(" (%s window)", quotaErr.Window)
				}
				if !retryAt.IsZero() {
					message = fmt.Sprintf("%s; retry after %s", message, retryAt.Format(time.RFC3339))
					response.Warnings = append(response.Warnings, fmt.Sprintf("Cost recommendations are quota-limited; retry after %s. Idle-resource coverage is incomplete.", retryAt.Format(time.RFC3339)))
				} else {
					response.Warnings = append(response.Warnings, "Cost recommendations are quota-limited; idle-resource coverage is incomplete.")
				}
			} else {
				response.Warnings = append(response.Warnings, "Cost recommendations were unavailable; idle-resource coverage is incomplete.")
			}
			response.Coverage.Checks = append(response.Coverage.Checks, models.CostCoverageCheck{Name: "recommender", Status: "partial", Message: message})
			response.Warnings = append(response.Warnings, recommendations.Warnings...)
		} else if !recommendations.Available {
			response.Coverage.Checks = append(response.Coverage.Checks, models.CostCoverageCheck{Name: "recommender", Status: "skipped", Message: "Recommender integration is disabled"})
			response.Warnings = append(response.Warnings, recommendations.Warnings...)
		} else if !recommendations.Complete {
			response.Coverage.Checks = append(response.Coverage.Checks, models.CostCoverageCheck{Name: "recommender", Status: "partial", Message: "Some cost recommender types or results were unavailable"})
			response.Warnings = append(response.Warnings, recommendations.Warnings...)
		} else {
			response.Coverage.Checks = append(response.Coverage.Checks, models.CostCoverageCheck{Name: "recommender", Status: "complete"})
			response.Warnings = append(response.Warnings, recommendations.Warnings...)
		}
	} else {
		response.Coverage.Checks = append(response.Coverage.Checks, models.CostCoverageCheck{Name: "recommender", Status: "skipped", Message: "include_idle=false"})
	}

	analyzeResponse(&response, facts, assets, recommendations, windows, req.MaxResults, boolValue(req.IncludeTraffic, true))
	if boolValue(req.IncludeTraffic, true) {
		e.enrichTrafficWithMonitoring(ctx, req.ProjectID, windows, &response)
	}
	finalizeResponse(&response, facts, windows, now, req.DetailLevel)
	if ctx.Err() != nil {
		response.Status = "partial"
		response.Warnings = append(response.Warnings, "Cost analysis reached its time budget; findings use completed collectors.")
	}
	response.Warnings = uniqueSorted(response.Warnings)
	e.setCachedUntil(cacheKey, response, now, recommenderRetryAt)
	e.log.InfoContext(ctx, "cost reasoning completed", "analysis_id", response.AnalysisID, "project", req.ProjectID,
		"status", response.Status, "drivers", len(response.Drivers), "bytes_processed", facts.BytesProcessed)
	return response, nil
}

func normalizeRequest(req *models.ExplainCostRequest, defaultTimezone string) error {
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.Period = strings.ToLower(strings.TrimSpace(req.Period))
	req.Comparison = strings.ToLower(strings.TrimSpace(req.Comparison))
	req.StartDate = strings.TrimSpace(req.StartDate)
	req.EndDate = strings.TrimSpace(req.EndDate)
	req.Timezone = strings.TrimSpace(req.Timezone)
	req.DetailLevel = strings.ToLower(strings.TrimSpace(req.DetailLevel))
	if req.ProjectID == "" {
		return errors.New("project_id is required")
	}
	if req.Period == "" {
		req.Period = "last_7_complete_days"
	}
	if req.Comparison == "" {
		req.Comparison = "previous_period"
	}
	if req.Comparison != "previous_period" {
		return errors.New("comparison must be previous_period")
	}
	if req.Timezone == "" {
		req.Timezone = defaultTimezone
	}
	if _, err := time.LoadLocation(req.Timezone); err != nil {
		return fmt.Errorf("invalid timezone %q", req.Timezone)
	}
	if req.DetailLevel == "" {
		req.DetailLevel = "standard"
	}
	switch req.DetailLevel {
	case "summary", "standard", "detailed":
	default:
		return errors.New("detail_level must be summary, standard, or detailed")
	}
	if req.MaxResults == 0 {
		req.MaxResults = 10
	}
	if req.MaxResults < 1 || req.MaxResults > 25 {
		return errors.New("max_results must be between 1 and 25")
	}
	return nil
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func responseCacheKey(req models.ExplainCostRequest, w analysisWindows) string {
	raw := fmt.Sprintf("%s|%s|%s|%s|%s|%d|%t|%t", req.ProjectID, w.currentStart, w.currentEnd,
		req.Timezone, req.DetailLevel, req.MaxResults, boolValue(req.IncludeIdle, true), boolValue(req.IncludeTraffic, true))
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (e *Engine) getCached(key string, now time.Time) (models.ExplainCostResponse, bool) {
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	entry, ok := e.cache[key]
	if !ok {
		return models.ExplainCostResponse{}, false
	}
	if !now.Before(entry.expiresAt) {
		delete(e.cache, key)
		return models.ExplainCostResponse{}, false
	}
	return entry.response, true
}

func (e *Engine) setCached(key string, response models.ExplainCostResponse, now time.Time) {
	e.setCachedUntil(key, response, now, time.Time{})
}

func (e *Engine) setCachedUntil(key string, response models.ExplainCostResponse, now, noLaterThan time.Time) {
	expiresAt := now.Add(cacheTTL)
	if !noLaterThan.IsZero() && noLaterThan.Before(expiresAt) {
		expiresAt = noLaterThan
	}
	if !now.Before(expiresAt) {
		e.cacheMu.Lock()
		delete(e.cache, key)
		e.cacheMu.Unlock()
		return
	}
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	for existingKey, entry := range e.cache {
		if !now.Before(entry.expiresAt) {
			delete(e.cache, existingKey)
		}
	}
	if _, exists := e.cache[key]; !exists && len(e.cache) >= maxCacheEntries {
		var oldestKey string
		var oldestExpiry time.Time
		for existingKey, entry := range e.cache {
			if oldestKey == "" || entry.expiresAt.Before(oldestExpiry) {
				oldestKey, oldestExpiry = existingKey, entry.expiresAt
			}
		}
		delete(e.cache, oldestKey)
	}
	e.cache[key] = cacheEntry{response: response, expiresAt: expiresAt}
}

func costRecommenderQuotaError(err error) (*ports.RecommenderQuotaExhaustedError, bool) {
	var quotaErr *ports.RecommenderQuotaExhaustedError
	if !errors.As(err, &quotaErr) || quotaErr == nil {
		return nil, false
	}
	return quotaErr, true
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
