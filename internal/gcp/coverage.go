package gcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/logging/logadmin"
	monitoringpb "cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (a *gcpAdapter) GetObservabilityCoverage(ctx context.Context, req models.GetObservabilityCoverageRequest) (models.ObservabilityCoverageResponse, error) {
	if err := a.rateWait(ctx, "coverage.GetObservabilityCoverage"); err != nil {
		return models.ObservabilityCoverageResponse{}, err
	}

	// ── Step 1: enumerate compute nodes ──────────────────────────────────
	type computeNode struct {
		urn         string
		kind        string
		name        string
		otelSidecar bool
	}

	var nodes []computeNode

	// Cloud Run services.
	if a.runSvc != nil {
		regions := req.Regions
		if len(regions) == 0 {
			regions = []string{"-"} // wildcard
		}
		for _, region := range regions {
			parent := fmt.Sprintf("projects/%s/locations/%s", req.ProjectID, region)
			svcResp, err := a.ListServices(ctx, models.ListServicesRequest{ProjectID: req.ProjectID, Region: region})
			if err == nil {
				for _, s := range svcResp.Services {
					urn := fmt.Sprintf("urn:gcp:run:%s:%s:cloud_run_service/%s", region, req.ProjectID, s.Name)
					nodes = append(nodes, computeNode{urn: urn, kind: models.KindCloudRunService, name: s.Name})
				}
			}
			_ = parent // suppress unused if no error path uses it
		}
	}

	if len(nodes) == 0 {
		return models.ObservabilityCoverageResponse{
			ProjectID:   req.ProjectID,
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			Nodes:       []models.NodeCoverage{},
			Summary:     models.CoverageSummary{},
		}, nil
	}

	// ── Step 2: build "has_traces" set ───────────────────────────────────
	traceNames := map[string]bool{}
	if a.traceClient != nil {
		traceResp, err := a.ListTraceDependencyEdges(ctx, models.ListTraceDependencyEdgesRequest{
			ProjectID:     req.ProjectID,
			LookbackHours: 168,
		})
		if err == nil {
			for _, e := range traceResp.Edges {
				traceNames[strings.ToLower(e.Caller)] = true
				traceNames[strings.ToLower(e.Callee)] = true
			}
		}
	}

	// ── Step 3: build "has_alerts" set ───────────────────────────────────
	alertNames := map[string]bool{}
	alertInventoryWarning := ""
	if a.monitoringSvc != nil {
		alertResp, err := a.ListAlertPolicies(ctx, models.ListAlertPoliciesRequest{ProjectID: req.ProjectID, PageSize: maxInventoryPageSize})
		if err == nil {
			for _, p := range alertResp.Policies {
				for _, cond := range p.Conditions {
					condLower := strings.ToLower(cond)
					for _, n := range nodes {
						if strings.Contains(condLower, strings.ToLower(n.name)) {
							alertNames[strings.ToLower(n.name)] = true
						}
					}
				}
			}
			if alertResp.Truncated {
				alertInventoryWarning = "Alert policy inventory was truncated; alert coverage may be understated. Narrow the project inventory or inspect the next page directly."
			}
		} else {
			alertInventoryWarning = "Alert policy inventory could not be read; alert coverage may be understated: " + err.Error()
		}
	}

	// ── Step 4: per-node metrics + log checks ─────────────────────────────
	now := time.Now().UTC()
	startTime := now.Add(-24 * time.Hour)

	coverages := make([]models.NodeCoverage, 0, len(nodes))
	for _, n := range nodes {
		nameLower := strings.ToLower(n.name)

		hasMetrics := a.checkHasMetrics(ctx, req.ProjectID, n.name, startTime, now)
		hasTraces := traceNames[nameLower]
		hasLogs := a.checkHasLogs(ctx, req.ProjectID, n.name, startTime)
		hasAlerts := alertNames[nameLower]

		score := coverageScore(hasMetrics, hasTraces, hasLogs, hasAlerts, n.otelSidecar)

		coverages = append(coverages, models.NodeCoverage{
			NodeURN:  n.urn,
			NodeKind: n.kind,
			NodeName: n.name,
			Coverage: models.ObservabilityBlock{
				HasMetrics:    hasMetrics,
				HasTraces:     hasTraces,
				HasLogs:       hasLogs,
				HasAlerts:     hasAlerts,
				OtelSidecar:   n.otelSidecar,
				CoverageScore: score,
			},
		})
	}

	// ── Step 5: build summary ─────────────────────────────────────────────
	summary := buildCoverageSummary(coverages)
	recs := buildRecommendations(coverages)
	if alertInventoryWarning != "" {
		recs = append([]string{alertInventoryWarning}, recs...)
	}

	return models.ObservabilityCoverageResponse{
		ProjectID:       req.ProjectID,
		GeneratedAt:     now.Format(time.RFC3339),
		Nodes:           coverages,
		Summary:         summary,
		Recommendations: recs,
	}, nil
}

