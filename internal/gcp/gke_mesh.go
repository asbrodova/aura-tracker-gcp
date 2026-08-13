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
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

const maxMeshLookbackHours = 168

var istioMeshGroupByFields = []string{
	"metric.labels.source_workload_name",
	"metric.labels.source_workload_namespace",
	"metric.labels.destination_service_name",
	"metric.labels.destination_service_namespace",
}

func (a *gcpAdapter) GetGKEMeshTopology(ctx context.Context, req models.GetGKEMeshTopologyRequest) (models.GKEMeshTopologyResponse, error) {
	if err := a.rateWait(ctx, "gke.GetGKEMeshTopology"); err != nil {
		return models.GKEMeshTopologyResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	if req.LookbackHours <= 0 {
		if req.LookbackHours < 0 {
			return models.GKEMeshTopologyResponse{}, fmt.Errorf("gke.GetGKEMeshTopology: lookback_hours must be between 1 and %d", maxMeshLookbackHours)
		}
		req.LookbackHours = 24
	}
	if req.LookbackHours > maxMeshLookbackHours {
		return models.GKEMeshTopologyResponse{}, fmt.Errorf("gke.GetGKEMeshTopology: lookback_hours must be between 1 and %d", maxMeshLookbackHours)
	}
	if strings.TrimSpace(req.ClusterName) == "" {
		return models.GKEMeshTopologyResponse{}, fmt.Errorf("gke.GetGKEMeshTopology: cluster_name is required")
	}
	if strings.TrimSpace(req.Location) == "" {
		return models.GKEMeshTopologyResponse{}, fmt.Errorf("gke.GetGKEMeshTopology: location is required")
	}

	now := time.Now().UTC()
	start := now.Add(-time.Duration(req.LookbackHours) * time.Hour)

	edges, backend, warnings := a.meshEdges(ctx, req.ProjectID, req.Location, req.ClusterName, start, now)
	if edges == nil {
		edges = []models.GKEMeshEdge{}
	}

	return models.GKEMeshTopologyResponse{
		ClusterName: req.ClusterName,
		Location:    req.Location,
		Edges:       edges,
		Backend:     backend,
		Warnings:    warnings,
	}, nil
}

// meshEdges attempts Istio Cloud Monitoring metrics first, then falls back to
// log-based inference from Istio proxy access logs.
func (a *gcpAdapter) meshEdges(ctx context.Context, projectID, location, clusterName string, start, end time.Time) ([]models.GKEMeshEdge, string, []string) {
	edges, err := a.istioMeshEdges(ctx, projectID, location, clusterName, start, end)
	if err == nil && len(edges) > 0 {
		return edges, "istio_metrics", nil
	}

	var warnings []string
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("Istio metrics unavailable (%s) — falling back to log-based inference", err.Error()))
	} else {
		warnings = append(warnings, "no Istio request_count metrics found for this cluster (Anthos Service Mesh may not be enabled) — falling back to log-based inference")
	}

	edges, err = a.logBasedMeshEdges(ctx, projectID, location, clusterName, start, end)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("log-based inference also failed: %s", err.Error()))
		return []models.GKEMeshEdge{}, "none", warnings
	}
	if len(edges) == 0 {
		warnings = append(warnings, "no service-to-service traffic patterns found in logs for the lookback period")
		return []models.GKEMeshEdge{}, "log_based", warnings
	}
	return edges, "log_based", warnings
}

