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