// checkHasMetrics returns true if any Cloud Monitoring time series exists for
// the Cloud Run service in the last 24 hours.
func (a *gcpAdapter) checkHasMetrics(ctx context.Context, projectID, serviceName string, start, end time.Time) bool {
	if a.metric == nil {
		return false
	}
	filter := fmt.Sprintf(`metric.type = "run.googleapis.com/request_count" AND resource.labels.service_name = "%s"`, serviceName)
	pbReq := &monitoringpb.ListTimeSeriesRequest{
		Name:   fmt.Sprintf("projects/%s", projectID),
		Filter: filter,
		Interval: &monitoringpb.TimeInterval{
			StartTime: timestamppb.New(start),
			EndTime:   timestamppb.New(end),
		},
		View: monitoringpb.ListTimeSeriesRequest_HEADERS,
	}
	it := a.metric.ListTimeSeries(ctx, pbReq)
	_, err := it.Next()
	return err == nil || err != iterator.Done
}

// checkHasLogs returns true if at least one log entry exists for the service
// in the last 24 hours.
func (a *gcpAdapter) checkHasLogs(ctx context.Context, projectID, serviceName string, since time.Time) bool {
	if a.logAdmin == nil {
		return false
	}
	sinceRFC := since.Format(time.RFC3339)
	filter := fmt.Sprintf(
		`logName:"projects/%s/logs/" AND (resource.labels.service_name="%s" OR resource.labels.container_name="%s") AND timestamp >= "%s"`,
		projectID, serviceName, serviceName, sinceRFC,
	)
	it := a.logAdmin.Entries(ctx,
		logadmin.ProjectIDs([]string{projectID}),
		logadmin.Filter(filter),
		logadmin.NewestFirst(),
		logadmin.PageSize(1),
	)
	_, err := it.Next()
	return err == nil
}

// coverageScore computes the 0–1 weighted signal coverage score per the plan.
func coverageScore(metrics, traces, logs, alerts, otel bool) float64 {
	score := 0.0
	if metrics {
		score += 0.35
	}
	if traces {
		score += 0.35
	}
	if logs {
		score += 0.20
	}
	if alerts {
		score += 0.10
	}
	if otel {
		score += 0.10
	}
	if score > 1.0 {
		score = 1.0
	}
	return score
}

func buildCoverageSummary(coverages []models.NodeCoverage) models.CoverageSummary {
	s := models.CoverageSummary{TotalNodes: len(coverages)}
	var total float64
	for _, c := range coverages {
		if c.Coverage.HasMetrics {
			s.WithMetrics++
		}
		if c.Coverage.HasTraces {
			s.WithTraces++
		}
		if c.Coverage.HasLogs {
			s.WithLogs++
		}
		if c.Coverage.HasAlerts {
			s.WithAlerts++
		}
		if c.Coverage.OtelSidecar {
			s.WithOtelSidecar++
		}
		total += c.Coverage.CoverageScore
		if c.Coverage.CoverageScore < 0.5 {
			s.GapNodes = append(s.GapNodes, c.NodeURN)
		}
	}
	if len(coverages) > 0 {
		s.MeanCoverageScore = total / float64(len(coverages))
	}
	sort.Strings(s.GapNodes)
	return s
}

func buildRecommendations(coverages []models.NodeCoverage) []string {
	var noTraces, noMetrics, noAlerts, noOtel []string
	for _, c := range coverages {
		if !c.Coverage.HasTraces {
			noTraces = append(noTraces, c.NodeName)
		}
		if !c.Coverage.HasMetrics {
			noMetrics = append(noMetrics, c.NodeName)
		}
		if !c.Coverage.HasAlerts {
			noAlerts = append(noAlerts, c.NodeName)
		}
		if !c.Coverage.OtelSidecar {
			noOtel = append(noOtel, c.NodeName)
		}
	}

	var recs []string
	if n := len(noTraces); n > 0 {
		recs = append(recs, fmt.Sprintf("%d service(s) have no traces — add OTel instrumentation to: %s", n, strings.Join(noTraces, ", ")))
	}
	if n := len(noMetrics); n > 0 {
		recs = append(recs, fmt.Sprintf("%d service(s) have no metrics in the last 24h — check if they are receiving traffic: %s", n, strings.Join(noMetrics, ", ")))
	}
	if n := len(noAlerts); n > 0 {
		recs = append(recs, fmt.Sprintf("%d service(s) have no alert policies — review SLO coverage for: %s", n, strings.Join(noAlerts, ", ")))
	}
	if n := len(noOtel); n > 0 && n < len(coverages) {
		// Only recommend OTel sidecar if at least some services already have it.
		recs = append(recs, fmt.Sprintf("%d service(s) have no OTel sidecar — see https://opentelemetry.io/docs/instrumentation/", n))
	}
	return recs
}
