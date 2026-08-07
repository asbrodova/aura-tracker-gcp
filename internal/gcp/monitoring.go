package gcp

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	monitoringpb "cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

var monitoringLabelKeyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var supportedAligners = map[string]monitoringpb.Aggregation_Aligner{
	"ALIGN_NONE":          monitoringpb.Aggregation_ALIGN_NONE,
	"ALIGN_MEAN":          monitoringpb.Aggregation_ALIGN_MEAN,
	"ALIGN_SUM":           monitoringpb.Aggregation_ALIGN_SUM,
	"ALIGN_MIN":           monitoringpb.Aggregation_ALIGN_MIN,
	"ALIGN_MAX":           monitoringpb.Aggregation_ALIGN_MAX,
	"ALIGN_COUNT":         monitoringpb.Aggregation_ALIGN_COUNT,
	"ALIGN_DELTA":         monitoringpb.Aggregation_ALIGN_DELTA,
	"ALIGN_RATE":          monitoringpb.Aggregation_ALIGN_RATE,
	"ALIGN_PERCENTILE_50": monitoringpb.Aggregation_ALIGN_PERCENTILE_50,
	"ALIGN_PERCENTILE_95": monitoringpb.Aggregation_ALIGN_PERCENTILE_95,
	"ALIGN_PERCENTILE_99": monitoringpb.Aggregation_ALIGN_PERCENTILE_99,
}

var supportedReducers = map[string]monitoringpb.Aggregation_Reducer{
	"REDUCE_NONE":          monitoringpb.Aggregation_REDUCE_NONE,
	"REDUCE_MEAN":          monitoringpb.Aggregation_REDUCE_MEAN,
	"REDUCE_SUM":           monitoringpb.Aggregation_REDUCE_SUM,
	"REDUCE_MIN":           monitoringpb.Aggregation_REDUCE_MIN,
	"REDUCE_MAX":           monitoringpb.Aggregation_REDUCE_MAX,
	"REDUCE_COUNT":         monitoringpb.Aggregation_REDUCE_COUNT,
	"REDUCE_PERCENTILE_50": monitoringpb.Aggregation_REDUCE_PERCENTILE_50,
	"REDUCE_PERCENTILE_95": monitoringpb.Aggregation_REDUCE_PERCENTILE_95,
	"REDUCE_PERCENTILE_99": monitoringpb.Aggregation_REDUCE_PERCENTILE_99,
}

