package gcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	monitoringpb "cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

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
	if req.AlignmentPeriodSeconds <= 0 {
		req.AlignmentPeriodSeconds = 60
	}

	now := time.Now().UTC()
	startTime := now.Add(-time.Duration(req.LookbackMinutes) * time.Minute)

	// Build resource label filter.
	var filterParts []string
	filterParts = append(filterParts, fmt.Sprintf(`metric.type = "%s"`, req.MetricType))
	for k, v := range req.ResourceLabels {
		filterParts = append(filterParts, fmt.Sprintf(`resource.labels.%s = "%s"`, k, v))
	}
	filter := strings.Join(filterParts, " AND ")

	listReq := &monitoringpb.ListTimeSeriesRequest{
		Name:   fmt.Sprintf("projects/%s", req.ProjectID),
		Filter: filter,
		Interval: &monitoringpb.TimeInterval{
			StartTime: timestamppb.New(startTime),
			EndTime:   timestamppb.New(now),
		},
		Aggregation: &monitoringpb.Aggregation{
			AlignmentPeriod:    durationpb.New(time.Duration(req.AlignmentPeriodSeconds) * time.Second),
			PerSeriesAligner:   monitoringpb.Aggregation_ALIGN_MEAN,
			CrossSeriesReducer: monitoringpb.Aggregation_REDUCE_MEAN,
		},
		View: monitoringpb.ListTimeSeriesRequest_FULL,
	}

	it := a.metric.ListTimeSeries(ctx, listReq)

	var points []models.MetricPoint
	if ts, err := it.Next(); err == nil {
		for _, pt := range ts.Points {
			val := extractPointValue(pt)
			t := ""
			if pt.Interval != nil && pt.Interval.EndTime != nil {
				t = pt.Interval.EndTime.AsTime().UTC().Format(time.RFC3339)
			}
			points = append(points, models.MetricPoint{
				Timestamp: t,
				Value:     val,
			})
		}
	} else if err != iterator.Done {
		return models.GetMetricsResponse{}, wrapGCPError("monitoring.GetMetrics", err)
	}

	if points == nil {
		points = []models.MetricPoint{}
	}

	unit := ""
	return models.GetMetricsResponse{
		MetricType: req.MetricType,
		Points:     points,
		Unit:       unit,
	}, nil
}

func extractPointValue(pt *monitoringpb.Point) float64 {
	if pt == nil || pt.Value == nil {
		return 0
	}
	switch v := pt.Value.Value.(type) {
	case *monitoringpb.TypedValue_DoubleValue:
		return v.DoubleValue
	case *monitoringpb.TypedValue_Int64Value:
		return float64(v.Int64Value)
	case *monitoringpb.TypedValue_BoolValue:
		if v.BoolValue {
			return 1
		}
		return 0
	default:
		return 0
	}
}
