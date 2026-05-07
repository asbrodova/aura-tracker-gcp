package gcp

import (
	"context"

	"cloud.google.com/go/storage"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

const (
	maxBuckets       = 200
	maxBucketObjects = 1000
)

func (a *gcpAdapter) ListBuckets(ctx context.Context, req models.ListBucketsRequest) (models.ListBucketsResponse, error) {
	if err := a.rateWait(ctx, "storage.ListBuckets"); err != nil {
		return models.ListBucketsResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	it := a.gcs.Buckets(ctx, req.ProjectID)
	var buckets []models.BucketSummary
	for len(buckets) < maxBuckets {
		attrs, err := it.Next()
		if err != nil {
			if isIteratorDone(err) {
				break
			}
			return models.ListBucketsResponse{}, wrapGCPError("storage.ListBuckets", err)
		}
		buckets = append(buckets, models.BucketSummary{
			Name:         attrs.Name,
			Location:     attrs.Location,
			StorageClass: attrs.StorageClass,
			Labels:       attrs.Labels,
			Created:      attrs.Created.Format("2006-01-02T15:04:05Z"),
		})
	}
	if buckets == nil {
		buckets = []models.BucketSummary{}
	}
	return models.ListBucketsResponse{ProjectID: req.ProjectID, Buckets: buckets}, nil
}

func (a *gcpAdapter) ListBucketObjects(ctx context.Context, req models.ListBucketObjectsRequest) (models.ListBucketObjectsResponse, error) {
	if err := a.rateWait(ctx, "storage.ListBucketObjects"); err != nil {
		return models.ListBucketObjectsResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	limit := req.MaxResults
	if limit <= 0 || limit > maxBucketObjects {
		limit = maxBucketObjects
	}

	q := &storage.Query{Prefix: req.Prefix}
	it := a.gcs.Bucket(req.BucketName).Objects(ctx, q)

	var objects []models.ObjectSummary
	for len(objects) < limit {
		attrs, err := it.Next()
		if err != nil {
			if isIteratorDone(err) {
				break
			}
			return models.ListBucketObjectsResponse{}, wrapGCPError("storage.ListBucketObjects", err)
		}
		objects = append(objects, models.ObjectSummary{
			Name:         attrs.Name,
			SizeBytes:    attrs.Size,
			ContentType:  attrs.ContentType,
			StorageClass: attrs.StorageClass,
			Updated:      attrs.Updated.Format("2006-01-02T15:04:05Z"),
		})
	}

	truncated := false
	if len(objects) == limit {
		if _, err := it.Next(); !isIteratorDone(err) {
			truncated = true
		}
	}
	if objects == nil {
		objects = []models.ObjectSummary{}
	}
	return models.ListBucketObjectsResponse{
		BucketName: req.BucketName,
		Prefix:     req.Prefix,
		Objects:    objects,
		Truncated:  truncated,
	}, nil
}

func (a *gcpAdapter) GetBucketMetadata(ctx context.Context, req models.GetBucketMetadataRequest) (models.BucketMetadataResponse, error) {
	if err := a.rateWait(ctx, "storage.GetBucketMetadata"); err != nil {
		return models.BucketMetadataResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	attrs, err := a.gcs.Bucket(req.BucketName).Attrs(ctx)
	if err != nil {
		return models.BucketMetadataResponse{}, wrapGCPError("storage.GetBucketMetadata", err)
	}

	pap := attrs.PublicAccessPrevention.String()
	if attrs.PublicAccessPrevention == storage.PublicAccessPreventionUnknown {
		pap = "unknown"
	}

	return models.BucketMetadataResponse{
		Name:                     attrs.Name,
		Location:                 attrs.Location,
		StorageClass:             attrs.StorageClass,
		Labels:                   attrs.Labels,
		Created:                  attrs.Created.Format("2006-01-02T15:04:05Z"),
		VersioningEnabled:        attrs.VersioningEnabled,
		UniformBucketLevelAccess: attrs.UniformBucketLevelAccess.Enabled,
		PublicAccessPrevention:   pap,
		LifecycleRuleCount:       len(attrs.Lifecycle.Rules),
	}, nil
}
