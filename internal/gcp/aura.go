package gcp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	containerpb "cloud.google.com/go/container/apiv1/containerpb"
	monitoringpb "cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

const (
	auraCacheTTL            = 5 * time.Minute
	auraSummaryConcurrency  = 8
	maxAuraSummaryResources = 100
)

var (
	auraResourceNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	auraLocationRE     = regexp.MustCompile(`^[a-z]+(?:-[a-z0-9]+)*[0-9](?:-[a-z])?$`)
)

var errAuraMetricNoData = errors.New("monitoring metric has no data in the requested window")

// recommenderQuotaNote is surfaced in AuraReport.RecommenderNote when the daily
// Recommender API quota is exhausted. The note instructs the LLM to stop polling.
const recommenderQuotaNote = "RECOMMENDER QUOTA EXHAUSTED: The Cloud Recommender API has " +
	"hit its daily limit (100 reads/day on Basic tier; 1,000,000/day on Paid tiers). " +
	"Do NOT call any Aura Score or Aura Summary tool again in this session — doing so " +
	"will not add new recommender signals and wastes tokens. The quota resets daily at " +
	"midnight Pacific Time (PT)."

// cacheKey builds the lookup key for auraCache.
func cacheKey(projectID string, kind models.ResourceKind, region, name string) string {
	return fmt.Sprintf("%s/%s/%s/%s", projectID, kind, region, name)
}

