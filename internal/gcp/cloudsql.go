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

	instances := []models.SQLInstanceSummary{}
	for pageToken := ""; ; {
		call := a.sqlAdmin.Instances.List(req.ProjectID).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return models.ListSQLInstancesResponse{}, wrapGCPError("cloudsql.ListSQLInstances", err)
		}
		for _, inst := range resp.Items {
			tier := ""
			var labels map[string]string
			if inst.Settings != nil {
				tier = inst.Settings.Tier
				labels = inst.Settings.UserLabels
			}
			instances = append(instances, models.SQLInstanceSummary{
				Name: inst.Name, DatabaseVersion: inst.DatabaseVersion, Region: inst.Region,
				State: inst.State, Tier: tier, Labels: labels,
			})
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return models.ListSQLInstancesResponse{Instances: instances}, nil
}
