package gcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// ListTaggedResources lists CRM v3 TagBindings for the project resource, filtered
// by tag_key and optional tag_value matched against the namespaced tag name.
func (a *gcpAdapter) ListTaggedResources(ctx context.Context, req models.ListTaggedResourcesRequest) (models.ListTaggedResourcesResponse, error) {
	if err := a.rateWait(ctx, "tagging.ListTaggedResources"); err != nil {
		return models.ListTaggedResourcesResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	// Full resource name for the project itself.
	parent := fmt.Sprintf("//cloudresourcemanager.googleapis.com/projects/%s", req.ProjectID)

	var resources []models.TaggedResourceSummary
	pageToken := ""
	for {
		call := a.crmV3Svc.TagBindings.List().
			Parent(parent).
			PageSize(300).
			Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return models.ListTaggedResourcesResponse{}, wrapGCPError("tagging.ListTaggedResources", err)
		}
		for _, b := range resp.TagBindings {
			if !matchesTagFilter(b.TagValueNamespacedName, req.TagKey, req.TagValue) {
				continue
			}
			resources = append(resources, models.TaggedResourceSummary{
				ResourceName: b.Parent,
				TagValue:     b.TagValue,
				TagNamespace: b.TagValueNamespacedName,
			})
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return models.ListTaggedResourcesResponse{
		ProjectID: req.ProjectID,
		TagKey:    req.TagKey,
		TagValue:  req.TagValue,
		Resources: resources,
	}, nil
}

// matchesTagFilter checks whether the namespaced tag name (e.g. "12345/env/prod")
// contains the requested key and optional value.
// namespacedName format: {org_id}/{key}/{value} or just {key}/{value}.
func matchesTagFilter(namespacedName, key, value string) bool {
	if namespacedName == "" || key == "" {
		return false
	}
	parts := strings.Split(namespacedName, "/")
	if len(parts) < 2 {
		return false
	}
	keySegment := parts[len(parts)-2]
	valueSegment := parts[len(parts)-1]
	if keySegment != key {
		return false
	}
	if value == "" {
		return true
	}
	return valueSegment == value
}
