package gcp

import (
	"context"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

const (
	maxTraceDependencyPages = 10
	traceDependencyPageSize = 200
	defaultLookbackHours    = 168
)

func (a *gcpAdapter) ListTraceDependencyEdges(ctx context.Context, req models.ListTraceDependencyEdgesRequest) (models.ListTraceDependencyEdgesResponse, error) {
	if err := a.rateWait(ctx, "trace.ListTraceDependencyEdges"); err != nil {
		return models.ListTraceDependencyEdgesResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	lookback := req.LookbackHours
	if lookback <= 0 {
		lookback = defaultLookbackHours
	}

	startTime := time.Now().UTC().Add(-time.Duration(lookback) * time.Hour).Format(time.RFC3339)

	// edgeCount maps "caller|callee" → count.
	edgeCount := map[string]int{}

	pageToken := ""
	tracesScanned := 0

	for page := 0; page < maxTraceDependencyPages; page++ {
		call := a.traceClient.Projects.Traces.List(req.ProjectID).
			StartTime(startTime).
			PageSize(traceDependencyPageSize).
			OrderBy("start_time desc").
			Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return models.ListTraceDependencyEdgesResponse{}, wrapGCPError("trace.ListTraceDependencyEdges", err)
		}

		for _, t := range resp.Traces {
			tracesScanned++
			// spanService maps spanId → service name (extracted from the span's root label or name).
			spanService := map[string]string{}

			// First pass: collect span service name per span ID.
			for _, span := range t.Spans {
				svc := serviceNameFromSpan(span.Name, span.Labels)
				if svc != "" {
					spanService[uint64ToString(span.SpanId)] = svc
				}
			}

			// Second pass: for each child span, find a parent with a different service name.
			for _, span := range t.Spans {
				if span.ParentSpanId == 0 {
					continue
				}
				callee := spanService[uint64ToString(span.SpanId)]
				if callee == "" {
					continue
				}
				parentSpanID := uint64ToString(span.ParentSpanId)
				caller := spanService[parentSpanID]
				if caller == "" || caller == callee {
					continue
				}
				key := caller + "|" + callee
				edgeCount[key]++
			}
		}

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	edges := make([]models.TraceDependencyEdge, 0, len(edgeCount))
	for key, count := range edgeCount {
		caller, callee := splitEdgeKey(key)
		edges = append(edges, models.TraceDependencyEdge{
			Caller:      caller,
			Callee:      callee,
			SampleCount: count,
		})
	}

	return models.ListTraceDependencyEdgesResponse{
		Edges:         edges,
		TracesScanned: tracesScanned,
		LookbackHours: lookback,
		Backend:       "trace",
	}, nil
}

// serviceNameFromSpan extracts a service name from Cloud Trace span labels or falls back to the root path of the span name.
func serviceNameFromSpan(spanName string, labels map[string]string) string {
	// Standard OpenTelemetry / Cloud Trace semantic labels.
	for _, key := range []string{
		"g.co/r/k8s_container/container_name",
		"g.co/r/cloud_run_revision/service_name",
		"g.co/agent",
		"/http/host",
	} {
		if v, ok := labels[key]; ok && v != "" {
			return v
		}
	}
	// Fall back to the first path segment of the span name (e.g. "/api/v1/users" → "api").
	if spanName != "" {
		for i := 1; i < len(spanName); i++ {
			if spanName[i] == '/' {
				return spanName[1:i]
			}
		}
		if len(spanName) > 1 {
			return spanName[1:]
		}
	}
	return ""
}

func uint64ToString(n uint64) string {
	if n == 0 {
		return ""
	}
	// Cloud Trace span IDs are decimal uint64 strings in the proto but the v1 REST API
	// returns them as uint64; convert to string for map keying.
	b := make([]byte, 0, 20)
	if n == 0 {
		return "0"
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func splitEdgeKey(key string) (string, string) {
	for i, c := range key {
		if c == '|' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}
