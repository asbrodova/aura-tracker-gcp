package models

// TaggedResourceSummary represents a GCP resource bound to a specific tag.
type TaggedResourceSummary struct {
	// ResourceName is the full resource name (e.g. "//run.googleapis.com/projects/p/locations/us-central1/services/svc")
	ResourceName string `json:"resource_name"`
	TagValue     string `json:"tag_value"`              // e.g. "tagValues/456"
	TagNamespace string `json:"tag_namespace,omitempty"` // e.g. "123456789/env/prod"
}

// ListTaggedResourcesRequest specifies the tag key (and optional value) to filter on.
type ListTaggedResourcesRequest struct {
	ProjectID string `json:"project_id"`
	TagKey    string `json:"tag_key"`   // required; namespaced key e.g. "123456789/env" or short key e.g. "env"
	TagValue  string `json:"tag_value"` // optional; if empty, all values under the key are returned
}

// ListTaggedResourcesResponse contains resources matching the requested tag.
type ListTaggedResourcesResponse struct {
	ProjectID string                  `json:"project_id"`
	TagKey    string                  `json:"tag_key"`
	TagValue  string                  `json:"tag_value,omitempty"`
	Resources []TaggedResourceSummary `json:"resources"`
}
