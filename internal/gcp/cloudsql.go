package gcp

import (
	"context"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (a *gcpAdapter) ListSQLInstances(ctx context.Context, req models.ListSQLInstancesRequest) (models.ListSQLInstancesResponse, error) {
	if err := a.rateWait(ctx, "cloudsql.ListSQLInstances"); err != nil {
		return models.ListSQLInstancesResponse{}, err
	}
	if a.sqlAdmin == nil {
		return models.ListSQLInstancesResponse{Instances: []models.SQLInstanceSummary{}}, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	resp, err := a.sqlAdmin.Instances.List(req.ProjectID).Context(ctx).Do()
	if err != nil {
		return models.ListSQLInstancesResponse{}, wrapGCPError("cloudsql.ListSQLInstances", err)
	}

	instances := make([]models.SQLInstanceSummary, 0, len(resp.Items))
	for _, inst := range resp.Items {
		tier := ""
		var labels map[string]string
		if inst.Settings != nil {
			tier = inst.Settings.Tier
			labels = inst.Settings.UserLabels
		}
		instances = append(instances, models.SQLInstanceSummary{
			Name:            inst.Name,
			DatabaseVersion: inst.DatabaseVersion,
			Region:          inst.Region,
			State:           inst.State,
			Tier:            tier,
			Labels:          labels,
		})
	}
	return models.ListSQLInstancesResponse{Instances: instances}, nil
}
