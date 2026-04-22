package gcp

import (
	"context"
	"fmt"

	runpb "cloud.google.com/go/run/apiv2/runpb"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (a *gcpAdapter) ListServices(ctx context.Context, req models.ListServicesRequest) (models.ListServicesResponse, error) {
	if err := a.rateWait(ctx, "cloudrun.ListServices"); err != nil {
		return models.ListServicesResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	parent := fmt.Sprintf("projects/%s/locations/%s", req.ProjectID, req.Region)
	it := a.runSvc.ListServices(ctx, &runpb.ListServicesRequest{Parent: parent})

	var services []models.ServiceSummary
	for {
		svc, err := it.Next()
		if err != nil {
			if isIteratorDone(err) {
				break
			}
			return models.ListServicesResponse{}, wrapGCPError("cloudrun.ListServices", err)
		}
		lastMod := ""
		if svc.UpdateTime != nil {
			lastMod = svc.UpdateTime.AsTime().Format("2006-01-02T15:04:05Z")
		}
		services = append(services, models.ServiceSummary{
			Name:         svc.Name,
			Region:       req.Region,
			URL:          svc.Uri,
			LastModified: lastMod,
		})
	}
	if services == nil {
		services = []models.ServiceSummary{}
	}
	return models.ListServicesResponse{Services: services}, nil
}

func (a *gcpAdapter) GetServiceDetails(ctx context.Context, req models.GetServiceDetailsRequest) (models.ServiceDetails, error) {
	if err := a.rateWait(ctx, "cloudrun.GetServiceDetails"); err != nil {
		return models.ServiceDetails{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	name := fmt.Sprintf("projects/%s/locations/%s/services/%s", req.ProjectID, req.Region, req.ServiceName)
	svc, err := a.runSvc.GetService(ctx, &runpb.GetServiceRequest{Name: name})
	if err != nil {
		return models.ServiceDetails{}, wrapGCPError("cloudrun.GetServiceDetails", err)
	}

	traffic := make([]models.TrafficTarget, 0, len(svc.Traffic))
	for _, t := range svc.Traffic {
		traffic = append(traffic, models.TrafficTarget{
			Revision: t.Revision,
			Percent:  int32(t.Percent),
			Tag:      t.Tag,
		})
	}

	lastMod := ""
	if svc.UpdateTime != nil {
		lastMod = svc.UpdateTime.AsTime().Format("2006-01-02T15:04:05Z")
	}

	latestRevision := ""
	if svc.LatestCreatedRevision != "" {
		latestRevision = svc.LatestCreatedRevision
	}

	return models.ServiceDetails{
		ServiceSummary: models.ServiceSummary{
			Name:         svc.Name,
			Region:       req.Region,
			URL:          svc.Uri,
			LastModified: lastMod,
		},
		Traffic:        traffic,
		LatestRevision: latestRevision,
		Labels:         svc.Labels,
	}, nil
}

// UpdateTraffic updates the traffic split for a Cloud Run service.
// When DryRun=true it returns a description of the change without executing it.
func (a *gcpAdapter) UpdateTraffic(ctx context.Context, req models.UpdateTrafficRequest) (models.UpdateTrafficResponse, error) {
	if err := a.rateWait(ctx, "cloudrun.UpdateTraffic"); err != nil {
		return models.UpdateTrafficResponse{}, err
	}

	// Always fetch current state for before/after reporting.
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	name := fmt.Sprintf("projects/%s/locations/%s/services/%s", req.ProjectID, req.Region, req.ServiceName)
	current, err := a.runSvc.GetService(ctx, &runpb.GetServiceRequest{Name: name})
	if err != nil {
		return models.UpdateTrafficResponse{}, wrapGCPError("cloudrun.UpdateTraffic.get", err)
	}

	before := make([]models.TrafficTarget, 0, len(current.Traffic))
	for _, t := range current.Traffic {
		before = append(before, models.TrafficTarget{
			Revision: t.Revision,
			Percent:  int32(t.Percent),
			Tag:      t.Tag,
		})
	}

	if req.DryRun {
		return models.UpdateTrafficResponse{
			DryRun:      true,
			ServiceName: req.ServiceName,
			Before:      before,
			After:       req.Traffic,
			Description: fmt.Sprintf("DRY RUN: would update traffic for service %q", req.ServiceName),
		}, nil
	}

	pbTraffic := make([]*runpb.TrafficTarget, 0, len(req.Traffic))
	for _, t := range req.Traffic {
		pbTraffic = append(pbTraffic, &runpb.TrafficTarget{
			Revision: t.Revision,
			Percent:  t.Percent,
			Tag:      t.Tag,
		})
	}

	op, err := a.runSvc.UpdateService(ctx, &runpb.UpdateServiceRequest{
		Service: &runpb.Service{
			Name:    name,
			Traffic: pbTraffic,
		},
	})
	if err != nil {
		return models.UpdateTrafficResponse{}, wrapGCPError("cloudrun.UpdateTraffic.update", err)
	}
	// We don't wait for the LRO to complete — the operation was submitted successfully.
	_ = op

	return models.UpdateTrafficResponse{
		DryRun:      false,
		ServiceName: req.ServiceName,
		Before:      before,
		After:       req.Traffic,
		Description: fmt.Sprintf("traffic update submitted for service %q — operation in progress", req.ServiceName),
	}, nil
}
