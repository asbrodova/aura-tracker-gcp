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

func (a *gcpAdapter) GetGKEMeshTopology(ctx context.Context, req models.GetGKEMeshTopologyRequest) (models.GKEMeshTopologyResponse, error) {
	if err := a.rateWait(ctx, "gke.GetGKEMeshTopology"); err != nil {
		return models.GKEMeshTopologyResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	if req.LookbackHours <= 0 {
		req.LookbackHours = 24
	}

	now := time.Now().UTC()
	start := now.Add(-time.Duration(req.LookbackHours) * time.Hour)

	edges, backend, warnings := a.meshEdges(ctx, req.ProjectID, req.ClusterName, start, now)
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
func (a *gcpAdapter) meshEdges(ctx context.Context, projectID, clusterName string, start, end time.Time) ([]models.GKEMeshEdge, string, []string) {
	edges, err := a.istioMeshEdges(ctx, projectID, clusterName, start, end)
	if err == nil && len(edges) > 0 {
		return edges, "istio_metrics", nil
	}

	var warnings []string
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("Istio metrics unavailable (%s) — falling back to log-based inference", err.Error()))
	} else {
		warnings = append(warnings, "no Istio request_count metrics found for this cluster (Anthos Service Mesh may not be enabled) — falling back to log-based inference")
	}

	edges, err = a.logBasedMeshEdges(ctx, projectID, clusterName, start, end)
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
func (a *gcpAdapter) istioMeshEdges(ctx context.Context, projectID, clusterName string, start, end time.Time) ([]models.GKEMeshEdge, error) {
	duration := end.Sub(start)
	if duration <= 0 {
		duration = 24 * time.Hour
	}

	filter := fmt.Sprintf(
		`metric.type="istio.io/service/server/request_count" AND resource.labels.cluster_name="%s"`,
		clusterName,
	)
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
			GroupByFields: []string{
				"metric.label.source_workload",
				"metric.label.source_workload_namespace",
				"metric.label.destination_service_name",
				"metric.label.destination_service_namespace",
			},
		},
		View: monitoringpb.ListTimeSeriesRequest_FULL,
	}

	it := a.metric.ListTimeSeries(ctx, pbReq)

	type edgeKey struct{ caller, callerNS, callee, calleeNS string }
	edgeMap := make(map[edgeKey]float64)

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

		lbl := ts.Metric.Labels
		src := lbl["source_workload"]
		srcNS := lbl["source_workload_namespace"]
		dst := lbl["destination_service_name"]
		dstNS := lbl["destination_service_namespace"]

		if src == "" || dst == "" || src == "unknown" || dst == "PassthroughCluster" {
			continue
		}

		key := edgeKey{src, srcNS, dst, dstNS}
		for _, pt := range ts.Points {
			// ALIGN_RATE gives req/s; convert to req/min.
			edgeMap[key] += extractPointValue(pt) * 60
		}
	}

	edges := make([]models.GKEMeshEdge, 0, len(edgeMap))
	for k, rpm := range edgeMap {
		edges = append(edges, models.GKEMeshEdge{
			Caller:            k.caller,
			CallerNamespace:   k.callerNS,
			Callee:            k.callee,
			CalleeNamespace:   k.calleeNS,
			RequestsPerMinute: rpm,
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
func (a *gcpAdapter) logBasedMeshEdges(ctx context.Context, projectID string, clusterName string, start, end time.Time) ([]models.GKEMeshEdge, error) {
	filter := fmt.Sprintf(
		`resource.type="k8s_container"`+
			` AND resource.labels.cluster_name="%s"`+
			` AND resource.labels.container_name="istio-proxy"`+
			` AND timestamp>="%s" AND timestamp<="%s"`,
		clusterName,
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
			RequestsPerMinute: float64(count), // log-entry count, not measured RPS
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