func (a *gcpAdapter) ListAlertPolicies(ctx context.Context, req models.ListAlertPoliciesRequest) (models.ListAlertPoliciesResponse, error) {
	if err := a.rateWait(ctx, "monitoring.ListAlertPolicies"); err != nil {
		return models.ListAlertPoliciesResponse{}, err
	}
	if a.monitoringSvc == nil {
		return models.ListAlertPoliciesResponse{Policies: []models.AlertPolicySummary{}}, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	call := a.monitoringSvc.Projects.AlertPolicies.List("projects/" + req.ProjectID).Context(ctx)
	if req.Filter != "" {
		call = call.Filter(req.Filter)
	}
	resp, err := call.Do()
	if err != nil {
		return models.ListAlertPoliciesResponse{}, wrapGCPError("monitoring.ListAlertPolicies", err)
	}

	policies := make([]models.AlertPolicySummary, 0, len(resp.AlertPolicies))
	for _, p := range resp.AlertPolicies {
		enabled := p.Enabled
		conditions := make([]string, 0, len(p.Conditions))
		for _, c := range p.Conditions {
			conditions = append(conditions, c.DisplayName)
		}
		policies = append(policies, models.AlertPolicySummary{
			Name:        p.Name,
			DisplayName: p.DisplayName,
			Enabled:     enabled,
			Severity:    p.Severity,
			Conditions:  conditions,
		})
	}
	return models.ListAlertPoliciesResponse{Policies: policies}, nil
}

func (a *gcpAdapter) ListUptimeChecks(ctx context.Context, req models.ListUptimeChecksRequest) (models.ListUptimeChecksResponse, error) {
	if err := a.rateWait(ctx, "monitoring.ListUptimeChecks"); err != nil {
		return models.ListUptimeChecksResponse{}, err
	}
	if a.monitoringSvc == nil {
		return models.ListUptimeChecksResponse{UptimeChecks: []models.UptimeCheckSummary{}}, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	resp, err := a.monitoringSvc.Projects.UptimeCheckConfigs.List("projects/" + req.ProjectID).Context(ctx).Do()
	if err != nil {
		return models.ListUptimeChecksResponse{}, wrapGCPError("monitoring.ListUptimeChecks", err)
	}

	checks := make([]models.UptimeCheckSummary, 0, len(resp.UptimeCheckConfigs))
	for _, c := range resp.UptimeCheckConfigs {
		checkerType := "STATIC_IP_CHECKERS"
		if c.CheckerType != "" {
			checkerType = c.CheckerType
		}
		checks = append(checks, models.UptimeCheckSummary{
			Name:        c.Name,
			DisplayName: c.DisplayName,
			Period:      c.Period,
			Timeout:     c.Timeout,
			CheckerType: checkerType,
		})
	}
	return models.ListUptimeChecksResponse{UptimeChecks: checks}, nil
}

func (a *gcpAdapter) ListSLOs(ctx context.Context, req models.ListSLOsRequest) (models.ListSLOsResponse, error) {
	if err := a.rateWait(ctx, "monitoring.ListSLOs"); err != nil {
		return models.ListSLOsResponse{}, err
	}
	if a.monitoringSvc == nil {
		return models.ListSLOsResponse{SLOs: []models.SLOSummary{}}, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	// List services first, then SLOs per service.
	parent := "projects/" + req.ProjectID
	svcResp, err := a.monitoringSvc.Services.List(parent).Context(ctx).Do()
	if err != nil {
		return models.ListSLOsResponse{}, wrapGCPError("monitoring.ListSLOs.services", err)
	}

	var slos []models.SLOSummary
	for _, svc := range svcResp.Services {
		if req.ServiceName != "" && !strings.HasSuffix(svc.Name, "/"+req.ServiceName) {
			continue
		}
		if err := a.rateWait(ctx, "monitoring.ListSLOs.slos"); err != nil {
			return models.ListSLOsResponse{}, err
		}
		sloResp, err := a.monitoringSvc.Services.ServiceLevelObjectives.List(svc.Name).Context(ctx).Do()
		if err != nil {
			continue
		}
		for _, s := range sloResp.ServiceLevelObjectives {
			slos = append(slos, models.SLOSummary{
				Name:           s.Name,
				DisplayName:    s.DisplayName,
				Goal:           s.Goal,
				CalendarPeriod: s.CalendarPeriod,
			})
		}
	}
	if slos == nil {
		slos = []models.SLOSummary{}
	}
	return models.ListSLOsResponse{SLOs: slos}, nil
}

func (a *gcpAdapter) ListDashboards(ctx context.Context, req models.ListDashboardsRequest) (models.ListDashboardsResponse, error) {
	if err := a.rateWait(ctx, "monitoring.ListDashboards"); err != nil {
		return models.ListDashboardsResponse{}, err
	}
	if a.monitoringV1Svc == nil {
		return models.ListDashboardsResponse{Dashboards: []models.DashboardSummary{}}, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	resp, err := a.monitoringV1Svc.Projects.Dashboards.List("projects/" + req.ProjectID).Context(ctx).Do()
	if err != nil {
		return models.ListDashboardsResponse{}, wrapGCPError("monitoring.ListDashboards", err)
	}

	dashboards := make([]models.DashboardSummary, 0, len(resp.Dashboards))
	for _, d := range resp.Dashboards {
		dashboards = append(dashboards, models.DashboardSummary{
			Name:        d.Name,
			DisplayName: d.DisplayName,
			Etag:        d.Etag,
		})
	}
	return models.ListDashboardsResponse{Dashboards: dashboards}, nil
}

func (a *gcpAdapter) ListMetricDescriptors(ctx context.Context, req models.ListMetricDescriptorsRequest) (models.ListMetricDescriptorsResponse, error) {
	if err := a.rateWait(ctx, "monitoring.ListMetricDescriptors"); err != nil {
		return models.ListMetricDescriptorsResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	pbReq := &monitoringpb.ListMetricDescriptorsRequest{
		Name: fmt.Sprintf("projects/%s", req.ProjectID),
	}
	if req.Filter != "" {
		pbReq.Filter = req.Filter
	}

	it := a.metric.ListMetricDescriptors(ctx, pbReq)
	var descriptors []models.MetricDescriptorSummary
	for {
		md, err := it.Next()
		if isIteratorDone(err) {
			break
		}
		if err != nil {
			return models.ListMetricDescriptorsResponse{}, wrapGCPError("monitoring.ListMetricDescriptors", err)
		}
		descriptors = append(descriptors, models.MetricDescriptorSummary{
			Type:        md.Type,
			DisplayName: md.DisplayName,
			Description: md.Description,
			MetricKind:  md.MetricKind.String(),
			ValueType:   md.ValueType.String(),
			Unit:        md.Unit,
		})
	}
	if descriptors == nil {
		descriptors = []models.MetricDescriptorSummary{}
	}
	return models.ListMetricDescriptorsResponse{Descriptors: descriptors}, nil
}

func (a *gcpAdapter) ListTraceServices(ctx context.Context, req models.ListTraceServicesRequest) (models.ListTraceServicesResponse, error) {
	if err := a.rateWait(ctx, "monitoring.ListTraceServices"); err != nil {
		return models.ListTraceServicesResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	if a.traceBackend == "monitoring" {
		return a.listTraceServicesViaMonitoring(ctx, req)
	}
	return a.listTraceServicesViaTrace(ctx, req)
}

func (a *gcpAdapter) listTraceServicesViaTrace(ctx context.Context, req models.ListTraceServicesRequest) (models.ListTraceServicesResponse, error) {
	startTime := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)

	seen := map[string]bool{}
	pageToken := ""
	for {
		call := a.traceClient.Projects.Traces.List(req.ProjectID).
			StartTime(startTime).
			PageSize(1000).
			Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return models.ListTraceServicesResponse{}, wrapGCPError("monitoring.ListTraceServices", err)
		}
		for _, t := range resp.Traces {
			for _, span := range t.Spans {
				if span.ParentSpanId == 0 {
					name := span.Name
					if name != "" && !seen[name] {
						seen[name] = true
					}
				}
			}
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	services := make([]models.TraceService, 0, len(seen))
	for name := range seen {
		services = append(services, models.TraceService{Name: name})
	}
	return models.ListTraceServicesResponse{Services: services, Backend: "trace"}, nil
}

func (a *gcpAdapter) listTraceServicesViaMonitoring(ctx context.Context, req models.ListTraceServicesRequest) (models.ListTraceServicesResponse, error) {
	now := time.Now().UTC()
	startTime := now.Add(-24 * time.Hour)

	pbReq := &monitoringpb.ListTimeSeriesRequest{
		Name:   fmt.Sprintf("projects/%s", req.ProjectID),
		Filter: `metric.type = "custom.googleapis.com/opencensus/grpc.io/client/completed_rpcs"`,
		Interval: &monitoringpb.TimeInterval{
			StartTime: timestamppb.New(startTime),
			EndTime:   timestamppb.New(now),
		},
		Aggregation: &monitoringpb.Aggregation{
			AlignmentPeriod:    durationpb.New(24 * time.Hour),
			PerSeriesAligner:   monitoringpb.Aggregation_ALIGN_MEAN,
			CrossSeriesReducer: monitoringpb.Aggregation_REDUCE_COUNT,
			GroupByFields:      []string{"resource.labels.service_name"},
		},
		View: monitoringpb.ListTimeSeriesRequest_HEADERS,
	}

	it := a.metric.ListTimeSeries(ctx, pbReq)
	seen := map[string]bool{}
	for {
		ts, err := it.Next()
		if isIteratorDone(err) {
			break
		}
		if err != nil {
			return models.ListTraceServicesResponse{}, wrapGCPError("monitoring.ListTraceServices(monitoring)", err)
		}
		if ts.Resource != nil {
			if name, ok := ts.Resource.Labels["service_name"]; ok && name != "" {
				seen[name] = true
			}
		}
		if ts.Metric != nil {
			if name, ok := ts.Metric.Labels["service_name"]; ok && name != "" {
				seen[name] = true
			}
		}
	}

	services := make([]models.TraceService, 0, len(seen))
	for name := range seen {
		services = append(services, models.TraceService{Name: name})
	}
	return models.ListTraceServicesResponse{Services: services, Backend: "monitoring"}, nil
}

func (a *gcpAdapter) GetMetrics(ctx context.Context, req models.GetMetricsRequest) (models.GetMetricsResponse, error) {
	if err := a.rateWait(ctx, "monitoring.GetMetrics"); err != nil {
		return models.GetMetricsResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	if req.LookbackMinutes <= 0 {
		req.LookbackMinutes = 60
	}
	if req.LookbackMinutes > 1440 {
		return models.GetMetricsResponse{}, fmt.Errorf("monitoring.GetMetrics: lookback_minutes must be at most 1440")
	}
	if req.AlignmentPeriodSeconds <= 0 {
		req.AlignmentPeriodSeconds = 60
	}
	if req.AlignmentPeriodSeconds < 10 || req.AlignmentPeriodSeconds > 86400 {
		return models.GetMetricsResponse{}, fmt.Errorf("monitoring.GetMetrics: alignment_period_seconds must be between 10 and 86400")
	}
	if req.MaxTimeSeries <= 0 {
		req.MaxTimeSeries = 100
	}
	if req.MaxTimeSeries > 1000 {
		return models.GetMetricsResponse{}, fmt.Errorf("monitoring.GetMetrics: max_time_series must be at most 1000")
	}

	alignerName := strings.ToUpper(strings.TrimSpace(req.PerSeriesAligner))
	if alignerName == "" {
		alignerName = "ALIGN_NONE"
	}
	aligner, ok := supportedAligners[alignerName]
	if !ok {
		return models.GetMetricsResponse{}, fmt.Errorf("monitoring.GetMetrics: unsupported per_series_aligner %q", req.PerSeriesAligner)
	}

	reducerName := strings.ToUpper(strings.TrimSpace(req.CrossSeriesReducer))
	if reducerName == "" {
		reducerName = "REDUCE_NONE"
	}
	reducer, ok := supportedReducers[reducerName]
	if !ok {
		return models.GetMetricsResponse{}, fmt.Errorf("monitoring.GetMetrics: unsupported cross_series_reducer %q", req.CrossSeriesReducer)
	}
	if reducer != monitoringpb.Aggregation_REDUCE_NONE && aligner == monitoringpb.Aggregation_ALIGN_NONE {
		return models.GetMetricsResponse{}, fmt.Errorf("monitoring.GetMetrics: cross-series reduction requires a non-NONE per-series aligner")
	}

	now := time.Now().UTC()
	startTime := now.Add(-time.Duration(req.LookbackMinutes) * time.Minute)

	filter, err := buildMetricFilter(req)
	if err != nil {
		return models.GetMetricsResponse{}, fmt.Errorf("monitoring.GetMetrics: %w", err)
	}

	listReq := &monitoringpb.ListTimeSeriesRequest{
		Name:   fmt.Sprintf("projects/%s", req.ProjectID),
		Filter: filter,
		Interval: &monitoringpb.TimeInterval{
			StartTime: timestamppb.New(startTime),
			EndTime:   timestamppb.New(now),
		},
		View: monitoringpb.ListTimeSeriesRequest_FULL,
	}
	if aligner != monitoringpb.Aggregation_ALIGN_NONE || reducer != monitoringpb.Aggregation_REDUCE_NONE {
		for _, field := range req.GroupByFields {
			if err := validateGroupByField(field); err != nil {
				return models.GetMetricsResponse{}, fmt.Errorf("monitoring.GetMetrics: %w", err)
			}
		}
		listReq.Aggregation = &monitoringpb.Aggregation{
			AlignmentPeriod:    durationpb.New(time.Duration(req.AlignmentPeriodSeconds) * time.Second),
			PerSeriesAligner:   aligner,
			CrossSeriesReducer: reducer,
			GroupByFields:      req.GroupByFields,
		}
	} else if len(req.GroupByFields) > 0 {
		return models.GetMetricsResponse{}, fmt.Errorf("monitoring.GetMetrics: group_by_fields requires aggregation")
	}

	it := a.metric.ListTimeSeries(ctx, listReq)

	series := make([]models.MetricSeries, 0)
	truncated := false
	for {
		ts, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return models.GetMetricsResponse{}, wrapGCPError("monitoring.GetMetrics", err)
		}
		if len(series) >= req.MaxTimeSeries {
			truncated = true
			break
		}
		series = append(series, metricSeriesFromProto(ts))
	}

	points := make([]models.MetricPoint, 0)
	unit := ""
	if len(series) > 0 {
		points = series[0].Points
		unit = series[0].Unit
	}

	return models.GetMetricsResponse{
		MetricType:         req.MetricType,
		Points:             points,
		Series:             series,
		Unit:               unit,
		PerSeriesAligner:   alignerName,
		CrossSeriesReducer: reducerName,
		NoData:             len(series) == 0,
		Truncated:          truncated,
	}, nil
}

func buildMetricFilter(req models.GetMetricsRequest) (string, error) {
	if strings.TrimSpace(req.MetricType) == "" {
		return "", fmt.Errorf("metric_type is required")
	}
	parts := []string{`metric.type = "` + escapeMonitoringString(req.MetricType) + `"`}
	resourceKeys := sortedMapKeys(req.ResourceLabels)
	for _, key := range resourceKeys {
		if !monitoringLabelKeyRE.MatchString(key) {
			return "", fmt.Errorf("invalid resource label key %q", key)
		}
		parts = append(parts, `resource.labels.`+key+` = "`+escapeMonitoringString(req.ResourceLabels[key])+`"`)
	}
	metricKeys := sortedMapKeys(req.MetricLabels)
	for _, key := range metricKeys {
		if !monitoringLabelKeyRE.MatchString(key) {
			return "", fmt.Errorf("invalid metric label key %q", key)
		}
		parts = append(parts, `metric.labels.`+key+` = "`+escapeMonitoringString(req.MetricLabels[key])+`"`)
	}
	return strings.Join(parts, " AND "), nil
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func escapeMonitoringString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func validateGroupByField(field string) error {
	if field == "resource.type" {
		return nil
	}
	for _, prefix := range []string{"resource.labels.", "metric.labels."} {
		if strings.HasPrefix(field, prefix) && monitoringLabelKeyRE.MatchString(strings.TrimPrefix(field, prefix)) {
			return nil
		}
	}
	return fmt.Errorf("invalid group_by_field %q", field)
}

func metricSeriesFromProto(ts *monitoringpb.TimeSeries) models.MetricSeries {
	series := models.MetricSeries{Points: []models.MetricPoint{}}
	if ts == nil {
		return series
	}
	if ts.Metric != nil {
		series.MetricLabels = ts.Metric.Labels
	}
	if ts.Resource != nil {
		series.ResourceType = ts.Resource.Type
		series.ResourceLabels = ts.Resource.Labels
	}
	series.MetricKind = ts.MetricKind.String()
	series.ValueType = ts.ValueType.String()
	series.Unit = ts.Unit
	for _, point := range ts.Points {
		series.Points = append(series.Points, metricPointFromProto(point))
	}
	return series
}

func metricPointFromProto(pt *monitoringpb.Point) models.MetricPoint {
	out := models.MetricPoint{}
	if pt == nil {
		return out
	}
	if pt.Interval != nil && pt.Interval.EndTime != nil {
		out.Timestamp = pt.Interval.EndTime.AsTime().UTC().Format(time.RFC3339)
	}
	if pt.Value == nil {
		return out
	}
	switch v := pt.Value.Value.(type) {
	case *monitoringpb.TypedValue_DoubleValue:
		out.Value = v.DoubleValue
		out.ValueType = "DOUBLE"
	case *monitoringpb.TypedValue_Int64Value:
		out.Value = float64(v.Int64Value)
		out.ValueType = "INT64"
	case *monitoringpb.TypedValue_BoolValue:
		out.ValueType = "BOOL"
		if v.BoolValue {
			out.Value = 1
		}
	case *monitoringpb.TypedValue_StringValue:
		out.ValueType = "STRING"
		out.TextValue = v.StringValue
	case *monitoringpb.TypedValue_DistributionValue:
		out.ValueType = "DISTRIBUTION"
		if distribution := v.DistributionValue; distribution != nil {
			summary := &models.MetricDistributionSummary{
				Count: distribution.Count,
				Mean:  distribution.Mean,
			}
			if distribution.Range != nil {
				summary.Min = distribution.Range.Min
				summary.Max = distribution.Range.Max
			}
			out.Value = distribution.Mean
			out.Distribution = summary
		}
	}
	return out
}

// extractPointValue is retained for the Aura and mesh collectors, which only
// consume scalar points. Distribution-aware callers should use
// metricPointFromProto so they do not lose count and range information.
func extractPointValue(pt *monitoringpb.Point) float64 {
	return metricPointFromProto(pt).Value
}
