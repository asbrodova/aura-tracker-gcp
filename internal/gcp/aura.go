package gcp

import (
	"context"
	"fmt"
	"math"
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
)

const auraCacheTTL = 5 * time.Minute

// cacheKey builds the lookup key for auraCache.
func cacheKey(projectID string, kind models.ResourceKind, region, name string) string {
	return fmt.Sprintf("%s/%s/%s/%s", projectID, kind, region, name)
}

// GetAuraScore returns a composite health+efficiency score for a single named resource.
// Results are cached for auraCacheTTL to keep repeated MCP calls under 500 ms.
func (a *gcpAdapter) GetAuraScore(ctx context.Context, req models.GetAuraScoreRequest) (models.AuraReport, error) {
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
	var err error

	switch req.ResourceKind {
	case models.ResourceKindCloudRun:
		signals, err = a.fetchCloudRunSignals(ctx, req.ProjectID, req.ResourceName, req.Region)
	case models.ResourceKindCloudSQL:
		signals, err = a.fetchCloudSQLSignals(ctx, req.ProjectID, req.ResourceName, req.Region)
	case models.ResourceKindBigQuery:
		signals, err = a.fetchBigQuerySignals(ctx, req.ProjectID, req.ResourceName)
	case models.ResourceKindGKE:
		signals, err = a.fetchGKESignals(ctx, req.ProjectID, req.ResourceName, req.Region)
	default:
		return models.AuraReport{}, fmt.Errorf("aura.GetAuraScore: unsupported resource_kind %q", req.ResourceKind)
	}
	if err != nil {
		return models.AuraReport{}, wrapGCPError("aura.GetAuraScore", err)
	}

	report := calculateAura(req.ResourceKind, req.ResourceName, req.Region, signals)
	report.CachedAt = time.Now().UTC()
	a.auraCache.set(key, report)
	return report, nil
}

