package gcp

import (
	"context"
	"strings"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (a *gcpAdapter) ListSpannerInstances(ctx context.Context, req models.ListSpannerInstancesRequest) (models.ListSpannerInstancesResponse, error) {
	if err := a.rateWait(ctx, "spanner.ListSpannerInstances"); err != nil {
		return models.ListSpannerInstancesResponse{}, err
	}
	if a.spannerSvc == nil {
		return models.ListSpannerInstancesResponse{Instances: []models.SpannerInstanceSummary{}}, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	parent := "projects/" + req.ProjectID
	instances := []models.SpannerInstanceSummary{}
	for pageToken := ""; ; {
		call := a.spannerSvc.Projects.Instances.List(parent).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return models.ListSpannerInstancesResponse{}, wrapGCPError("spanner.ListSpannerInstances", err)
		}
		for _, inst := range resp.Instances {
			instances = append(instances, models.SpannerInstanceSummary{
				Name:            inst.Name,
				DisplayName:     inst.DisplayName,
				State:           inst.State,
				NodeCount:       inst.NodeCount,
				ProcessingUnits: inst.ProcessingUnits,
				Config:          inst.Config,
				Edition:         inst.Edition,
			})
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return models.ListSpannerInstancesResponse{Instances: instances}, nil
}

func (a *gcpAdapter) ListAlloyDBClusters(ctx context.Context, req models.ListAlloyDBClustersRequest) (models.ListAlloyDBClustersResponse, error) {
	if err := a.rateWait(ctx, "alloydb.ListAlloyDBClusters"); err != nil {
		return models.ListAlloyDBClustersResponse{}, err
	}
	if a.alloydbSvc == nil {
		return models.ListAlloyDBClustersResponse{Clusters: []models.AlloyDBClusterSummary{}}, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	loc := req.Location
	if loc == "" {
		loc = "-"
	}
	parent := "projects/" + req.ProjectID + "/locations/" + loc

	clusters := []models.AlloyDBClusterSummary{}
	for pageToken := ""; ; {
		call := a.alloydbSvc.Projects.Locations.Clusters.List(parent).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return models.ListAlloyDBClustersResponse{}, wrapGCPError("alloydb.ListAlloyDBClusters", err)
		}
		for _, c := range resp.Clusters {
			clusters = append(clusters, models.AlloyDBClusterSummary{
				Name:            c.Name,
				DisplayName:     c.DisplayName,
				State:           c.State,
				ClusterType:     c.ClusterType,
				DatabaseVersion: c.DatabaseVersion,
				Location:        locationFromResourceName(c.Name),
				CreateTime:      c.CreateTime,
			})
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return models.ListAlloyDBClustersResponse{Clusters: clusters}, nil
}

func (a *gcpAdapter) ListFirestoreDatabases(ctx context.Context, req models.ListFirestoreDatabasesRequest) (models.ListFirestoreDatabasesResponse, error) {
	if err := a.rateWait(ctx, "firestore.ListFirestoreDatabases"); err != nil {
		return models.ListFirestoreDatabasesResponse{}, err
	}
	if a.firestoreSvc == nil {
		return models.ListFirestoreDatabasesResponse{Databases: []models.FirestoreDatabaseSummary{}}, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	parent := "projects/" + req.ProjectID
	databases := []models.FirestoreDatabaseSummary{}
	resp, err := a.firestoreSvc.Projects.Databases.List(parent).Context(ctx).Do()
	if err != nil {
		return models.ListFirestoreDatabasesResponse{}, wrapGCPError("firestore.ListFirestoreDatabases", err)
	}
	for _, db := range resp.Databases {
		databases = append(databases, models.FirestoreDatabaseSummary{
			Name:            db.Name,
			Type:            db.Type,
			LocationID:      db.LocationId,
			ConcurrencyMode: db.ConcurrencyMode,
			DatabaseEdition: db.DatabaseEdition,
			CreateTime:      db.CreateTime,
		})
	}
	return models.ListFirestoreDatabasesResponse{Databases: databases}, nil
}

func (a *gcpAdapter) ListMemorystoreInstances(ctx context.Context, req models.ListMemorystoreInstancesRequest) (models.ListMemorystoreInstancesResponse, error) {
	if err := a.rateWait(ctx, "redis.ListMemorystoreInstances"); err != nil {
		return models.ListMemorystoreInstancesResponse{}, err
	}
	if a.redisSvc == nil {
		return models.ListMemorystoreInstancesResponse{Instances: []models.MemorystoreInstanceSummary{}}, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	loc := req.Location
	if loc == "" {
		loc = "-"
	}
	parent := "projects/" + req.ProjectID + "/locations/" + loc

	instances := []models.MemorystoreInstanceSummary{}
	for pageToken := ""; ; {
		call := a.redisSvc.Projects.Locations.Instances.List(parent).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return models.ListMemorystoreInstancesResponse{}, wrapGCPError("redis.ListMemorystoreInstances", err)
		}
		for _, inst := range resp.Instances {
			instances = append(instances, models.MemorystoreInstanceSummary{
				Name:         inst.Name,
				DisplayName:  inst.DisplayName,
				State:        inst.State,
				Tier:         inst.Tier,
				MemorySizeGb: inst.MemorySizeGb,
				RedisVersion: inst.RedisVersion,
				Host:         inst.Host,
				LocationID:   inst.LocationId,
			})
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return models.ListMemorystoreInstancesResponse{Instances: instances}, nil
}

// locationFromResourceName extracts the location segment from a GCP resource name of the form
// "projects/{p}/locations/{loc}/...".
func locationFromResourceName(name string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		if p == "locations" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