// GetAuraScore returns a composite health+efficiency score for a single named resource.
// Results are cached for auraCacheTTL to keep repeated MCP calls under 500 ms.
func (a *gcpAdapter) GetAuraScore(ctx context.Context, req models.GetAuraScoreRequest) (models.AuraReport, error) {
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	if err := validateAuraRequest(req); err != nil {
		return models.AuraReport{}, err
	}
	key := cacheKey(req.ProjectID, req.ResourceKind, req.Region, req.ResourceName)
	if cached, ok := a.auraCache.get(key); ok {
		return cached, nil
	}

	if err := a.rateWait(ctx, "aura.GetAuraScore"); err != nil {
		return models.AuraReport{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	var signals []models.AuraHealthSignal
	var quotaExhausted bool
	var err error

	switch req.ResourceKind {
	case models.ResourceKindCloudRun:
		signals, quotaExhausted, err = a.fetchCloudRunSignals(ctx, req.ProjectID, req.ResourceName, req.Region)
	case models.ResourceKindCloudSQL:
		signals, quotaExhausted, err = a.fetchCloudSQLSignals(ctx, req.ProjectID, req.ResourceName, req.Region)
	case models.ResourceKindBigQuery:
		signals, err = a.fetchBigQuerySignals(ctx, req.ProjectID, req.ResourceName)
	case models.ResourceKindGKE:
		signals, err = a.fetchGKESignals(ctx, req.ProjectID, req.ResourceName, req.Region)
	case models.ResourceKindGCS:
		var details models.GCSAuraDetails
		signals, details, err = a.fetchGCSSignals(ctx, req.ProjectID, req.ResourceName)
		_ = details // generic path discards extra fields
	default:
		return models.AuraReport{}, fmt.Errorf("aura.GetAuraScore: unsupported resource_kind %q", req.ResourceKind)
	}
	if err != nil {
		return models.AuraReport{}, wrapGCPError("aura.GetAuraScore", err)
	}

	report := calculateAura(req.ResourceKind, req.ResourceName, req.Region, signals)
	report.CachedAt = time.Now().UTC()
	if quotaExhausted {
		report.RecommenderNote = recommenderQuotaNote
	}
	a.auraCache.set(key, report)
	return report, nil
}

func validateAuraRequest(req models.GetAuraScoreRequest) error {
	if !costProjectIDRE.MatchString(strings.TrimSpace(req.ProjectID)) {
		return fmt.Errorf("aura.GetAuraScore: invalid project_id")
	}
	if len(req.ResourceName) > 1024 || !auraResourceNameRE.MatchString(req.ResourceName) {
		return fmt.Errorf("aura.GetAuraScore: invalid resource_name")
	}
	if req.Region != "" && !auraLocationRE.MatchString(req.Region) {
		return fmt.Errorf("aura.GetAuraScore: invalid region")
	}
	return nil
}

// ratioMetricValue combines independently fetched numerator and denominator
// metrics without treating an absent zero-valued numerator as missing telemetry.
// Monitoring commonly returns no series for zero errors, while still returning
// a positive total count. A missing or zero denominator remains unavailable
// because the ratio cannot be established safely.
func ratioMetricValue(numerator float64, numeratorErr error, denominator float64, denominatorErr error) (float64, error) {
	if denominatorErr != nil {
		return 0, denominatorErr
	}
	if denominator <= 0 {
		return 0, fmt.Errorf("%w: ratio denominator is zero", errAuraMetricNoData)
	}
	if numeratorErr != nil {
		if errors.Is(numeratorErr, errAuraMetricNoData) {
			return 0, nil
		}
		return 0, numeratorErr
	}
	return numerator / denominator, nil
}

func metricHealthSignal(name string, value float64, err error, score func(float64) (int, string)) models.AuraHealthSignal {
	if err != nil {
		availability, label := "error", "Unavailable"
		if errors.Is(err, errAuraMetricNoData) {
			availability, label = "no_data", "No data"
		}
		return models.AuraHealthSignal{Name: name, Availability: availability, Label: label, Message: err.Error()}
	}
	signalScore, label := score(value)
	return models.AuraHealthSignal{Name: name, Value: value, Score: signalScore, Label: label, Availability: "observed"}
}

func signalObserved(signal models.AuraHealthSignal) bool {
	return signal.Availability == "" || signal.Availability == "observed"
}

func expectedAuraSignals(kind models.ResourceKind) []string {
	switch kind {
	case models.ResourceKindCloudRun:
		return []string{"error_rate", "cpu_util", "latency_p99", "request_count_total"}
	case models.ResourceKindCloudSQL:
		return []string{"cpu_util", "memory_util", "disk_util"}
	case models.ResourceKindBigQuery:
		return []string{"job_failure_rate", "slot_utilization", "storage_bytes"}
	case models.ResourceKindGKE:
		return []string{"node_cpu_util", "node_mem_util", "pod_restart_rate", "control_plane_health"}
	case models.ResourceKindGCS:
		return []string{"public_access_prevention", "uniform_bucket_level_access", "versioning", "lifecycle_policy", "storage_class_fit"}
	default:
		return nil
	}
}

// GetProjectAuraSummary discovers all Cloud Run, Cloud SQL, and BigQuery resources in the
// project and returns their Aura Scores sorted worst-first.
func (a *gcpAdapter) GetProjectAuraSummary(ctx context.Context, req models.ProjectAuraSummaryRequest) (models.ProjectAuraSummaryResponse, error) {
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	if !costProjectIDRE.MatchString(req.ProjectID) || (req.Region != "" && !auraLocationRE.MatchString(req.Region)) {
		return models.ProjectAuraSummaryResponse{}, fmt.Errorf("aura.GetProjectAuraSummary: invalid project_id or region")
	}
	if err := a.rateWait(ctx, "aura.GetProjectAuraSummary"); err != nil {
		return models.ProjectAuraSummaryResponse{}, err
	}
	// Project summaries fan out into several metric reads per resource. Use the
	// bounded graph-operation budget rather than one single-call timeout.
	ctx, cancel := context.WithTimeout(ctx, a.graphTimeout)
	defer cancel()

	// Discover a bounded resource inventory.
	type resource struct {
		kind   models.ResourceKind
		name   string
		region string
	}
	var resources []resource
	truncated := false
	var discoveryWarnings []string
	appendResource := func(value resource) bool {
		if len(resources) >= maxAuraSummaryResources {
			truncated = true
			return false
		}
		resources = append(resources, value)
		return true
	}

	crServices, err := a.ListServices(ctx, models.ListServicesRequest{ProjectID: req.ProjectID, Region: req.Region})
	if err != nil {
		return models.ProjectAuraSummaryResponse{}, wrapGCPError("aura.GetProjectAuraSummary", err)
	}
	for _, svc := range crServices.Services {
		if !appendResource(resource{models.ResourceKindCloudRun, svc.Name, svc.Region}) {
			break
		}
	}
	truncated = truncated || crServices.Truncated
	if truncated {
		discoveryWarnings = append(discoveryWarnings, fmt.Sprintf("Aura resource discovery stopped at %d resources; narrow by region", maxAuraSummaryResources))
	}

	var sqlInstances []sqlInstance
	err = nil
	if !truncated {
		sqlInstances, err = a.listSQLInstances(ctx, req.ProjectID)
	}
	if err != nil {
		if errors.Is(err, errInventoryLimitReached) {
			truncated = true
			discoveryWarnings = append(discoveryWarnings, fmt.Sprintf("Aura resource discovery stopped at %d resources; narrow by region", maxAuraSummaryResources))
		} else {
			// Non-fatal: log and continue with whatever we have.
			a.log.WarnContext(ctx, "aura: could not list Cloud SQL instances", "err", err)
		}
	}
	for _, inst := range sqlInstances {
		if !appendResource(resource{models.ResourceKindCloudSQL, inst.name, inst.region}) {
			break
		}
	}

	var bqDatasets []string
	err = nil
	if !truncated {
		bqDatasets, err = a.listBigQueryDatasets(ctx, req.ProjectID)
	}
	if err != nil {
		if errors.Is(err, errInventoryLimitReached) {
			truncated = true
			discoveryWarnings = append(discoveryWarnings, fmt.Sprintf("Aura resource discovery stopped at %d resources; narrow by region", maxAuraSummaryResources))
		} else {
			a.log.WarnContext(ctx, "aura: could not list BigQuery datasets", "err", err)
		}
	}
	for _, ds := range bqDatasets {
		if !appendResource(resource{models.ResourceKindBigQuery, ds, ""}) {
			break
		}
	}

	var gkeClusters []gkeCluster
	err = nil
	if !truncated {
		gkeClusters, err = a.listGKEClusters(ctx, req.ProjectID)
	}
	if err != nil {
		a.log.WarnContext(ctx, "aura: could not list GKE clusters", "err", err)
	}
	for _, c := range gkeClusters {
		if !appendResource(resource{models.ResourceKindGKE, c.name, c.location}) {
			break
		}
	}
	if truncated && len(discoveryWarnings) == 0 {
		discoveryWarnings = append(discoveryWarnings, fmt.Sprintf("Aura resource discovery stopped at %d resources; narrow by region", maxAuraSummaryResources))
	}

	// Fan-out: score every resource concurrently.
	reports := make([]models.AuraReport, len(resources))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(auraSummaryConcurrency)
	for i, r := range resources {
		g.Go(func() error {
			rep, err := a.GetAuraScore(gctx, models.GetAuraScoreRequest{
				ProjectID:    req.ProjectID,
				ResourceKind: r.kind,
				ResourceName: r.name,
				Region:       r.region,
			})
			if err != nil {
				// Degrade gracefully: emit an explicitly unavailable placeholder.
				reports[i] = models.AuraReport{
					ResourceKind:   r.kind,
					ResourceName:   r.name,
					Region:         r.region,
					Score:          0,
					Band:           models.AuraBandUnavailable,
					Display:        fmt.Sprintf("⚪ %s: %s | Aura: N/A (fetch error: %v)", kindLabel(r.kind), r.name, err),
					Reasons:        []string{fmt.Sprintf("Failed to fetch metrics: %v", err)},
					CoverageStatus: "unavailable",
					Warnings:       []string{err.Error()},
				}
				return nil // don't abort sibling goroutines
			}
			reports[i] = rep
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return models.ProjectAuraSummaryResponse{}, err
	}

	// Sort scored resources worst-first, with unavailable resources last. Their
	// placeholder score is not a health score and must not rank as critical.
	sort.Slice(reports, func(i, j int) bool {
		iUnavailable := reports[i].CoverageStatus == "unavailable"
		jUnavailable := reports[j].CoverageStatus == "unavailable"
		if iUnavailable != jUnavailable {
			return !iUnavailable
		}
		if reports[i].Score != reports[j].Score {
			return reports[i].Score < reports[j].Score
		}
		if reports[i].ResourceKind != reports[j].ResourceKind {
			return reports[i].ResourceKind < reports[j].ResourceKind
		}
		if reports[i].Region != reports[j].Region {
			return reports[i].Region < reports[j].Region
		}
		return reports[i].ResourceName < reports[j].ResourceName
	})

	// Build summary block.
	var lines []string
	var critical, warning, healthy, unavailable int
	for _, r := range reports {
		lines = append(lines, r.Display)
		if r.CoverageStatus == "unavailable" {
			unavailable++
			continue
		}
		switch r.Band {
		case models.AuraBandRed:
			critical++
		case models.AuraBandYellow:
			warning++
		default:
			healthy++
		}
	}

	return models.ProjectAuraSummaryResponse{
		ProjectID:        req.ProjectID,
		Resources:        reports,
		Summary:          strings.Join(lines, "\n"),
		TotalCount:       len(reports),
		CriticalCount:    critical,
		WarningCount:     warning,
		HealthyCount:     healthy,
		UnavailableCount: unavailable,
		Truncated:        truncated,
		Warnings:         discoveryWarnings,
	}, nil
}

// ---------------------------------------------------------------------------
// Per-resource signal fetchers
// ---------------------------------------------------------------------------

func (a *gcpAdapter) fetchCloudRunSignals(ctx context.Context, projectID, name, region string) ([]models.AuraHealthSignal, bool, error) {
	baseFilter := fmt.Sprintf(`resource.labels.service_name = "%s" AND resource.labels.location = "%s"`, escapeMonitoringString(name), escapeMonitoringString(region))

	var (
		errCount5xx, totalCount, cpuUtil, latencyP99   float64
		errCountErr, totalCountErr, cpuErr, latencyErr error
		recInsights                                    []recommenderInsight
		quotaExhausted                                 bool
	)

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		v, err := a.fetchRateMetric(gctx, projectID,
			`run.googleapis.com/request_count`,
			baseFilter+` AND metric.labels.response_code_class = "5xx"`, 60)
		errCount5xx, errCountErr = v, err
		return nil
	})
	g.Go(func() error {
		v, err := a.fetchRateMetric(gctx, projectID,
			`run.googleapis.com/request_count`,
			baseFilter, 60)
		totalCount, totalCountErr = v, err
		return nil
	})
	g.Go(func() error {
		// cpu/utilizations is a DELTA DISTRIBUTION metric; ALIGN_MEAN is rejected by the
		// Monitoring API. Use p50 (median) to get a representative utilization value.
		v, err := a.fetchPercentileMetric(gctx, projectID,
			`run.googleapis.com/container/cpu/utilizations`,
			baseFilter, 50, 60)
		cpuUtil, cpuErr = v, err
		return nil
	})
	g.Go(func() error {
		v, err := a.fetchPercentileMetric(gctx, projectID,
			`run.googleapis.com/request_latencies`,
			baseFilter, 99, 60)
		latencyP99, latencyErr = v, err
		return nil
	})
	g.Go(func() error {
		ins, err := a.fetchRecommenderInsights(
			gctx, projectID, region,
			recommenderIDCloudRunIdle,
			"/services/"+name,
		)
		if err != nil {
			var qe *ports.RecommenderQuotaExhaustedError
			if errors.As(err, &qe) {
				quotaExhausted = true
				return nil
			}
			a.log.WarnContext(gctx, "aura: recommender unavailable for Cloud Run", "service", name, "err", err)
			return nil
		}
		recInsights = ins
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, false, err
	}

	errorRate, errorRateErr := ratioMetricValue(errCount5xx, errCountErr, totalCount, totalCountErr)
	signals := []models.AuraHealthSignal{
		metricHealthSignal("error_rate", round4(errorRate), errorRateErr, func(value float64) (int, string) { return cloudRunSignalScore("error_rate", value) }),
		metricHealthSignal("cpu_util", round4(cpuUtil), cpuErr, func(value float64) (int, string) { return cloudRunSignalScore("cpu_util", value) }),
		metricHealthSignal("latency_p99", math.Round(latencyP99), latencyErr, func(value float64) (int, string) { return cloudRunSignalScore("latency_p99", value) }),
		metricHealthSignal("request_count_total", round4(totalCount), totalCountErr, func(float64) (int, string) { return 100, "info" }),
	}
	for _, ins := range recInsights {
		signals = append(signals, recommenderSignal(ins))
	}
	return signals, quotaExhausted, nil
}

func (a *gcpAdapter) fetchCloudSQLSignals(ctx context.Context, projectID, instanceName, region string) ([]models.AuraHealthSignal, bool, error) {
	// Cloud SQL instance IDs are formatted as "project:region:instance" in some APIs
	// but resource.labels.database_id uses the plain instance name.
	baseFilter := fmt.Sprintf(`resource.labels.database_id = "%s:%s"`, escapeMonitoringString(projectID), escapeMonitoringString(instanceName))

	var cpuUtil, memUtil, diskUtil float64
	var cpuErr, memErr, diskErr error
	var idleInsights, overprovisionedInsights []recommenderInsight
	var idleQuotaExhausted, overprovisionedQuotaExhausted bool

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		cpuUtil, cpuErr = a.fetchMeanMetric(gctx, projectID, `cloudsql.googleapis.com/database/cpu/utilization`, baseFilter, 60)
		return nil
	})
	g.Go(func() error {
		memUtil, memErr = a.fetchMeanMetric(gctx, projectID, `cloudsql.googleapis.com/database/memory/utilization`, baseFilter, 60)
		return nil
	})
	g.Go(func() error {
		diskUtil, diskErr = a.fetchMeanMetric(gctx, projectID, `cloudsql.googleapis.com/database/disk/utilization`, baseFilter, 60)
		return nil
	})
	// Fetch both Cloud SQL recommenders concurrently; results are merged.
	g.Go(func() error {
		idle, err := a.fetchRecommenderInsights(
			gctx, projectID, region,
			recommenderIDCloudSQLIdle,
			"/instances/"+instanceName,
		)
		if err != nil {
			var qe *ports.RecommenderQuotaExhaustedError
			if errors.As(err, &qe) {
				idleQuotaExhausted = true
				return nil
			}
			a.log.WarnContext(gctx, "aura: recommender unavailable for Cloud SQL idle", "instance", instanceName, "err", err)
			return nil
		}
		idleInsights = idle
		return nil
	})
	g.Go(func() error {
		op, err := a.fetchRecommenderInsights(
			gctx, projectID, region,
			recommenderIDCloudSQLOverpro,
			"/instances/"+instanceName,
		)
		if err != nil {
			var qe *ports.RecommenderQuotaExhaustedError
			if errors.As(err, &qe) {
				overprovisionedQuotaExhausted = true
				return nil
			}
			a.log.WarnContext(gctx, "aura: recommender unavailable for Cloud SQL overprovisioned", "instance", instanceName, "err", err)
			return nil
		}
		overprovisionedInsights = op
		return nil
	})

	_ = g.Wait()

	signals := []models.AuraHealthSignal{
		metricHealthSignal("cpu_util", round4(cpuUtil), cpuErr, sqlSignalScore),
		metricHealthSignal("memory_util", round4(memUtil), memErr, sqlSignalScore),
		metricHealthSignal("disk_util", round4(diskUtil), diskErr, sqlSignalScore),
	}
	recInsights := append(idleInsights, overprovisionedInsights...)
	for _, ins := range recInsights {
		signals = append(signals, recommenderSignal(ins))
	}
	return signals, idleQuotaExhausted || overprovisionedQuotaExhausted, nil
}

func (a *gcpAdapter) fetchBigQuerySignals(ctx context.Context, projectID, datasetID string) ([]models.AuraHealthSignal, error) {
	projectFilter := fmt.Sprintf(`resource.labels.project_id = "%s"`, escapeMonitoringString(projectID))

	var failedJobs, totalJobs, slotsAlloc, storageBytes float64
	var failedJobsErr, totalJobsErr, slotsErr, storageErr error

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		failedJobs, failedJobsErr = a.fetchMeanMetric(gctx, projectID,
			`bigquery.googleapis.com/job_count`,
			projectFilter+` AND metric.labels.status = "error"`, 60)
		return nil
	})
	g.Go(func() error {
		totalJobs, totalJobsErr = a.fetchMeanMetric(gctx, projectID,
			`bigquery.googleapis.com/job_count`,
			projectFilter, 60)
		return nil
	})
	g.Go(func() error {
		slotsAlloc, slotsErr = a.fetchMeanMetric(gctx, projectID,
			`bigquery.googleapis.com/slots/allocated_for_project`,
			projectFilter, 60)
		return nil
	})
	g.Go(func() error {
		dsFilter := fmt.Sprintf(`resource.labels.dataset_id = "%s"`, escapeMonitoringString(datasetID))
		storageBytes, storageErr = a.fetchMeanMetric(gctx, projectID,
			`bigquery.googleapis.com/storage/billable_bytes_stored`,
			dsFilter, 60)
		return nil
	})

	_ = g.Wait()

	failRate, failRateErr := ratioMetricValue(failedJobs, failedJobsErr, totalJobs, totalJobsErr)

	return []models.AuraHealthSignal{
		metricHealthSignal("job_failure_rate", round4(failRate), failRateErr, func(value float64) (int, string) { return bqSignalScore("job_failure_rate", value) }),
		metricHealthSignal("slot_utilization", round4(slotsAlloc), slotsErr, func(value float64) (int, string) { return bqSignalScore("slot_utilization", value) }),
		metricHealthSignal("storage_bytes", storageBytes, storageErr, func(value float64) (int, string) { return bqSignalScore("storage_bytes", value) }),
	}, nil
}

