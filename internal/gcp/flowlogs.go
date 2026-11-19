package gcp

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"cloud.google.com/go/logging/logadmin"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// VPC Flow Logs are high volume, so architecture-graph inference reads a
// bounded, newest-first sample: at most the last day, at most this many
// records, aggregated into at most this many distinct connections.
const (
	flowLogMaxWindow  = 24 * time.Hour
	flowLogMaxRecords = 5000
	flowLogMaxEdges   = 500
	flowLogPageSize   = 1000
)

// flowLogSample is the aggregated result of one flow-log read.
type flowLogSample struct {
	edges     []models.FlowLogEdge
	records   int
	truncated bool
	window    time.Duration
}

// collectFlowLogEdges reads VPC Flow Log records in which GKE annotated the
// source pod's workload and the destination's workload or Service, and
// aggregates them per source, destination, port and protocol.
func (a *gcpAdapter) collectFlowLogEdges(ctx context.Context, projectID string, lookbackHours int, now time.Time) (flowLogSample, error) {
	if a.logAdmin == nil {
		return flowLogSample{}, errClientNotInitialized("archgraph.flowLogs", clientLogAdmin)
	}
	if err := a.rateWait(ctx, "archgraph.flowLogs"); err != nil {
		return flowLogSample{}, err
	}
	window := time.Duration(lookbackHours) * time.Hour
	if window <= 0 || window > flowLogMaxWindow {
		window = flowLogMaxWindow
	}
	it := a.logAdmin.Entries(ctx,
		logadmin.ProjectIDs([]string{projectID}),
		logadmin.Filter(flowLogFilter(projectID, now.Add(-window), now)),
		logadmin.NewestFirst(),
		logadmin.PageSize(flowLogPageSize),
	)
	aggregate := newFlowAggregate()
	sample := flowLogSample{window: window}
	for {
		entry, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return flowLogSample{}, wrapGCPError("archgraph.flowLogs", err)
		}
		if sample.records >= flowLogMaxRecords {
			sample.truncated = true
			break
		}
		sample.records++
		payload, ok := entry.Payload.(*structpb.Struct)
		if !ok || payload == nil {
			continue
		}
		if !aggregate.add(payload.AsMap()) {
			sample.truncated = true
			break
		}
	}
	sample.edges = aggregate.edges()
	return sample, nil
}

func flowLogFilter(projectID string, start, end time.Time) string {
	return fmt.Sprintf(
		`logName="projects/%s/logs/compute.googleapis.com%%2Fvpc_flows"`+
			` AND timestamp>="%s" AND timestamp<="%s"`+
			` AND jsonPayload.src_gke_details.pod.pod_workload.workload_name:*`+
			` AND (jsonPayload.dest_gke_details.pod.pod_workload.workload_name:* OR jsonPayload.dest_gke_details.service.service_name:*)`,
		escapeLoggingString(projectID), start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339),
	)
}

type flowKey struct {
	source, destination models.FlowEndpoint
	port, protocol      int
}

type flowAggregate struct {
	byKey map[flowKey]*models.FlowLogEdge
}

func newFlowAggregate() *flowAggregate {
	return &flowAggregate{byKey: make(map[flowKey]*models.FlowLogEdge)}
}

// add folds one flow record into the aggregate. It returns false when a new
// connection would exceed flowLogMaxEdges; records for known connections are
// always counted.
func (f *flowAggregate) add(payload map[string]any) bool {
	edge, ok := flowLogEdgeFromPayload(payload)
	if !ok {
		return true
	}
	key := flowKey{source: edge.Source, destination: edge.Destination, port: edge.Port, protocol: edge.Protocol}
	if existing := f.byKey[key]; existing != nil {
		existing.Records++
		existing.Bytes += edge.Bytes
		return true
	}
	if len(f.byKey) >= flowLogMaxEdges {
		return false
	}
	f.byKey[key] = &edge
	return true
}

func (f *flowAggregate) edges() []models.FlowLogEdge {
	out := make([]models.FlowLogEdge, 0, len(f.byKey))
	for _, edge := range f.byKey {
		out = append(out, *edge)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Records != out[j].Records {
			return out[i].Records > out[j].Records
		}
		return fmt.Sprint(out[i].Source, out[i].Destination, out[i].Port) < fmt.Sprint(out[j].Source, out[j].Destination, out[j].Port)
	})
	return out
}

// flowLogEdgeFromPayload maps one vpc_flows jsonPayload to a single-record
// edge. Records without a GKE workload on the source side, or without a
// workload or Service on the destination side, are skipped.
func flowLogEdgeFromPayload(payload map[string]any) (models.FlowLogEdge, bool) {
	source := flowEndpoint(nestedMap(payload, "src_gke_details"))
	destination := flowEndpoint(nestedMap(payload, "dest_gke_details"))
	if source.Workload == "" || (destination.Workload == "" && destination.Service == "") {
		return models.FlowLogEdge{}, false
	}
	connection := nestedMap(payload, "connection")
	return models.FlowLogEdge{
		Source: source, Destination: destination,
		Port:     int(numberValue(connection["dest_port"])),
		Protocol: int(numberValue(connection["protocol"])),
		Records:  1,
		Bytes:    int64(numberValue(payload["bytes_sent"])),
	}, true
}

func flowEndpoint(details map[string]any) models.FlowEndpoint {
	if details == nil {
		return models.FlowEndpoint{}
	}
	cluster := nestedMap(details, "cluster")
	pod := nestedMap(details, "pod")
	endpoint := models.FlowEndpoint{
		Cluster:   stringValue(cluster["cluster_name"]),
		Location:  stringValue(cluster["cluster_location"]),
		Namespace: stringValue(pod["pod_namespace"]),
		Workload:  stringValue(nestedMap(pod, "pod_workload")["workload_name"]),
	}
	// GKE lists the Services that select the pod; use one only when it is
	// unambiguous.
	if services, ok := details["service"].([]any); ok && len(services) == 1 {
		if service, ok := services[0].(map[string]any); ok {
			endpoint.Service = stringValue(service["service_name"])
			if namespace := stringValue(service["service_namespace"]); namespace != "" {
				endpoint.Namespace = namespace
			}
		}
	}
	return endpoint
}

func nestedMap(value map[string]any, key string) map[string]any {
	if value == nil {
		return nil
	}
	child, _ := value[key].(map[string]any)
	return child
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

// numberValue reads a JSON number, or an int64 that Cloud Logging encodes as
// a string (bytes_sent, packets_sent).
func numberValue(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}
