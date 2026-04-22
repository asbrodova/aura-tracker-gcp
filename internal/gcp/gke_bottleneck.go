package gcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (a *gcpAdapter) GetClusterBottlenecks(ctx context.Context, req models.GetClusterBottlenecksRequest) (models.ClusterBottleneckReport, error) {
	if err := a.rateWait(ctx, "gke.GetClusterBottlenecks"); err != nil {
		return models.ClusterBottleneckReport{}, err
	}

	if req.LookbackMinutes <= 0 {
		req.LookbackMinutes = 30
	}

	// Single 30-second timeout budget shared across all fan-out goroutines.
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	var (
		clusterDetails models.ClusterDetails
		cpuMetrics     models.GetMetricsResponse
		memMetrics     models.GetMetricsResponse
		logSummary     models.QueryRecentLogsResponse
	)

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		var err error
		clusterDetails, err = a.GetClusterDetails(gctx, models.GetClusterDetailsRequest{
			ProjectID:   req.ProjectID,
			Location:    req.Location,
			ClusterName: req.ClusterName,
		})
		if err != nil {
			return fmt.Errorf("cluster details: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		var err error
		cpuMetrics, err = a.GetMetrics(gctx, models.GetMetricsRequest{
			ProjectID:  req.ProjectID,
			MetricType: "kubernetes.io/node/cpu/allocatable_utilization",
			ResourceLabels: map[string]string{
				"cluster_name": req.ClusterName,
			},
			LookbackMinutes:        req.LookbackMinutes,
			AlignmentPeriodSeconds: 60,
		})
		if err != nil {
			return fmt.Errorf("cpu metrics: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		var err error
		memMetrics, err = a.GetMetrics(gctx, models.GetMetricsRequest{
			ProjectID:  req.ProjectID,
			MetricType: "kubernetes.io/node/memory/allocatable_utilization",
			ResourceLabels: map[string]string{
				"cluster_name": req.ClusterName,
			},
			LookbackMinutes:        req.LookbackMinutes,
			AlignmentPeriodSeconds: 60,
		})
		if err != nil {
			return fmt.Errorf("memory metrics: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		var err error
		logSummary, err = a.QueryRecentLogs(gctx, models.QueryRecentLogsRequest{
			ProjectID:       req.ProjectID,
			ResourceType:    "k8s_cluster",
			ResourceName:    req.ClusterName,
			MinSeverity:     "WARNING",
			MaxEntries:      100,
			LookbackMinutes: req.LookbackMinutes,
		})
		if err != nil {
			return fmt.Errorf("logs: %w", err)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return models.ClusterBottleneckReport{}, wrapGCPError("gke.GetClusterBottlenecks", err)
	}

	_ = clusterDetails // used by aggregateBottlenecks for future extension
	return aggregateBottlenecks(req, cpuMetrics, memMetrics, logSummary), nil
}

// aggregateBottlenecks is a pure function with no I/O — it is fully unit-testable
// without any mocks.
func aggregateBottlenecks(
	req models.GetClusterBottlenecksRequest,
	cpu models.GetMetricsResponse,
	mem models.GetMetricsResponse,
	logs models.QueryRecentLogsResponse,
) models.ClusterBottleneckReport {
	const (
		cpuWarnThresh = 0.75
		cpuCritThresh = 0.90
		memWarnThresh = 0.80
		memCritThresh = 0.95
	)

	report := models.ClusterBottleneckReport{
		ProjectID:     req.ProjectID,
		ClusterName:   req.ClusterName,
		Location:      req.Location,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		CPUMetrics:    cpu.Points,
		MemoryMetrics: mem.Points,
		LogSummary: models.LogSummary{
			ErrorCount:   countBySeverity(logs.Entries, "ERROR"),
			WarningCount: countBySeverity(logs.Entries, "WARNING"),
			TopMessages:  topNMessages(logs.Entries, 5),
		},
	}

	severity := models.SeverityNone

	if peak := peakValue(cpu.Points); peak > cpuCritThresh {
		severity = maxSeverity(severity, models.SeverityCritical)
		report.Bottlenecks = append(report.Bottlenecks, models.ResourceBottleneck{
			Resource:    fmt.Sprintf("cluster/%s", req.ClusterName),
			MetricName:  "cpu_allocatable_utilization",
			PeakValue:   peak,
			Threshold:   cpuCritThresh,
			Description: fmt.Sprintf("CPU peaked at %.1f%%, exceeding critical threshold of %.0f%%", peak*100, cpuCritThresh*100),
		})
	} else if peak := peakValue(cpu.Points); peak > cpuWarnThresh {
		severity = maxSeverity(severity, models.SeverityMedium)
		report.Bottlenecks = append(report.Bottlenecks, models.ResourceBottleneck{
			Resource:    fmt.Sprintf("cluster/%s", req.ClusterName),
			MetricName:  "cpu_allocatable_utilization",
			PeakValue:   peak,
			Threshold:   cpuWarnThresh,
			Description: fmt.Sprintf("CPU peaked at %.1f%%, exceeding warning threshold of %.0f%%", peak*100, cpuWarnThresh*100),
		})
	}

	if peak := peakValue(mem.Points); peak > memCritThresh {
		severity = maxSeverity(severity, models.SeverityCritical)
		report.Bottlenecks = append(report.Bottlenecks, models.ResourceBottleneck{
			Resource:    fmt.Sprintf("cluster/%s", req.ClusterName),
			MetricName:  "memory_allocatable_utilization",
			PeakValue:   peak,
			Threshold:   memCritThresh,
			Description: fmt.Sprintf("Memory peaked at %.1f%%, exceeding critical threshold of %.0f%%", peak*100, memCritThresh*100),
		})
	} else if peak := peakValue(mem.Points); peak > memWarnThresh {
		severity = maxSeverity(severity, models.SeverityMedium)
		report.Bottlenecks = append(report.Bottlenecks, models.ResourceBottleneck{
			Resource:    fmt.Sprintf("cluster/%s", req.ClusterName),
			MetricName:  "memory_allocatable_utilization",
			PeakValue:   peak,
			Threshold:   memWarnThresh,
			Description: fmt.Sprintf("Memory peaked at %.1f%%, exceeding warning threshold of %.0f%%", peak*100, memWarnThresh*100),
		})
	}

	if report.LogSummary.ErrorCount > 50 {
		severity = maxSeverity(severity, models.SeverityHigh)
	} else if report.LogSummary.ErrorCount > 10 {
		severity = maxSeverity(severity, models.SeverityMedium)
	}

	report.Severity = severity
	report.Summary = buildSummary(report)
	return report
}

func peakValue(pts []models.MetricPoint) float64 {
	var peak float64
	for _, p := range pts {
		if p.Value > peak {
			peak = p.Value
		}
	}
	return peak
}

func countBySeverity(entries []models.LogEntry, severity string) int {
	n := 0
	for _, e := range entries {
		if strings.EqualFold(e.Severity, severity) {
			n++
		}
	}
	return n
}

func topNMessages(entries []models.LogEntry, n int) []string {
	freq := make(map[string]int, len(entries))
	for _, e := range entries {
		freq[e.Message]++
	}
	type kv struct {
		msg   string
		count int
	}
	ranked := make([]kv, 0, len(freq))
	for msg, count := range freq {
		ranked = append(ranked, kv{msg, count})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].count > ranked[j].count })
	result := make([]string, 0, n)
	for i, kv := range ranked {
		if i >= n {
			break
		}
		result = append(result, kv.msg)
	}
	return result
}

var severityOrder = map[models.BottleneckSeverity]int{
	models.SeverityNone:     0,
	models.SeverityLow:      1,
	models.SeverityMedium:   2,
	models.SeverityHigh:     3,
	models.SeverityCritical: 4,
}

func maxSeverity(a, b models.BottleneckSeverity) models.BottleneckSeverity {
	if severityOrder[b] > severityOrder[a] {
		return b
	}
	return a
}

func buildSummary(r models.ClusterBottleneckReport) string {
	if r.Severity == models.SeverityNone {
		return fmt.Sprintf("Cluster %q shows no bottlenecks in the last window.", r.ClusterName)
	}
	return fmt.Sprintf(
		"Cluster %q has %s severity issues: %d bottleneck(s) detected, %d errors and %d warnings in logs.",
		r.ClusterName, r.Severity, len(r.Bottlenecks), r.LogSummary.ErrorCount, r.LogSummary.WarningCount,
	)
}