func (a *gcpAdapter) fetchGKESignals(ctx context.Context, projectID, clusterName, location string) ([]models.AuraHealthSignal, error) {
	clusterFilter := fmt.Sprintf(`resource.labels.cluster_name = "%s" AND resource.labels.location = "%s"`, escapeMonitoringString(clusterName), escapeMonitoringString(location))

	var nodeCPU, nodeMem, restartRate, ctrlHealth float64
	var nodeCPUErr, nodeMemErr, restartErr, ctrlErr error

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		nodeCPU, nodeCPUErr = a.fetchMeanMetric(gctx, projectID,
			`kubernetes.io/node/cpu/allocatable_utilization`,
			clusterFilter, 60)
		return nil
	})
	g.Go(func() error {
		nodeMem, nodeMemErr = a.fetchMeanMetric(gctx, projectID,
			`kubernetes.io/node/memory/allocatable_utilization`,
			clusterFilter, 60)
		return nil
	})
	g.Go(func() error {
		restartRate, restartErr = a.fetchRateMetric(gctx, projectID,
			`kubernetes.io/container/restart_count`,
			clusterFilter, 60)
		return nil
	})
	g.Go(func() error {
		if a.clusterMgr == nil {
			ctrlErr = fmt.Errorf("GKE control-plane client is unavailable")
			return nil
		}
		clusterPath := fmt.Sprintf("projects/%s/locations/%s/clusters/%s", projectID, location, clusterName)
		c, err := a.clusterMgr.GetCluster(gctx, &containerpb.GetClusterRequest{Name: clusterPath})
		if err != nil {
			ctrlErr = fmt.Errorf("GKE control-plane health check: %w", err)
			return nil
		}
		switch c.Status {
		case containerpb.Cluster_RUNNING:
			ctrlHealth = 1.0
		case containerpb.Cluster_RECONCILING, containerpb.Cluster_PROVISIONING:
			ctrlHealth = 0.5
		default: // ERROR, DEGRADED, STOPPING, STATUS_UNSPECIFIED
			ctrlHealth = 0.0
		}
		return nil
	})

	_ = g.Wait()

	return []models.AuraHealthSignal{
		metricHealthSignal("node_cpu_util", round4(nodeCPU), nodeCPUErr, func(value float64) (int, string) { return gkeSignalScore("node_cpu_util", value) }),
		metricHealthSignal("node_mem_util", round4(nodeMem), nodeMemErr, func(value float64) (int, string) { return gkeSignalScore("node_mem_util", value) }),
		metricHealthSignal("pod_restart_rate", round4(restartRate), restartErr, func(value float64) (int, string) { return gkeSignalScore("pod_restart_rate", value) }),
		metricHealthSignal("control_plane_health", ctrlHealth, ctrlErr, func(value float64) (int, string) { return gkeSignalScore("control_plane_health", value) }),
	}, nil
}

