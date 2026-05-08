package models

type ListBucketsRequest struct {
	ProjectID string `json:"project_id"`
}

type BucketSummary struct {
	Name         string            `json:"name"`
	Location     string            `json:"location"`
	StorageClass string            `json:"storage_class"`
	Labels       map[string]string `json:"labels,omitempty"`
	Created      string            `json:"created"`
}

type ListBucketsResponse struct {
	ProjectID string          `json:"project_id"`
	Buckets   []BucketSummary `json:"buckets"`
}

type GetBucketMetadataRequest struct {
	ProjectID  string `json:"project_id"`
	BucketName string `json:"bucket_name"`
}

type ListBucketObjectsRequest struct {
	ProjectID  string `json:"project_id"`
	BucketName string `json:"bucket_name"`
	Prefix     string `json:"prefix,omitempty"`
	MaxResults int    `json:"max_results,omitempty"`
}

type ObjectSummary struct {
	Name         string `json:"name"`
	SizeBytes    int64  `json:"size_bytes"`
	ContentType  string `json:"content_type"`
	StorageClass string `json:"storage_class"`
	Updated      string `json:"updated"`
}

type ListBucketObjectsResponse struct {
	BucketName string          `json:"bucket_name"`
	Prefix     string          `json:"prefix,omitempty"`
	Objects    []ObjectSummary `json:"objects"`
	Truncated  bool            `json:"truncated"`
}

type BucketMetadataResponse struct {
	Name                     string            `json:"name"`
	Location                 string            `json:"location"`
	StorageClass             string            `json:"storage_class"`
	Labels                   map[string]string `json:"labels,omitempty"`
	Created                  string            `json:"created"`
	VersioningEnabled        bool              `json:"versioning_enabled"`
	UniformBucketLevelAccess bool              `json:"uniform_bucket_level_access"`
	PublicAccessPrevention   string            `json:"public_access_prevention"`
	LifecycleRuleCount       int               `json:"lifecycle_rule_count"`
}