// istioMeshEdges queries the Istio server-side request_count metric from Cloud
// Monitoring, aggregating across the lookback window into per-(caller,callee)
// request-per-minute totals.
func (a *gcpAdapter) istioMeshEdges(ctx context.Context, projectID, location, clusterName string, start, end time.Time) ([]models.GKEMeshEdge, error) {
	duration := end.Sub(start)
	if duration <= 0 {
		duration = 24 * time.Hour
	}

	filter := buildIstioMeshFilter(location, clusterName)
	pbReq := &monitoringpb.ListTimeSeriesRequest{
		Name:   fmt.Sprintf("projects/%s", projectID),
		Filter: filter,
		Interval: &monitoringpb.TimeInterval{
			StartTime: timestamppb.New(start),
			EndTime:   timestamppb.New(end),
		},
		Aggregation: &monitoringpb.Aggregation{
			AlignmentPeriod:    durationpb.New(duration),
			PerSeriesAligner:   monitoringpb.Aggregation_ALIGN_RATE,
			CrossSeriesReducer: monitoringpb.Aggregation_REDUCE_SUM,
			GroupByFields:      istioMeshGroupByFields,
		},
		View: monitoringpb.ListTimeSeriesRequest_FULL,
	}

	it := a.metric.ListTimeSeries(ctx, pbReq)

	type edgeKey struct{ caller, callerNS, callee, calleeNS string }
	type rateSamples struct {
		total float64
		count int
	}
	edgeMap := make(map[edgeKey]rateSamples)

	for {
		ts, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			msg := err.Error()
			// Metric type not found → ASM not installed; treat as no data.
			if strings.Contains(msg, "does not exist") || strings.Contains(msg, "no time series") {
				return nil, nil
			}
			return nil, wrapGCPError("gke.istioMeshEdges", err)
		}

		if ts.Metric == nil {
			continue
		}
		lbl := ts.Metric.Labels
		src := lbl["source_workload_name"]
		srcNS := lbl["source_workload_namespace"]
		dst := lbl["destination_service_name"]
		dstNS := lbl["destination_service_namespace"]

		if src == "" || dst == "" || src == "unknown" || dst == "PassthroughCluster" {
			continue
		}

		key := edgeKey{src, srcNS, dst, dstNS}
		samples := edgeMap[key]
		for _, pt := range ts.Points {
			// ALIGN_RATE gives req/s. Average aligned rate samples across the
			// lookback before converting the result to requests/minute.
			samples.total += extractPointValue(pt)
			samples.count++
		}
		edgeMap[key] = samples
	}

	edges := make([]models.GKEMeshEdge, 0, len(edgeMap))
	for k, samples := range edgeMap {
		if samples.count == 0 {
			continue
		}
		edges = append(edges, models.GKEMeshEdge{
			Caller:            k.caller,
			CallerNamespace:   k.callerNS,
			Callee:            k.callee,
			CalleeNamespace:   k.calleeNS,
			RequestsPerMinute: alignedRatesToRequestsPerMinute(samples.total, samples.count),
		})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Caller != edges[j].Caller {
			return edges[i].Caller < edges[j].Caller
		}
		return edges[i].Callee < edges[j].Callee
	})
	return edges, nil
}

// logBasedMeshEdges scans Istio proxy access log entries in Cloud Logging for
// source_workload / destination_workload label pairs. This is the fallback when
// Anthos Service Mesh metrics are unavailable.
func (a *gcpAdapter) logBasedMeshEdges(ctx context.Context, projectID, location, clusterName string, start, end time.Time) ([]models.GKEMeshEdge, error) {
	filter := fmt.Sprintf(
		`resource.type="k8s_container"`+
			` AND resource.labels.cluster_name="%s"`+
			` AND resource.labels.location="%s"`+
			` AND resource.labels.container_name="istio-proxy"`+
			` AND timestamp>="%s" AND timestamp<="%s"`,
		escapeLoggingString(clusterName),
		escapeLoggingString(location),
		start.Format(time.RFC3339),
		end.Format(time.RFC3339),
	)

	it := a.logAdmin.Entries(ctx,
		logadmin.ProjectIDs([]string{projectID}),
		logadmin.Filter(filter),
		logadmin.NewestFirst(),
		logadmin.PageSize(500),
	)

	type edgeKey struct{ caller, callerNS, callee, calleeNS string }
	edgeMap := make(map[edgeKey]int)

	for {
		entry, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			if strings.Contains(err.Error(), "PermissionDenied") || strings.Contains(err.Error(), "403") {
				return nil, &PermissionDeniedError{Op: "gke.logBasedMeshEdges", Err: err}
			}
			return nil, wrapGCPError("gke.logBasedMeshEdges", err)
		}

		src := entry.Labels["source_workload"]
		srcNS := entry.Labels["source_workload_namespace"]
		dst := entry.Labels["destination_workload"]
		dstNS := entry.Labels["destination_workload_namespace"]

		if src == "" || dst == "" || src == "unknown" {
			continue
		}

		edgeMap[edgeKey{src, srcNS, dst, dstNS}]++
		if len(edgeMap) >= 200 {
			break
		}
	}

	edges := make([]models.GKEMeshEdge, 0, len(edgeMap))
	for k, count := range edgeMap {
		edges = append(edges, models.GKEMeshEdge{
			Caller:            k.caller,
			CallerNamespace:   k.callerNS,
			Callee:            k.callee,
			CalleeNamespace:   k.calleeNS,
			RequestsPerMinute: logCountToRequestsPerMinute(count, start, end),
		})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Caller != edges[j].Caller {
			return edges[i].Caller < edges[j].Caller
		}
		return edges[i].Callee < edges[j].Callee
	})
	return edges, nil
}

func buildIstioMeshFilter(location, clusterName string) string {
	return fmt.Sprintf(
		`metric.type="istio.io/service/server/request_count" AND resource.labels.cluster_name="%s" AND resource.labels.location="%s"`,
		escapeMonitoringString(clusterName),
		escapeMonitoringString(location),
	)
}

func alignedRatesToRequestsPerMinute(totalRate float64, sampleCount int) float64 {
	if sampleCount <= 0 {
		return 0
	}
	return (totalRate / float64(sampleCount)) * 60
}

func logCountToRequestsPerMinute(count int, start, end time.Time) float64 {
	minutes := end.Sub(start).Minutes()
	if minutes <= 0 {
		return 0
	}
	return float64(count) / minutes
}