// fetchGKESignalsRich fans out 5 goroutines: the 3 metric fetches from
// fetchGKESignals, plus GetCluster (for node-pool autoscaling details) and
// GetServerConfig (for release-channel version drift). A missing input makes the
// richer audit unavailable so callers do not receive a falsely healthy score.
func (a *gcpAdapter) fetchGKESignalsRich(ctx context.Context, projectID, clusterName, location string) (
	[]models.AuraHealthSignal, []models.NodePoolAudit, models.GKEVersionDrift, error,
) {
	clusterFilter := fmt.Sprintf(`resource.labels.cluster_name = "%s" AND resource.labels.location = "%s"`, escapeMonitoringString(clusterName), escapeMonitoringString(location))

	var nodeCPU, nodeMem, restartRate float64
	ctrlHealth := 0.0
	var pools []models.NodePoolAudit
	var channelName, currentVersion string
	var serverChannels map[string]string // channel name → latest default version

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		v, err := a.fetchMeanMetric(gctx, projectID, `kubernetes.io/node/cpu/allocatable_utilization`, clusterFilter, 60)
		if err != nil {
			return err
		}
		nodeCPU = v
		return nil
	})
	g.Go(func() error {
		v, err := a.fetchMeanMetric(gctx, projectID, `kubernetes.io/node/memory/allocatable_utilization`, clusterFilter, 60)
		if err != nil {
			return err
		}
		nodeMem = v
		return nil
	})
	g.Go(func() error {
		v, err := a.fetchRateMetric(gctx, projectID, `kubernetes.io/container/restart_count`, clusterFilter, 60)
		if err != nil {
			return err
		}
		restartRate = v
		return nil
	})
	g.Go(func() error {
		if a.clusterMgr == nil {
			return fmt.Errorf("GKE control-plane client is unavailable")
		}
		clusterPath := fmt.Sprintf("projects/%s/locations/%s/clusters/%s", projectID, location, clusterName)
		c, err := a.clusterMgr.GetCluster(gctx, &containerpb.GetClusterRequest{Name: clusterPath})
		if err != nil {
			return fmt.Errorf("GKE GetCluster: %w", err)
		}
		switch c.Status {
		case containerpb.Cluster_RUNNING:
			ctrlHealth = 1.0
		case containerpb.Cluster_RECONCILING, containerpb.Cluster_PROVISIONING:
			ctrlHealth = 0.5
		default:
			ctrlHealth = 0.0
		}
		currentVersion = c.CurrentMasterVersion
		if c.ReleaseChannel != nil {
			channelName = c.ReleaseChannel.Channel.String()
		}
		for _, np := range c.NodePools {
			audit := models.NodePoolAudit{
				Name:             np.Name,
				InitialNodeCount: np.InitialNodeCount,
			}
			if np.Autoscaling != nil {
				audit.AutoscalingEnabled = np.Autoscaling.Enabled
				audit.MinNodeCount = np.Autoscaling.MinNodeCount
				audit.MaxNodeCount = np.Autoscaling.MaxNodeCount
				if audit.AutoscalingEnabled && audit.MaxNodeCount > 0 {
					audit.AtMaxCapacity = np.InitialNodeCount >= audit.MaxNodeCount
				}
			}
			if np.Management != nil {
				audit.AutoRepair = np.Management.AutoRepair
				audit.AutoUpgrade = np.Management.AutoUpgrade
			}
			pools = append(pools, audit)
		}
		return nil
	})
	g.Go(func() error {
		if a.clusterMgr == nil {
			return fmt.Errorf("GKE server-config client is unavailable")
		}
		cfg, err := a.clusterMgr.GetServerConfig(gctx, &containerpb.GetServerConfigRequest{
			Name: fmt.Sprintf("projects/%s/locations/%s", projectID, location),
		})
		if err != nil {
			return fmt.Errorf("GKE GetServerConfig: %w", err)
		}
		m := make(map[string]string, len(cfg.Channels))
		for _, ch := range cfg.Channels {
			m[ch.Channel.String()] = ch.DefaultVersion
		}
		serverChannels = m
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, nil, models.GKEVersionDrift{}, err
	}

	latestVersion := serverChannels[channelName]
	vd := models.GKEVersionDrift{
		Channel:        channelName,
		CurrentVersion: currentVersion,
		LatestVersion:  latestVersion,
		IsCurrent:      latestVersion == "" || latestVersion == currentVersion,
	}
	driftScore, driftLabel := gkeVersionDriftScore(currentVersion, latestVersion)

	// Autoscaling efficiency: inject boolean sentinel signals only when a problem exists.
	// These are read by weightedScores to apply efficiency deductions without affecting
	// the health weighted average (they have no health weight).
	anyAutoscalingDisabled := false
	anyAtMax := false
	for _, p := range pools {
		if !strings.HasPrefix(p.Name, "default-pool") || len(pools) == 1 {
			if !p.AutoscalingEnabled {
				anyAutoscalingDisabled = true
			}
		}
		if p.AtMaxCapacity {
			anyAtMax = true
		}
	}

	cpuScore, cpuLabel := gkeSignalScore("node_cpu_util", nodeCPU)
	memScore, memLabel := gkeSignalScore("node_mem_util", nodeMem)
	restartScore, restartLabel := gkeSignalScore("pod_restart_rate", restartRate)
	ctrlScore, ctrlLabel := gkeSignalScore("control_plane_health", ctrlHealth)

	signals := []models.AuraHealthSignal{
		{Name: "node_cpu_util", Value: round4(nodeCPU), Score: cpuScore, Label: cpuLabel},
		{Name: "node_mem_util", Value: round4(nodeMem), Score: memScore, Label: memLabel},
		{Name: "pod_restart_rate", Value: round4(restartRate), Score: restartScore, Label: restartLabel},
		{Name: "control_plane_health", Value: ctrlHealth, Score: ctrlScore, Label: ctrlLabel},
		{Name: "version_drift", Value: boolToFloat(vd.IsCurrent), Score: driftScore, Label: driftLabel},
	}
	if anyAutoscalingDisabled {
		signals = append(signals, models.AuraHealthSignal{Name: "autoscaling_disabled", Value: 1.0, Score: 0, Label: "Warning"})
	}
	if anyAtMax {
		signals = append(signals, models.AuraHealthSignal{Name: "nodepool_at_max", Value: 1.0, Score: 0, Label: "Warning"})
	}
	return signals, pools, vd, nil
}