// GetProjectAuraSummary discovers all Cloud Run, Cloud SQL, and BigQuery resources in the
// project and returns their Aura Scores sorted worst-first.
func (a *gcpAdapter) GetProjectAuraSummary(ctx context.Context, req models.ProjectAuraSummaryRequest) (models.ProjectAuraSummaryResponse, error) {
	if err := a.rateWait(ctx, "aura.GetProjectAuraSummary"); err != nil {
		return models.ProjectAuraSummaryResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	// Discover resources concurrently.
	type resource struct {
		kind   models.ResourceKind
		name   string
		region string
	}
	var (
		mu        = new(interface{ Lock(); Unlock() })
		resources []resource
	)
	_ = mu // use a plain slice guarded by errgroup serialisation below

	crServices, err := a.ListServices(ctx, models.ListServicesRequest{ProjectID: req.ProjectID, Region: req.Region})
	if err != nil {
		return models.ProjectAuraSummaryResponse{}, wrapGCPError("aura.GetProjectAuraSummary", err)
	}
	for _, svc := range crServices.Services {
		resources = append(resources, resource{models.ResourceKindCloudRun, svc.Name, svc.Region})
	}

	sqlInstances, err := a.listSQLInstances(ctx, req.ProjectID)
	if err != nil {
		// Non-fatal: log and continue with whatever we have.
		a.log.WarnContext(ctx, "aura: could not list Cloud SQL instances", "err", err)
	}
	for _, inst := range sqlInstances {
		resources = append(resources, resource{models.ResourceKindCloudSQL, inst.name, inst.region})
	}

	bqDatasets, err := a.listBigQueryDatasets(ctx, req.ProjectID)
	if err != nil {
		a.log.WarnContext(ctx, "aura: could not list BigQuery datasets", "err", err)
	}
	for _, ds := range bqDatasets {
		resources = append(resources, resource{models.ResourceKindBigQuery, ds, ""})
	}

	gkeClusters, err := a.listGKEClusters(ctx, req.ProjectID)
	if err != nil {
		a.log.WarnContext(ctx, "aura: could not list GKE clusters", "err", err)
	}
	for _, c := range gkeClusters {
		resources = append(resources, resource{models.ResourceKindGKE, c.name, c.location})
	}

	// Fan-out: score every resource concurrently.
	reports := make([]models.AuraReport, len(resources))
	g, gctx := errgroup.WithContext(ctx)
	for i, r := range resources {
		g.Go(func() error {
			rep, err := a.GetAuraScore(gctx, models.GetAuraScoreRequest{
				ProjectID:    req.ProjectID,
				ResourceKind: r.kind,
				ResourceName: r.name,
				Region:       r.region,
			})
			if err != nil {
				// Degrade gracefully: emit a red placeholder so the LLM knows something is wrong.
				reports[i] = models.AuraReport{
					ResourceKind: r.kind,
					ResourceName: r.name,
					Region:       r.region,
					Score:        0,
					Band:         models.AuraBandRed,
					Display:      fmt.Sprintf("🔴 %s: %s | Aura: N/A (fetch error: %v)", kindLabel(r.kind), r.name, err),
					Reasons:      []string{fmt.Sprintf("Failed to fetch metrics: %v", err)},
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

	// Sort worst-first.
	sort.Slice(reports, func(i, j int) bool { return reports[i].Score < reports[j].Score })

	// Build summary block.
	var lines []string
	var critical, warning, healthy int
	for _, r := range reports {
		lines = append(lines, r.Display)
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
		ProjectID:     req.ProjectID,
		Resources:     reports,
		Summary:       strings.Join(lines, "\n"),
		TotalCount:    len(reports),
		CriticalCount: critical,
		WarningCount:  warning,
		HealthyCount:  healthy,
	}, nil
}

// ---------------------------------------------------------------------------
// Per-resource signal fetchers
// ---------------------------------------------------------------------------

func (a *gcpAdapter) fetchCloudRunSignals(ctx context.Context, projectID, name, region string) ([]models.AuraHealthSignal, error) {
	baseFilter := fmt.Sprintf(`resource.labels.service_name = "%s" AND resource.labels.location = "%s"`, name, region)

	var (
		errCount5xx float64
		totalCount  float64
		cpuUtil     float64
		latencyP99  float64
		recInsights []recommenderInsight
	)

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		v, err := a.fetchRateMetric(gctx, projectID,
			`run.googleapis.com/request_count`,
			baseFilter+` AND metric.labels.response_code_class = "5xx"`, 60)
		if err != nil {
			a.log.WarnContext(gctx, "aura: metric unavailable", "signal", "error_count_5xx", "err", err)
			return nil
		}
		errCount5xx = v
		return nil
	})
	g.Go(func() error {
		v, err := a.fetchRateMetric(gctx, projectID,
			`run.googleapis.com/request_count`,
			baseFilter, 60)
		if err != nil {
			a.log.WarnContext(gctx, "aura: metric unavailable", "signal", "request_count_total", "err", err)
			return nil
		}
		totalCount = v
		return nil
	})
	g.Go(func() error {
		// cpu/utilizations is a DELTA DISTRIBUTION metric; ALIGN_MEAN is rejected by the
		// Monitoring API. Use p50 (median) to get a representative utilization value.
		v, err := a.fetchPercentileMetric(gctx, projectID,
			`run.googleapis.com/container/cpu/utilizations`,
			baseFilter, 50, 60)
		if err != nil {
			a.log.WarnContext(gctx, "aura: metric unavailable", "signal", "cpu_util", "err", err)
			return nil
		}
		cpuUtil = v
		return nil
	})
	g.Go(func() error {
		v, err := a.fetchPercentileMetric(gctx, projectID,
			`run.googleapis.com/request_latencies`,
			baseFilter, 99, 60)
		if err != nil {
			a.log.WarnContext(gctx, "aura: metric unavailable", "signal", "latency_p99", "err", err)
			return nil
		}
		latencyP99 = v
		return nil
	})
	g.Go(func() error {
		ins, err := a.fetchRecommenderInsights(
			gctx, projectID, region,
			recommenderIDCloudRunIdle,
			"/services/"+name,
		)
		if err != nil {
			// Non-fatal: recommender permission not granted or API unavailable.
			a.log.WarnContext(gctx, "aura: recommender unavailable for Cloud Run", "service", name, "err", err)
			return nil
		}
		recInsights = ins
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	errorRate := 0.0
	if totalCount > 0 {
		errorRate = errCount5xx / totalCount
	}

	errScore, errLabel := cloudRunSignalScore("error_rate", errorRate)
	cpuScore, cpuLabel := cloudRunSignalScore("cpu_util", cpuUtil)
	latScore, latLabel := cloudRunSignalScore("latency_p99", latencyP99)

	signals := []models.AuraHealthSignal{
		{Name: "error_rate", Value: round4(errorRate), Score: errScore, Label: errLabel},
		{Name: "cpu_util", Value: round4(cpuUtil), Score: cpuScore, Label: cpuLabel},
		{Name: "latency_p99", Value: math.Round(latencyP99), Score: latScore, Label: latLabel},
		// total_count is kept for efficiency calculation via a synthetic signal
		{Name: "request_count_total", Value: round4(totalCount), Score: 100, Label: "info"},
	}
	for _, ins := range recInsights {
		signals = append(signals, recommenderSignal(ins))
	}
	return signals, nil
}

func (a *gcpAdapter) fetchCloudSQLSignals(ctx context.Context, projectID, instanceName, region string) ([]models.AuraHealthSignal, error) {
	// Cloud SQL instance IDs are formatted as "project:region:instance" in some APIs
	// but resource.labels.database_id uses the plain instance name.
	baseFilter := fmt.Sprintf(`resource.labels.database_id = "%s:%s"`, projectID, instanceName)

	var cpuUtil, memUtil, diskUtil float64
	var recInsights []recommenderInsight

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		v, err := a.fetchMeanMetric(gctx, projectID, `cloudsql.googleapis.com/database/cpu/utilization`, baseFilter, 60)
		if err != nil {
			return err
		}
		cpuUtil = v
		return nil
	})
	g.Go(func() error {
		v, err := a.fetchMeanMetric(gctx, projectID, `cloudsql.googleapis.com/database/memory/utilization`, baseFilter, 60)
		if err != nil {
			return err
		}
		memUtil = v
		return nil
	})
	g.Go(func() error {
		v, err := a.fetchMeanMetric(gctx, projectID, `cloudsql.googleapis.com/database/disk/utilization`, baseFilter, 60)
		if err != nil {
			return err
		}
		diskUtil = v
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
			a.log.WarnContext(gctx, "aura: recommender unavailable for Cloud SQL idle", "instance", instanceName, "err", err)
			return nil
		}
		recInsights = append(recInsights, idle...)
		return nil
	})
	g.Go(func() error {
		op, err := a.fetchRecommenderInsights(
			gctx, projectID, region,
			recommenderIDCloudSQLOverpro,
			"/instances/"+instanceName,
		)
		if err != nil {
			a.log.WarnContext(gctx, "aura: recommender unavailable for Cloud SQL overprovisioned", "instance", instanceName, "err", err)
			return nil
		}
		recInsights = append(recInsights, op...)
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	cpuScore, cpuLabel := sqlSignalScore(cpuUtil)
	memScore, memLabel := sqlSignalScore(memUtil)
	diskScore, diskLabel := sqlSignalScore(diskUtil)

	signals := []models.AuraHealthSignal{
		{Name: "cpu_util", Value: round4(cpuUtil), Score: cpuScore, Label: cpuLabel},
		{Name: "memory_util", Value: round4(memUtil), Score: memScore, Label: memLabel},
		{Name: "disk_util", Value: round4(diskUtil), Score: diskScore, Label: diskLabel},
	}
	for _, ins := range recInsights {
		signals = append(signals, recommenderSignal(ins))
	}
	return signals, nil
}

func (a *gcpAdapter) fetchBigQuerySignals(ctx context.Context, projectID, datasetID string) ([]models.AuraHealthSignal, error) {
	projectFilter := fmt.Sprintf(`resource.labels.project_id = "%s"`, projectID)

	var failedJobs, totalJobs, slotsAlloc, storageBytes float64

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		v, err := a.fetchMeanMetric(gctx, projectID,
			`bigquery.googleapis.com/job_count`,
			projectFilter+` AND metric.labels.status = "error"`, 60)
		if err != nil {
			return err
		}
		failedJobs = v
		return nil
	})
	g.Go(func() error {
		v, err := a.fetchMeanMetric(gctx, projectID,
			`bigquery.googleapis.com/job_count`,
			projectFilter, 60)
		if err != nil {
			return err
		}
		totalJobs = v
		return nil
	})
	g.Go(func() error {
		v, err := a.fetchMeanMetric(gctx, projectID,
			`bigquery.googleapis.com/slots/allocated_for_project`,
			projectFilter, 60)
		if err != nil {
			return err
		}
		slotsAlloc = v
		return nil
	})
	g.Go(func() error {
		dsFilter := fmt.Sprintf(`resource.labels.dataset_id = "%s"`, datasetID)
		v, err := a.fetchMeanMetric(gctx, projectID,
			`bigquery.googleapis.com/storage/billable_bytes_stored`,
			dsFilter, 60)
		if err != nil {
			return err
		}
		storageBytes = v
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	failRate := 0.0
	if totalJobs > 0 {
		failRate = failedJobs / totalJobs
	}

	failScore, failLabel := bqSignalScore("job_failure_rate", failRate)
	slotScore, slotLabel := bqSignalScore("slot_utilization", slotsAlloc)
	storScore, storLabel := bqSignalScore("storage_bytes", storageBytes)

	return []models.AuraHealthSignal{
		{Name: "job_failure_rate", Value: round4(failRate), Score: failScore, Label: failLabel},
		{Name: "slot_utilization", Value: round4(slotsAlloc), Score: slotScore, Label: slotLabel},
		{Name: "storage_bytes", Value: storageBytes, Score: storScore, Label: storLabel},
	}, nil
}

func (a *gcpAdapter) fetchGKESignals(ctx context.Context, projectID, clusterName, location string) ([]models.AuraHealthSignal, error) {
	clusterFilter := fmt.Sprintf(`resource.labels.cluster_name = "%s" AND resource.labels.location = "%s"`, clusterName, location)

	var nodeCPU, nodeMem, restartRate, ctrlHealth float64

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		v, err := a.fetchMeanMetric(gctx, projectID,
			`kubernetes.io/node/cpu/allocatable_utilization`,
			clusterFilter, 60)
		if err != nil {
			return err
		}
		nodeCPU = v
		return nil
	})
	g.Go(func() error {
		v, err := a.fetchMeanMetric(gctx, projectID,
			`kubernetes.io/node/memory/allocatable_utilization`,
			clusterFilter, 60)
		if err != nil {
			return err
		}
		nodeMem = v
		return nil
	})
	g.Go(func() error {
		v, err := a.fetchRateMetric(gctx, projectID,
			`kubernetes.io/container/restart_count`,
			clusterFilter, 60)
		if err != nil {
			return err
		}
		restartRate = v
		return nil
	})
	g.Go(func() error {
		if a.clusterMgr == nil {
			ctrlHealth = 1.0 // graceful: no client = assume healthy
			return nil
		}
		clusterPath := fmt.Sprintf("projects/%s/locations/%s/clusters/%s", projectID, location, clusterName)
		c, err := a.clusterMgr.GetCluster(gctx, &containerpb.GetClusterRequest{Name: clusterPath})
		if err != nil {
			// Non-fatal: treat as unknown health rather than failing the whole score.
			a.log.WarnContext(gctx, "aura: GKE control-plane health check failed", "cluster", clusterName, "err", err)
			ctrlHealth = 1.0
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

	if err := g.Wait(); err != nil {
		return nil, err
	}

	cpuScore, cpuLabel := gkeSignalScore("node_cpu_util", nodeCPU)
	memScore, memLabel := gkeSignalScore("node_mem_util", nodeMem)
	restartScore, restartLabel := gkeSignalScore("pod_restart_rate", restartRate)
	ctrlScore, ctrlLabel := gkeSignalScore("control_plane_health", ctrlHealth)

	return []models.AuraHealthSignal{
		{Name: "node_cpu_util", Value: round4(nodeCPU), Score: cpuScore, Label: cpuLabel},
		{Name: "node_mem_util", Value: round4(nodeMem), Score: memScore, Label: memLabel},
		{Name: "pod_restart_rate", Value: round4(restartRate), Score: restartScore, Label: restartLabel},
		{Name: "control_plane_health", Value: ctrlHealth, Score: ctrlScore, Label: ctrlLabel},
	}, nil
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

	fullFilter := fmt.Sprintf(`metric.type = "%s"`, metricType)
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
		return 0, nil // no data = metric not emitted = resource likely idle
	}
	if err != nil {
		return 0, err
	}
	if len(ts.Points) == 0 {
		return 0, nil
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
	for {
		ts, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
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
	for {
		ts, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
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
	healthScore, efficiencyScore := weightedScores(kind, signals)
	score := int(math.Round(float64(healthScore)*0.6 + float64(efficiencyScore)*0.4))
	band := auraBand(score)
	label := auraLabel(score, signals, kind)

	return models.AuraReport{
		ResourceKind:    kind,
		ResourceName:    name,
		Region:          region,
		Score:           score,
		Band:            band,
		Display:         buildDisplay(kind, name, score, label),
		HealthScore:     healthScore,
		EfficiencyScore: efficiencyScore,
		HealthSignals:   signals,
		Reasons:         buildReasons(kind, signals),
	}
}

// weightedScores returns (healthScore, efficiencyScore) for the given resource kind.
func weightedScores(kind models.ResourceKind, signals []models.AuraHealthSignal) (int, int) {
	byName := signalMap(signals)

	switch kind {
	case models.ResourceKindCloudRun:
		errScore := byName["error_rate"]
		cpuScore := byName["cpu_util"]
		latScore := byName["latency_p99"]
		health := int(math.Round(float64(errScore)*0.5 + float64(cpuScore)*0.3 + float64(latScore)*0.2))

		// Efficiency: start with metric-based estimate, then let Recommender override.
		eff := 90
		cpuVal := signalValue(signals, "cpu_util")
		totalReq := signalValue(signals, "request_count_total")
		if cpuVal < 0.05 && totalReq > 0 {
			eff = 40
		}
		eff = applyRecommenderEfficiency(byName, eff)
		return clamp(health), clamp(eff)

	case models.ResourceKindCloudSQL:
		cpuScore := byName["cpu_util"]
		memScore := byName["memory_util"]
		diskScore := byName["disk_util"]
		health := int(math.Round(float64(cpuScore)*0.4 + float64(memScore)*0.3 + float64(diskScore)*0.3))

		eff := 90
		if signalValue(signals, "cpu_util") < 0.10 {
			eff = 30
		} else if signalValue(signals, "disk_util") > 0.80 {
			eff = 55
		}
		eff = applyRecommenderEfficiency(byName, eff)
		return clamp(health), clamp(eff)

	case models.ResourceKindBigQuery:
		failScore := byName["job_failure_rate"]
		slotScore := byName["slot_utilization"]
		storScore := byName["storage_bytes"]
		health := int(math.Round(float64(failScore)*0.6 + float64(slotScore)*0.2 + float64(storScore)*0.2))

		eff := 90
		if signalValue(signals, "slot_utilization") < 10 { // < 10 slots ≈ idle
			eff = 50
		}
		eff = applyRecommenderEfficiency(byName, eff)
		return clamp(health), clamp(eff)

	case models.ResourceKindGKE:
		cpuScore := byName["node_cpu_util"]
		memScore := byName["node_mem_util"]
		restartScore := byName["pod_restart_rate"]
		ctrlScore := byName["control_plane_health"]
		health := int(math.Round(
			float64(cpuScore)*0.35 +
				float64(memScore)*0.35 +
				float64(restartScore)*0.20 +
				float64(ctrlScore)*0.10,
		))

		eff := 90
		if signalValue(signals, "node_cpu_util") < 0.10 && signalValue(signals, "node_mem_util") < 0.10 {
			eff = 40 // cluster is idle / over-provisioned
		}
		return clamp(health), clamp(eff)
	}

	return 100, 100
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

	// GKE-specific labels.
	if kind == models.ResourceKindGKE {
		if signalValue(signals, "control_plane_health") < 0.5 {
			return "Control Plane Error"
		}
		if hasSignal(signals, "pod_restart_rate") {
			if s := signalByName(signals, "pod_restart_rate"); s != nil && s.Label == "Critical" {
				return "Pod Instability Detected"
			}
		}
		switch band {
		case models.AuraBandGreen:
			if signalValue(signals, "node_cpu_util") < 0.10 && signalValue(signals, "node_mem_util") < 0.10 {
				return "Healthy, Over-provisioned"
			}
			return "Healthy Cluster"
		case models.AuraBandYellow:
			if signalValue(signals, "node_cpu_util") < 0.10 && signalValue(signals, "node_mem_util") < 0.10 {
				return "Idle Cluster, Consider Downsize"
			}
			return "Cluster Under Pressure"
		default:
			return "Cluster Stressed"
		}
	}

	switch band {
	case models.AuraBandGreen:
		if kind == models.ResourceKindCloudRun && signalValue(signals, "cpu_util") < 0.05 {
			return "Healthy, Over-provisioned"
		}
		return "Healthy & Scaled"
	case models.AuraBandYellow:
		if kind == models.ResourceKindCloudRun && signalValue(signals, "cpu_util") < 0.05 {
			return "Healthy, Over-provisioned"
		}
		if kind == models.ResourceKindCloudSQL && signalValue(signals, "cpu_util") < 0.10 {
			return "Idle, Consider Downsize"
		}
		if kind == models.ResourceKindCloudSQL && signalValue(signals, "disk_util") > 0.80 {
			return "Healthy, but High Disk Cost"
		}
		return "Degraded"
	default:
		return "Critical"
	}
}

func buildDisplay(kind models.ResourceKind, name string, score int, label string) string {
	band := auraBand(score)
	return fmt.Sprintf("%s %s: %s | Aura: %d (%s)", auraEmoji(band), kindLabel(kind), name, score, label)
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
		if v := signalValue(signals, "error_rate"); v >= 0.05 {
			reasons = append(reasons, fmt.Sprintf("Error rate at %.1f%% — investigate recent deployments or upstream dependencies", v*100))
		} else if v >= 0.01 {
			reasons = append(reasons, fmt.Sprintf("Error rate elevated at %.1f%% — monitor for trends", v*100))
		}
		if v := signalValue(signals, "cpu_util"); v < 0.05 && signalValue(signals, "request_count_total") > 0 {
			reasons = append(reasons, fmt.Sprintf("CPU at %.0f%% — consider min-instances=0 or a smaller instance class to reduce cost", v*100))
		} else if v >= 0.90 {
			reasons = append(reasons, fmt.Sprintf("CPU at %.0f%% — service is CPU-saturated; scale up or optimise hot paths", v*100))
		}
		if v := signalValue(signals, "latency_p99"); v >= 3000 {
			reasons = append(reasons, fmt.Sprintf("p99 latency at %.0fms — investigate slow requests or cold starts", v))
		} else if v >= 1000 {
			reasons = append(reasons, fmt.Sprintf("p99 latency at %.0fms — consider caching or startup optimisation", v))
		}

	case models.ResourceKindCloudSQL:
		if v := signalValue(signals, "cpu_util"); v < 0.10 {
			reasons = append(reasons, fmt.Sprintf("CPU at %.0f%% — instance may be idle; consider a smaller machine type", v*100))
		} else if v >= 0.95 {
			reasons = append(reasons, fmt.Sprintf("CPU at %.0f%% — database is CPU-saturated; optimise queries or upgrade tier", v*100))
		}
		if v := signalValue(signals, "disk_util"); v >= 0.80 {
			reasons = append(reasons, fmt.Sprintf("Disk at %.0f%% — increase storage capacity or enable auto-storage-increase", v*100))
		}
		if v := signalValue(signals, "memory_util"); v >= 0.90 {
			reasons = append(reasons, fmt.Sprintf("Memory at %.0f%% — consider increasing RAM or tuning buffer pool", v*100))
		}

	case models.ResourceKindBigQuery:
		if v := signalValue(signals, "job_failure_rate"); v >= 0.05 {
			reasons = append(reasons, fmt.Sprintf("Job failure rate at %.1f%% — review INFORMATION_SCHEMA.JOBS for error details", v*100))
		}
		if v := signalValue(signals, "slot_utilization"); v < 10 {
			reasons = append(reasons, "Slot utilization near zero — consider on-demand pricing if jobs are infrequent")
		}
		if v := signalValue(signals, "storage_bytes"); v > 1<<40 { // > 1 TB
			reasons = append(reasons, fmt.Sprintf("Storage at %.1f TB — review table expiration policies and partitioning strategy", v/float64(1<<40)))
		}

	case models.ResourceKindGKE:
		if signalValue(signals, "control_plane_health") < 0.5 {
			reasons = append(reasons, "Control plane is in an error or degraded state — check GKE console for active conditions")
		} else if signalValue(signals, "control_plane_health") < 1.0 {
			reasons = append(reasons, "Control plane is reconciling — avoid cluster mutations until it returns to RUNNING")
		}
		if v := signalValue(signals, "pod_restart_rate"); v >= 0.005 {
			reasons = append(reasons, fmt.Sprintf("Container restart rate at %.4f/s — investigate crashing pods with `kubectl get pods --all-namespaces`", v))
		} else if v > 0 {
			reasons = append(reasons, fmt.Sprintf("Container restart rate at %.4f/s — monitor for increasing instability", v))
		}
		cpu := signalValue(signals, "node_cpu_util")
		mem := signalValue(signals, "node_mem_util")
		if cpu >= 0.90 {
			reasons = append(reasons, fmt.Sprintf("Node CPU at %.0f%% — cluster is CPU-saturated; add node pool capacity or upgrade machine type", cpu*100))
		}
		if mem >= 0.90 {
			reasons = append(reasons, fmt.Sprintf("Node memory at %.0f%% — cluster is memory-saturated; add nodes or use larger machine type", mem*100))
		}
		if cpu < 0.10 && mem < 0.10 {
			reasons = append(reasons, fmt.Sprintf("Node CPU %.0f%% and memory %.0f%% — cluster is idle; consider scaling down node pools to reduce cost", cpu*100, mem*100))
		}
	}

	if len(reasons) == 0 {
		reasons = []string{"All signals nominal — no immediate action required"}
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
