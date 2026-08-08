package gcp

import (
	"context"
	"fmt"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// ListCreatedAssets returns Cloud Asset Inventory resources whose authoritative
// createTime falls inside the requested interval. Asset types without a
// searchable createTime are naturally absent and are reported as partial
// coverage by the reasoning engine.
func (a *gcpAdapter) ListCreatedAssets(ctx context.Context, req models.ListCreatedAssetsRequest) (models.ListCreatedAssetsResponse, error) {
	const op = "cost.ListCreatedAssets"
	if a.assetSvc == nil {
		return models.ListCreatedAssetsResponse{}, fmt.Errorf("%s: Cloud Asset Inventory is not configured", op)
	}
	if err := a.rateWait(ctx, op); err != nil {
		return models.ListCreatedAssetsResponse{}, err
	}
	if !costProjectIDRE.MatchString(req.ProjectID) {
		return models.ListCreatedAssetsResponse{}, fmt.Errorf("%s: invalid project ID %q", op, req.ProjectID)
	}
	start, err := time.Parse(time.RFC3339, req.StartTime)
	if err != nil {
		return models.ListCreatedAssetsResponse{}, fmt.Errorf("%s: invalid start_time", op)
	}
	end, err := time.Parse(time.RFC3339, req.EndTime)
	if err != nil || !start.Before(end) {
		return models.ListCreatedAssetsResponse{}, fmt.Errorf("%s: invalid end_time", op)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	ctx, cancel := a.withTimeout(ctx)
	defer cancel()
	query := createdAssetQuery(start, end)
	assets := make([]models.CreatedAsset, 0, min(limit, 100))
	truncated := false
	// The generated REST client uses a concrete response callback, so page
	// manually to retain a strict result cap.
	pageToken := ""
	for {
		pageCall := a.assetSvc.V1.SearchAllResources("projects/" + req.ProjectID).
			Query(query).
			ReadMask("name,displayName,assetType,location,createTime,labels").
			PageSize(int64(min(limit-len(assets), 500)))
		if pageToken != "" {
			pageCall = pageCall.PageToken(pageToken)
		}
		resp, err := pageCall.Context(ctx).Do()
		if err != nil {
			return models.ListCreatedAssetsResponse{}, wrapGCPError(op, err)
		}
		for _, resource := range resp.Results {
			if len(assets) >= limit {
				truncated = true
				break
			}
			assets = append(assets, models.CreatedAsset{
				Name: resource.Name, DisplayName: resource.DisplayName, AssetType: resource.AssetType,
				Location: resource.Location, CreateTime: resource.CreateTime, Labels: resource.Labels,
			})
		}
		if len(assets) >= limit && resp.NextPageToken != "" {
			truncated = true
		}
		if truncated || resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return models.ListCreatedAssetsResponse{Assets: assets, Truncated: truncated}, nil
}

func createdAssetQuery(start, end time.Time) string {
	return fmt.Sprintf("createTime >= %d AND createTime < %d", start.Unix(), end.Unix())
}