// GetGKEAuraScore returns a rich GKE Aura Score including node-pool autoscaling
// audit and version-drift analysis. Results are not cached (use for fresh deep-dives).
func (a *gcpAdapter) GetGKEAuraScore(ctx context.Context, req models.GetGKEAuraScoreRequest) (models.GKEAuraReport, error) {
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	if err := validateAuraRequest(models.GetAuraScoreRequest{
		ProjectID: req.ProjectID, ResourceKind: models.ResourceKindGKE, ResourceName: req.ClusterName, Region: req.Location,
	}); err != nil {
		return models.GKEAuraReport{}, err
	}
	if err := a.rateWait(ctx, "aura.GetGKEAuraScore"); err != nil {
		return models.GKEAuraReport{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	signals, pools, vd, err := a.fetchGKESignalsRich(ctx, req.ProjectID, req.ClusterName, req.Location)
	if err != nil {
		return models.GKEAuraReport{}, wrapGCPError("aura.GetGKEAuraScore", err)
	}
	base := calculateAura(models.ResourceKindGKE, req.ClusterName, req.Location, signals)
	return models.GKEAuraReport{AuraReport: base, NodePools: pools, VersionDrift: vd}, nil
}

// gkeVersionDriftScore maps the gap between a cluster's current master version and the
// channel's latest default version to a 0-100 score using GKE minor-version distance.
func gkeVersionDriftScore(current, latest string) (int, string) {
	if latest == "" || current == "" || current == latest {
		return 100, "OK"
	}
	curMinor := parseGKEMinorVersion(current)
	latMinor := parseGKEMinorVersion(latest)
	diff := latMinor - curMinor
	switch {
	case diff <= 0:
		return 100, "OK"
	case diff == 1:
		return 70, "Warning"
	default:
		return 20, "Critical"
	}
}

// parseGKEMinorVersion extracts the minor version integer from a GKE version string
// such as "1.29.4-gke.100". Returns 0 when the string cannot be parsed.
func parseGKEMinorVersion(v string) int {
	// Format: MAJOR.MINOR.PATCH[-gke.BUILD]
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return 0
	}
	minor := 0
	for _, ch := range parts[1] {
		if ch < '0' || ch > '9' {
			break
		}
		minor = minor*10 + int(ch-'0')
	}
	return minor
}

// gkeSignalScore returns (score 0-100, label) for a single GKE health signal.
func gkeSignalScore(name string, value float64) (int, string) {
	switch name {
	case "node_cpu_util", "node_mem_util":
		switch {
		case value < 0.10:
			return 50, "Warning" // idle / over-provisioned nodes
		case value < 0.75:
			return 100, "OK"
		case value < 0.90:
			return 70, "OK"
		case value < 0.95:
			return 40, "Warning"
		default:
			return 10, "Critical"
		}
	case "pod_restart_rate": // restarts per second across the cluster
		switch {
		case value == 0:
			return 100, "OK"
		case value < 0.005:
			return 70, "Warning"
		default:
			return 20, "Critical"
		}
	case "control_plane_health": // 1.0=healthy, 0.5=reconciling, 0.0=error
		switch {
		case value >= 1.0:
			return 100, "OK"
		case value >= 0.5:
			return 60, "Warning"
		default:
			return 0, "Critical"
		}
	}
	return 100, "OK"
}

// gkeCluster is a minimal cluster descriptor used for Aura discovery.
type gkeCluster struct{ name, location string }

// listGKEClusters enumerates all GKE clusters across all locations in a project.
// Returns nil, nil when clusterMgr is not initialised (graceful degradation).
func (a *gcpAdapter) listGKEClusters(ctx context.Context, projectID string) ([]gkeCluster, error) {
	if a.clusterMgr == nil {
		return nil, nil
	}
	resp, err := a.clusterMgr.ListClusters(ctx, &containerpb.ListClustersRequest{
		Parent: fmt.Sprintf("projects/%s/locations/-", projectID),
	})
	if err != nil {
		return nil, err
	}
	out := make([]gkeCluster, 0, len(resp.Clusters))
	for _, c := range resp.Clusters {
		out = append(out, gkeCluster{name: c.Name, location: c.Location})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Low-level metric helpers
// ---------------------------------------------------------------------------

// fetchMeanMetric returns the mean value of the most recent aligned point for a metric.
func (a *gcpAdapter) fetchMeanMetric(ctx context.Context, projectID, metricType, filter string, lookbackMinutes int) (float64, error) {
	return a.fetchMetricPoint(ctx, projectID, metricType, filter, lookbackMinutes, monitoringpb.Aggregation_ALIGN_MEAN)
}

// fetchRateMetric converts a CUMULATIVE/DELTA metric into a per-second rate before averaging.
// Use this for request_count and similar counter metrics instead of ALIGN_MEAN.
func (a *gcpAdapter) fetchRateMetric(ctx context.Context, projectID, metricType, filter string, lookbackMinutes int) (float64, error) {
	return a.fetchMetricPoint(ctx, projectID, metricType, filter, lookbackMinutes, monitoringpb.Aggregation_ALIGN_RATE)
}

// fetchPercentileMetric returns a per-series percentile-aligned point.
// Supported percentiles: 50, 95, 99 (anything else defaults to p99).
// The result is the mean across all series.
func (a *gcpAdapter) fetchPercentileMetric(ctx context.Context, projectID, metricType, filter string, percentile int, lookbackMinutes int) (float64, error) {
	aligner := monitoringpb.Aggregation_ALIGN_PERCENTILE_99
	switch percentile {
	case 50:
		aligner = monitoringpb.Aggregation_ALIGN_PERCENTILE_50
	case 95:
		aligner = monitoringpb.Aggregation_ALIGN_PERCENTILE_95
	}
	return a.fetchMetricPoint(ctx, projectID, metricType, filter, lookbackMinutes, aligner)
}

func (a *gcpAdapter) fetchMetricPoint(ctx context.Context, projectID, metricType, filter string, lookbackMinutes int, aligner monitoringpb.Aggregation_Aligner) (float64, error) {
	now := time.Now().UTC()
	start := now.Add(-time.Duration(lookbackMinutes) * time.Minute)

	fullFilter := fmt.Sprintf(`metric.type = "%s"`, escapeMonitoringString(metricType))
	if filter != "" {
		fullFilter += " AND " + filter
	}

	req := &monitoringpb.ListTimeSeriesRequest{
		Name:   fmt.Sprintf("projects/%s", projectID),
		Filter: fullFilter,
		Interval: &monitoringpb.TimeInterval{
			StartTime: timestamppb.New(start),
			EndTime:   timestamppb.New(now),
		},
		Aggregation: &monitoringpb.Aggregation{
			AlignmentPeriod:    durationpb.New(time.Duration(lookbackMinutes) * time.Minute),
			PerSeriesAligner:   aligner,
			CrossSeriesReducer: monitoringpb.Aggregation_REDUCE_MEAN,
		},
		View: monitoringpb.ListTimeSeriesRequest_FULL,
	}

	it := a.metric.ListTimeSeries(ctx, req)
	ts, err := it.Next()
	if err == iterator.Done {
		return 0, fmt.Errorf("%w: %s", errAuraMetricNoData, metricType)
	}
	if err != nil {
		return 0, err
	}
	if len(ts.Points) == 0 {
		return 0, fmt.Errorf("%w: %s", errAuraMetricNoData, metricType)
	}
	return extractPointValue(ts.Points[0]), nil
}

// ---------------------------------------------------------------------------
// Resource discovery helpers
// ---------------------------------------------------------------------------

type sqlInstance struct{ name, region string }

func (a *gcpAdapter) listSQLInstances(ctx context.Context, projectID string) ([]sqlInstance, error) {
	// Use Cloud Monitoring to enumerate instance IDs rather than requiring the
	// SQL Admin API — this keeps the permission surface minimal (monitoring viewer).
	now := time.Now().UTC()
	req := &monitoringpb.ListTimeSeriesRequest{
		Name:   fmt.Sprintf("projects/%s", projectID),
		Filter: `metric.type = "cloudsql.googleapis.com/database/cpu/utilization"`,
		Interval: &monitoringpb.TimeInterval{
			StartTime: timestamppb.New(now.Add(-10 * time.Minute)),
			EndTime:   timestamppb.New(now),
		},
		Aggregation: &monitoringpb.Aggregation{
			AlignmentPeriod:  durationpb.New(10 * time.Minute),
			PerSeriesAligner: monitoringpb.Aggregation_ALIGN_MEAN,
		},
		View: monitoringpb.ListTimeSeriesRequest_HEADERS, // labels only, no points
	}

	it := a.metric.ListTimeSeries(ctx, req)
	seen := map[string]bool{}
	var instances []sqlInstance
	for scanned := 0; ; scanned++ {
		ts, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		if scanned >= maxUnpagedInventoryItems {
			return instances, errInventoryLimitReached
		}
		if ts.Resource == nil {
			continue
		}
		dbID := ts.Resource.Labels["database_id"] // "project:instance"
		region := ts.Resource.Labels["region"]
		if dbID == "" || seen[dbID] {
			continue
		}
		seen[dbID] = true
		// Strip "project:" prefix to get the bare instance name.
		parts := strings.SplitN(dbID, ":", 2)
		instName := parts[len(parts)-1]
		instances = append(instances, sqlInstance{name: instName, region: region})
	}
	return instances, nil
}

func (a *gcpAdapter) listBigQueryDatasets(ctx context.Context, projectID string) ([]string, error) {
	now := time.Now().UTC()
	req := &monitoringpb.ListTimeSeriesRequest{
		Name:   fmt.Sprintf("projects/%s", projectID),
		Filter: `metric.type = "bigquery.googleapis.com/storage/billable_bytes_stored"`,
		Interval: &monitoringpb.TimeInterval{
			StartTime: timestamppb.New(now.Add(-24 * time.Hour)), // BQ storage metrics are sparse
			EndTime:   timestamppb.New(now),
		},
		Aggregation: &monitoringpb.Aggregation{
			AlignmentPeriod:  durationpb.New(24 * time.Hour),
			PerSeriesAligner: monitoringpb.Aggregation_ALIGN_MEAN,
		},
		View: monitoringpb.ListTimeSeriesRequest_HEADERS,
	}

	it := a.metric.ListTimeSeries(ctx, req)
	seen := map[string]bool{}
	var datasets []string
	for scanned := 0; ; scanned++ {
		ts, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		if scanned >= maxUnpagedInventoryItems {
			return datasets, errInventoryLimitReached
		}
		if ts.Resource == nil {
			continue
		}
		ds := ts.Resource.Labels["dataset_id"]
		if ds == "" || seen[ds] {
			continue
		}
		seen[ds] = true
		datasets = append(datasets, ds)
	}
	return datasets, nil
}

// ---------------------------------------------------------------------------
// Pure scoring functions (no I/O — fully unit-testable)
// ---------------------------------------------------------------------------

// calculateAura computes the composite AuraReport from raw signals.
func calculateAura(kind models.ResourceKind, name, region string, signals []models.AuraHealthSignal) models.AuraReport {
	expected := expectedAuraSignals(kind)
	observedSignals := make([]models.AuraHealthSignal, 0, len(signals))
	warnings := make([]string, 0)
	observedCore := 0
	expectedSet := make(map[string]bool, len(expected))
	for _, signalName := range expected {
		expectedSet[signalName] = true
	}
	for _, signal := range signals {
		if signalObserved(signal) {
			observedSignals = append(observedSignals, signal)
			if expectedSet[signal.Name] {
				observedCore++
			}
			continue
		}
		if expectedSet[signal.Name] {
			warnings = append(warnings, fmt.Sprintf("%s is %s: %s", signal.Name, signal.Availability, signal.Message))
		}
	}
	coverageStatus := "complete"
	if observedCore == 0 && len(expected) > 0 {
		coverageStatus = "unavailable"
	} else if observedCore < len(expected) {
		coverageStatus = "partial"
	}
	healthScore, efficiencyScore := weightedScores(kind, observedSignals)
	score := int(math.Round(float64(healthScore)*0.6 + float64(efficiencyScore)*0.4))
	band := auraBand(score)
	label := auraLabel(score, observedSignals, kind)
	reasons := buildReasons(kind, observedSignals)
	if coverageStatus == "partial" {
		reasons = append([]string{fmt.Sprintf("Aura score uses %d of %d expected signals; review coverage warnings before acting", observedCore, len(expected))}, reasons...)
	}
	display := buildDisplay(kind, name, score, label, reasons)
	if coverageStatus == "partial" {
		display += fmt.Sprintf(" [PARTIAL: %d/%d signals]", observedCore, len(expected))
	}
	if coverageStatus == "unavailable" {
		healthScore, efficiencyScore, score = 0, 0, 0
		band = models.AuraBandUnavailable
		display = fmt.Sprintf("⚪ %s: %s | Aura: N/A (telemetry unavailable)", kindLabel(kind), name)
		reasons = []string{"No expected health signals were observed; do not interpret missing telemetry as a healthy zero."}
	}

	return models.AuraReport{
		ResourceKind:    kind,
		ResourceName:    name,
		Region:          region,
		Score:           score,
		Band:            band,
		Display:         display,
		HealthScore:     healthScore,
		EfficiencyScore: efficiencyScore,
		HealthSignals:   signals,
		Reasons:         reasons,
		CoverageStatus:  coverageStatus,
		SignalsObserved: observedCore,
		SignalsExpected: len(expected),
		Warnings:        warnings,
	}
}

// weightedScores returns (healthScore, efficiencyScore) for the given resource kind.
func weightedScores(kind models.ResourceKind, signals []models.AuraHealthSignal) (int, int) {
	byName := signalMap(signals)

	switch kind {
	case models.ResourceKindCloudRun:
		health := weightedAvailableScore(byName, map[string]float64{"error_rate": 0.5, "cpu_util": 0.3, "latency_p99": 0.2})

		// Efficiency: start with metric-based estimate, then let Recommender override.
		eff := 90
		cpuVal := signalValue(signals, "cpu_util")
		totalReq := signalValue(signals, "request_count_total")
		if hasSignal(signals, "cpu_util") && hasSignal(signals, "request_count_total") && cpuVal < 0.05 && totalReq > 0 {
			eff = 40
		}
		eff = applyRecommenderEfficiency(byName, eff)
		return clamp(health), clamp(eff)

	case models.ResourceKindCloudSQL:
		health := weightedAvailableScore(byName, map[string]float64{"cpu_util": 0.4, "memory_util": 0.3, "disk_util": 0.3})

		eff := 90
		if hasSignal(signals, "cpu_util") && signalValue(signals, "cpu_util") < 0.10 {
			eff = 30
		} else if hasSignal(signals, "disk_util") && signalValue(signals, "disk_util") > 0.80 {
			eff = 55
		}
		eff = applyRecommenderEfficiency(byName, eff)
		return clamp(health), clamp(eff)

	case models.ResourceKindBigQuery:
		health := weightedAvailableScore(byName, map[string]float64{"job_failure_rate": 0.6, "slot_utilization": 0.2, "storage_bytes": 0.2})

		eff := 90
		if hasSignal(signals, "slot_utilization") && signalValue(signals, "slot_utilization") < 10 { // < 10 slots ≈ idle
			eff = 50
		}
		eff = applyRecommenderEfficiency(byName, eff)
		return clamp(health), clamp(eff)

	case models.ResourceKindGKE:
		var health int
		if driftScore, ok := byName["version_drift"]; ok {
			_ = driftScore
			health = weightedAvailableScore(byName, map[string]float64{"node_cpu_util": 0.25, "node_mem_util": 0.25, "pod_restart_rate": 0.20, "control_plane_health": 0.20, "version_drift": 0.10})
		} else {
			health = weightedAvailableScore(byName, map[string]float64{"node_cpu_util": 0.35, "node_mem_util": 0.35, "pod_restart_rate": 0.20, "control_plane_health": 0.10})
		}

		eff := 90
		if hasSignal(signals, "node_cpu_util") && hasSignal(signals, "node_mem_util") &&
			signalValue(signals, "node_cpu_util") < 0.10 && signalValue(signals, "node_mem_util") < 0.10 {
			eff = 40 // cluster is idle / over-provisioned
		}
		if _, ok := byName["autoscaling_disabled"]; ok {
			eff = min(eff, 65)
		}
		if _, ok := byName["nodepool_at_max"]; ok {
			eff = min(eff, 75)
		}
		return clamp(health), clamp(eff)

	case models.ResourceKindGCS:
		health := weightedAvailableScore(byName, map[string]float64{"public_access_prevention": 0.45, "uniform_bucket_level_access": 0.35, "versioning": 0.20})
		eff := weightedAvailableScore(byName, map[string]float64{"lifecycle_policy": 0.60, "storage_class_fit": 0.40})
		return clamp(health), clamp(eff)
	}

	return 100, 100
}

func weightedAvailableScore(scores map[string]int, weights map[string]float64) int {
	weighted, totalWeight := 0.0, 0.0
	for name, weight := range weights {
		score, ok := scores[name]
		if !ok {
			continue
		}
		weighted += float64(score) * weight
		totalWeight += weight
	}
	if totalWeight == 0 {
		return 0
	}
	return int(math.Round(weighted / totalWeight))
}

// applyRecommenderEfficiency overrides the metric-based efficiency score when
// an active Recommender recommendation is present. Idle beats overprovisioned
// because idle means zero utilisation (stronger signal).
func applyRecommenderEfficiency(byName map[string]int, eff int) int {
	if _, ok := byName["recommender_idle"]; ok {
		return min(eff, 20)
	}
	if _, ok := byName["recommender_overprovisioned"]; ok {
		return min(eff, 45)
	}
	return eff
}

// Signal score tables per resource kind.

func cloudRunSignalScore(name string, value float64) (int, string) {
	switch name {
	case "error_rate":
		switch {
		case value == 0:
			return 100, "OK"
		case value < 0.01:
			return 85, "OK"
		case value < 0.03:
			return 60, "Warning"
		case value < 0.05:
			return 30, "Warning"
		default:
			return 0, "Critical"
		}
	case "cpu_util":
		switch {
		case value < 0.05:
			return 55, "Warning" // over-provisioned
		case value < 0.50:
			return 100, "OK"
		case value < 0.80:
			return 80, "OK"
		case value < 0.90:
			return 50, "Warning"
		default:
			return 20, "Critical"
		}
	case "latency_p99": // value in milliseconds
		switch {
		case value < 200:
			return 100, "OK"
		case value < 500:
			return 80, "OK"
		case value < 1000:
			return 60, "Warning"
		case value < 3000:
			return 30, "Warning"
		default:
			return 0, "Critical"
		}
	}
	return 100, "OK"
}

func sqlSignalScore(value float64) (int, string) {
	// Symmetric thresholds for cpu/memory/disk.
	switch {
	case value < 0.10:
		return 60, "Warning" // idle / underutilized
	case value < 0.70:
		return 100, "OK"
	case value < 0.85:
		return 75, "OK"
	case value < 0.95:
		return 40, "Warning"
	default:
		return 0, "Critical"
	}
}

func bqSignalScore(name string, value float64) (int, string) {
	switch name {
	case "job_failure_rate":
		switch {
		case value == 0:
			return 100, "OK"
		case value < 0.02:
			return 80, "OK"
		case value < 0.05:
			return 55, "Warning"
		case value < 0.10:
			return 30, "Warning"
		default:
			return 0, "Critical"
		}
	case "slot_utilization":
		// Slots allocated: < 10 → idle, healthy range varies by project quota.
		switch {
		case value < 10:
			return 60, "Warning"
		case value < 500:
			return 100, "OK"
		default:
			return 80, "OK" // high but not alarming
		}
	case "storage_bytes":
		const gb = 1 << 30
		switch {
		case value < 10*gb:
			return 100, "OK"
		case value < 100*gb:
			return 85, "OK"
		case value < 1024*gb: // 1 TB
			return 65, "Warning"
		default:
			return 40, "Warning" // large storage = cost concern
		}
	}
	return 100, "OK"
}

// Band and display helpers.

func auraBand(score int) models.AuraBand {
	switch {
	case score >= 80:
		return models.AuraBandGreen
	case score >= 50:
		return models.AuraBandYellow
	default:
		return models.AuraBandRed
	}
}

func auraEmoji(band models.AuraBand) string {
	switch band {
	case models.AuraBandGreen:
		return "🟢"
	case models.AuraBandYellow:
		return "🟡"
	case models.AuraBandUnavailable:
		return "⚪"
	default:
		return "🔴"
	}
}

func auraLabel(score int, signals []models.AuraHealthSignal, kind models.ResourceKind) string {
	band := auraBand(score)

	// Recommender signals take precedence — they carry authoritative GCP insight.
	if hasSignal(signals, "recommender_idle") {
		return "Idle Resource (GCP Recommender)"
	}
	if hasSignal(signals, "recommender_overprovisioned") {
		return "Over-provisioned (GCP Recommender)"
	}

	// Scan for Critical health signals.
	for _, s := range signals {
		if s.Label == "Critical" {
			return "Critical: " + s.Name
		}
	}

	// GCS-specific labels.
	if kind == models.ResourceKindGCS {
		switch band {
		case models.AuraBandGreen:
			return "Secure & Optimized"
		case models.AuraBandYellow:
			return "Review Needed"
		default:
			return "Security Risk"
		}
	}

	// GKE-specific labels.
	if kind == models.ResourceKindGKE {
		if controlPlane, ok := signalValueObserved(signals, "control_plane_health"); ok && controlPlane < 0.5 {
			return "Control Plane Error"
		}
		if hasSignal(signals, "pod_restart_rate") {
			if s := signalByName(signals, "pod_restart_rate"); s != nil && s.Label == "Critical" {
				return "Pod Instability Detected"
			}
		}
		switch band {
		case models.AuraBandGreen:
			cpu, cpuOK := signalValueObserved(signals, "node_cpu_util")
			mem, memOK := signalValueObserved(signals, "node_mem_util")
			if cpuOK && memOK && cpu < 0.10 && mem < 0.10 {
				return "Healthy, Over-provisioned"
			}
			return "Healthy Cluster"
		case models.AuraBandYellow:
			cpu, cpuOK := signalValueObserved(signals, "node_cpu_util")
			mem, memOK := signalValueObserved(signals, "node_mem_util")
			if cpuOK && memOK && cpu < 0.10 && mem < 0.10 {
				return "Idle Cluster, Consider Downsize"
			}
			return "Cluster Under Pressure"
		default:
			return "Cluster Stressed"
		}
	}

	switch band {
	case models.AuraBandGreen:
		if cpu, ok := signalValueObserved(signals, "cpu_util"); kind == models.ResourceKindCloudRun && ok && cpu < 0.05 {
			return "Healthy, Over-provisioned"
		}
		return "Healthy & Scaled"
	case models.AuraBandYellow:
		if cpu, ok := signalValueObserved(signals, "cpu_util"); kind == models.ResourceKindCloudRun && ok && cpu < 0.05 {
			return "Healthy, Over-provisioned"
		}
		if cpu, ok := signalValueObserved(signals, "cpu_util"); kind == models.ResourceKindCloudSQL && ok && cpu < 0.10 {
			return "Idle, Consider Downsize"
		}
		if disk, ok := signalValueObserved(signals, "disk_util"); kind == models.ResourceKindCloudSQL && ok && disk > 0.80 {
			return "Healthy, but High Disk Cost"
		}
		return "Degraded"
	default:
		return "Critical"
	}
}

func buildDisplay(kind models.ResourceKind, name string, score int, label string, reasons []string) string {
	band := auraBand(score)
	display := fmt.Sprintf("%s %s: %s | Aura: %d (%s)", auraEmoji(band), kindLabel(kind), name, score, label)
	if band != models.AuraBandGreen && len(reasons) > 0 {
		display += " — " + shortenReason(reasons[0])
	}
	return display
}

// shortenReason extracts a compact diagnostic snippet from a full reason string.
// For Recommender reasons it surfaces the dollar savings; for signal reasons it
// keeps only the measurement before the action-advice separator.
func shortenReason(r string) string {
	if strings.HasPrefix(r, "GCP Recommender: resource is idle") {
		if i := strings.Index(r, "estimated $"); i != -1 {
			rest := r[i+len("estimated $"):]
			if j := strings.Index(rest, "/mo"); j != -1 {
				return "est. $" + rest[:j] + "/mo"
			}
		}
		return "idle resource"
	}
	if strings.HasPrefix(r, "GCP Recommender: resource is over-provisioned") {
		if i := strings.Index(r, "estimated $"); i != -1 {
			rest := r[i+len("estimated $"):]
			if j := strings.Index(rest, "/mo"); j != -1 {
				return "est. $" + rest[:j] + "/mo savings"
			}
		}
		return "over-provisioned"
	}
	// Generic: keep only the diagnostic measurement, strip action advice after " — ".
	if i := strings.Index(r, " — "); i != -1 {
		r = r[:i]
	}
	if i := strings.Index(r, "; "); i != -1 {
		r = r[:i]
	}
	if len(r) > 55 {
		r = r[:52] + "..."
	}
	return r
}

func buildReasons(kind models.ResourceKind, signals []models.AuraHealthSignal) []string {
	var reasons []string

	// Recommender insights come first — they are the most actionable and cost-specific.
	for _, s := range signals {
		switch s.Name {
		case "recommender_idle":
			if s.Value > 0 {
				reasons = append(reasons, fmt.Sprintf("GCP Recommender: resource is idle — estimated $%.2f/mo savings if deleted or stopped", s.Value))
			} else {
				reasons = append(reasons, "GCP Recommender: resource is idle — consider deleting or stopping it")
			}
		case "recommender_overprovisioned":
			if s.Value > 0 {
				reasons = append(reasons, fmt.Sprintf("GCP Recommender: resource is over-provisioned — estimated $%.2f/mo savings by right-sizing", s.Value))
			} else {
				reasons = append(reasons, "GCP Recommender: resource is over-provisioned — consider right-sizing")
			}
		}
	}

	switch kind {
	case models.ResourceKindCloudRun:
		if v, ok := signalValueObserved(signals, "error_rate"); ok && v >= 0.05 {
			reasons = append(reasons, fmt.Sprintf("Error rate at %.1f%% — investigate recent deployments or upstream dependencies", v*100))
		} else if ok && v >= 0.01 {
			reasons = append(reasons, fmt.Sprintf("Error rate elevated at %.1f%% — monitor for trends", v*100))
		}
		if v, ok := signalValueObserved(signals, "cpu_util"); ok {
			requestCount, requestCountOK := signalValueObserved(signals, "request_count_total")
			if v < 0.05 && requestCountOK && requestCount > 0 {
				reasons = append(reasons, fmt.Sprintf("CPU at %.0f%% — consider min-instances=0 or a smaller instance class to reduce cost", v*100))
			} else if v >= 0.90 {
				reasons = append(reasons, fmt.Sprintf("CPU at %.0f%% — service is CPU-saturated; scale up or optimise hot paths", v*100))
			}
		}
		if v, ok := signalValueObserved(signals, "latency_p99"); ok && v >= 3000 {
			reasons = append(reasons, fmt.Sprintf("p99 latency at %.0fms — investigate slow requests or cold starts", v))
		} else if ok && v >= 1000 {
			reasons = append(reasons, fmt.Sprintf("p99 latency at %.0fms — consider caching or startup optimisation", v))
		}

	case models.ResourceKindCloudSQL:
		if v, ok := signalValueObserved(signals, "cpu_util"); ok && v < 0.10 {
			reasons = append(reasons, fmt.Sprintf("CPU at %.0f%% — instance may be idle; consider a smaller machine type", v*100))
		} else if ok && v >= 0.95 {
			reasons = append(reasons, fmt.Sprintf("CPU at %.0f%% — database is CPU-saturated; optimise queries or upgrade tier", v*100))
		}
		if v, ok := signalValueObserved(signals, "disk_util"); ok && v >= 0.80 {
			reasons = append(reasons, fmt.Sprintf("Disk at %.0f%% — increase storage capacity or enable auto-storage-increase", v*100))
		}
		if v, ok := signalValueObserved(signals, "memory_util"); ok && v >= 0.90 {
			reasons = append(reasons, fmt.Sprintf("Memory at %.0f%% — consider increasing RAM or tuning buffer pool", v*100))
		}

	case models.ResourceKindBigQuery:
		if v, ok := signalValueObserved(signals, "job_failure_rate"); ok && v >= 0.05 {
			reasons = append(reasons, fmt.Sprintf("Job failure rate at %.1f%% — review INFORMATION_SCHEMA.JOBS for error details", v*100))
		}
		if v, ok := signalValueObserved(signals, "slot_utilization"); ok && v < 10 {
			reasons = append(reasons, "Slot utilization near zero — consider on-demand pricing if jobs are infrequent")
		}
		if v, ok := signalValueObserved(signals, "storage_bytes"); ok && v > 1<<40 { // > 1 TB
			reasons = append(reasons, fmt.Sprintf("Storage at %.1f TB — review table expiration policies and partitioning strategy", v/float64(1<<40)))
		}

	case models.ResourceKindGKE:
		if controlPlane, ok := signalValueObserved(signals, "control_plane_health"); ok && controlPlane < 0.5 {
			reasons = append(reasons, "Control plane is in an error or degraded state — check GKE console for active conditions")
		} else if ok && controlPlane < 1.0 {
			reasons = append(reasons, "Control plane is reconciling — avoid cluster mutations until it returns to RUNNING")
		}
		if v, ok := signalValueObserved(signals, "pod_restart_rate"); ok && v >= 0.005 {
			reasons = append(reasons, fmt.Sprintf("Container restart rate at %.4f/s — investigate crashing pods with `kubectl get pods --all-namespaces`", v))
		} else if ok && v > 0 {
			reasons = append(reasons, fmt.Sprintf("Container restart rate at %.4f/s — monitor for increasing instability", v))
		}
		cpu, cpuOK := signalValueObserved(signals, "node_cpu_util")
		mem, memOK := signalValueObserved(signals, "node_mem_util")
		if cpuOK && cpu >= 0.90 {
			reasons = append(reasons, fmt.Sprintf("Node CPU at %.0f%% — cluster is CPU-saturated; add node pool capacity or upgrade machine type", cpu*100))
		}
		if memOK && mem >= 0.90 {
			reasons = append(reasons, fmt.Sprintf("Node memory at %.0f%% — cluster is memory-saturated; add nodes or use larger machine type", mem*100))
		}
		if cpuOK && memOK && cpu < 0.10 && mem < 0.10 {
			reasons = append(reasons, fmt.Sprintf("Node CPU %.0f%% and memory %.0f%% — cluster is idle; consider scaling down node pools to reduce cost", cpu*100, mem*100))
		}
		if hasSignal(signals, "autoscaling_disabled") {
			reasons = append(reasons, "One or more node pools have autoscaling disabled — enable cluster autoscaler to right-size node capacity automatically")
		}
		if hasSignal(signals, "nodepool_at_max") {
			reasons = append(reasons, "A node pool is at its maximum autoscaling capacity — increase max node count or add a new pool to absorb demand")
		}
		if driftS := signalByName(signals, "version_drift"); driftS != nil && driftS.Label != "OK" {
			reasons = append(reasons, "Cluster master version lags the channel's latest release — upgrade to reduce security exposure and access new features")
		}

	case models.ResourceKindGCS:
		if v, ok := signalValueObserved(signals, "public_access_prevention"); ok && v < 1.0 {
			reasons = append(reasons, "Public access prevention is not enforced — set publicAccessPrevention to 'enforced' to block all public ACLs")
		}
		if v, ok := signalValueObserved(signals, "uniform_bucket_level_access"); ok && v < 1.0 {
			reasons = append(reasons, "Uniform bucket-level access is disabled — legacy ACLs may bypass IAM policies; enable UBLA for uniform access control")
		}
		if v, ok := signalValueObserved(signals, "lifecycle_policy"); ok && v == 0 {
			reasons = append(reasons, "No lifecycle management rules — add transition rules to NEARLINE/COLDLINE/ARCHIVE to reduce long-term storage costs")
		}
		if v, ok := signalValueObserved(signals, "versioning"); ok && v < 1.0 {
			reasons = append(reasons, "Object versioning is disabled — enable it to protect against accidental deletes and overwrites")
		}
	}

	if len(reasons) == 0 {
		reasons = []string{"All observed signals nominal — no immediate action required"}
	}
	return reasons
}

// ---------------------------------------------------------------------------
// Utility
// ---------------------------------------------------------------------------

func kindLabel(k models.ResourceKind) string {
	switch k {
	case models.ResourceKindCloudRun:
		return "Cloud Run"
	case models.ResourceKindCloudSQL:
		return "Cloud SQL"
	case models.ResourceKindBigQuery:
		return "BigQuery"
	case models.ResourceKindGKE:
		return "GKE Cluster"
	case models.ResourceKindGCS:
		return "GCS Bucket"
	default:
		return string(k)
	}
}

func signalMap(signals []models.AuraHealthSignal) map[string]int {
	m := make(map[string]int, len(signals))
	for _, s := range signals {
		m[s.Name] = s.Score
	}
	return m
}

func signalValue(signals []models.AuraHealthSignal, name string) float64 {
	for _, s := range signals {
		if s.Name == name {
			return s.Value
		}
	}
	return 0
}

func signalValueObserved(signals []models.AuraHealthSignal, name string) (float64, bool) {
	for _, signal := range signals {
		if signal.Name == name && signalObserved(signal) {
			return signal.Value, true
		}
	}
	return 0, false
}

func hasSignal(signals []models.AuraHealthSignal, name string) bool {
	for _, s := range signals {
		if s.Name == name {
			return true
		}
	}
	return false
}

func signalByName(signals []models.AuraHealthSignal, name string) *models.AuraHealthSignal {
	for i := range signals {
		if signals[i].Name == name {
			return &signals[i]
		}
	}
	return nil
}

func clamp(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

func boolToFloat(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}

// ---------------------------------------------------------------------------
// GCS scoring
// ---------------------------------------------------------------------------

// fetchGCSSignals reuses the existing GetBucketMetadata adapter method to build
// the signal slice for GCS Aura scoring. No new SDK code required.
func (a *gcpAdapter) fetchGCSSignals(ctx context.Context, projectID, bucketName string) (
	[]models.AuraHealthSignal, models.GCSAuraDetails, error,
) {
	meta, err := a.GetBucketMetadata(ctx, models.GetBucketMetadataRequest{
		ProjectID:  projectID,
		BucketName: bucketName,
	})
	if err != nil {
		return nil, models.GCSAuraDetails{}, err
	}

	details := models.GCSAuraDetails{
		UniformBucketLevelAccess: meta.UniformBucketLevelAccess,
		PublicAccessPrevention:   meta.PublicAccessPrevention,
		VersioningEnabled:        meta.VersioningEnabled,
		LifecycleRuleCount:       meta.LifecycleRuleCount,
		StorageClass:             meta.StorageClass,
	}

	papVal := boolToFloat(meta.PublicAccessPrevention == "enforced")
	ublaVal := boolToFloat(meta.UniformBucketLevelAccess)
	verVal := boolToFloat(meta.VersioningEnabled)
	lcVal := float64(meta.LifecycleRuleCount)

	papScore, papLabel := gcsSignalScore("public_access_prevention", papVal)
	ublaScore, ublaLabel := gcsSignalScore("uniform_bucket_level_access", ublaVal)
	verScore, verLabel := gcsSignalScore("versioning", verVal)
	lcScore, lcLabel := gcsSignalScore("lifecycle_policy", lcVal)
	clsScore, clsLabel := gcsStorageClassScore(meta.StorageClass, meta.LifecycleRuleCount > 0)

	signals := []models.AuraHealthSignal{
		{Name: "public_access_prevention", Value: papVal, Score: papScore, Label: papLabel},
		{Name: "uniform_bucket_level_access", Value: ublaVal, Score: ublaScore, Label: ublaLabel},
		{Name: "versioning", Value: verVal, Score: verScore, Label: verLabel},
		{Name: "lifecycle_policy", Value: lcVal, Score: lcScore, Label: lcLabel},
		{Name: "storage_class_fit", Value: 0, Score: clsScore, Label: clsLabel},
	}
	return signals, details, nil
}

// GetGCSAuraScore returns a security- and cost-focused Aura Score for a single GCS bucket.
func (a *gcpAdapter) GetGCSAuraScore(ctx context.Context, req models.GetGCSAuraScoreRequest) (models.GCSAuraReport, error) {
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	if err := validateAuraRequest(models.GetAuraScoreRequest{
		ProjectID: req.ProjectID, ResourceKind: models.ResourceKindGCS, ResourceName: req.BucketName,
	}); err != nil {
		return models.GCSAuraReport{}, err
	}
	if err := a.rateWait(ctx, "aura.GetGCSAuraScore"); err != nil {
		return models.GCSAuraReport{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	signals, details, err := a.fetchGCSSignals(ctx, req.ProjectID, req.BucketName)
	if err != nil {
		return models.GCSAuraReport{}, wrapGCPError("aura.GetGCSAuraScore", err)
	}
	base := calculateAura(models.ResourceKindGCS, req.BucketName, "", signals)
	posture := gcsSecurityPosture(details.UniformBucketLevelAccess, details.PublicAccessPrevention)
	return models.GCSAuraReport{AuraReport: base, SecurityPosture: posture, Details: details}, nil
}

func gcsSignalScore(name string, value float64) (int, string) {
	switch name {
	case "public_access_prevention":
		if value == 1.0 {
			return 100, "OK"
		}
		return 15, "Critical"
	case "uniform_bucket_level_access":
		if value == 1.0 {
			return 100, "OK"
		}
		return 30, "Warning"
	case "versioning":
		if value == 1.0 {
			return 100, "OK"
		}
		return 50, "Warning"
	case "lifecycle_policy":
		if value > 0 {
			return 100, "OK"
		}
		return 25, "Warning"
	}
	return 100, "OK"
}

func gcsStorageClassScore(class string, hasLifecycle bool) (int, string) {
	switch class {
	case "NEARLINE", "COLDLINE", "ARCHIVE":
		return 100, "OK" // explicitly cost-optimised archival class
	case "STANDARD", "MULTI_REGIONAL", "REGIONAL":
		if hasLifecycle {
			return 90, "OK"
		}
		return 40, "Warning" // standard storage with no transition rules = likely over-paying
	}
	return 70, "OK" // unknown class — neutral
}

func gcsSecurityPosture(ubla bool, pap string) models.GCSSecurityPosture {
	enforced := pap == "enforced"
	switch {
	case enforced && ubla:
		return models.GCSPostureCompliant
	case !enforced && !ubla:
		return models.GCSPostureCritical
	default:
		return models.GCSPostureAtRisk
	}
}
