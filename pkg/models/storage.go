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
