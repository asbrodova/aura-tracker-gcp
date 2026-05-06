package models

// --- Spanner ---

type ListSpannerInstancesRequest struct {
	ProjectID string `json:"project_id"`
}

type SpannerInstanceSummary struct {
	Name            string `json:"name"`
	DisplayName     string `json:"display_name"`
	State           string `json:"state"`
	NodeCount       int64  `json:"node_count,omitempty"`
	ProcessingUnits int64  `json:"processing_units,omitempty"`
	Config          string `json:"config,omitempty"`
	Edition         string `json:"edition,omitempty"`
}

type ListSpannerInstancesResponse struct {
	Instances []SpannerInstanceSummary `json:"instances"`
}

// --- AlloyDB ---

type ListAlloyDBClustersRequest struct {
	ProjectID string `json:"project_id"`
	Location  string `json:"location,omitempty"`
}

type AlloyDBClusterSummary struct {
	Name            string `json:"name"`
	DisplayName     string `json:"display_name,omitempty"`
	State           string `json:"state"`
	ClusterType     string `json:"cluster_type,omitempty"`
	DatabaseVersion string `json:"database_version,omitempty"`
	Location        string `json:"location,omitempty"`
	CreateTime      string `json:"create_time,omitempty"`
}

type ListAlloyDBClustersResponse struct {
	Clusters []AlloyDBClusterSummary `json:"clusters"`
}

// --- Firestore ---

type ListFirestoreDatabasesRequest struct {
	ProjectID string `json:"project_id"`
}

type FirestoreDatabaseSummary struct {
	Name            string `json:"name"`
	Type            string `json:"type,omitempty"`
	LocationID      string `json:"location_id,omitempty"`
	ConcurrencyMode string `json:"concurrency_mode,omitempty"`
	DatabaseEdition string `json:"database_edition,omitempty"`
	CreateTime      string `json:"create_time,omitempty"`
}

type ListFirestoreDatabasesResponse struct {
	Databases []FirestoreDatabaseSummary `json:"databases"`
}

// --- Memorystore (Redis) ---

type ListMemorystoreInstancesRequest struct {
	ProjectID string `json:"project_id"`
	Location  string `json:"location,omitempty"`
}

type MemorystoreInstanceSummary struct {
	Name         string `json:"name"`
	DisplayName  string `json:"display_name,omitempty"`
	State        string `json:"state"`
	Tier         string `json:"tier,omitempty"`
	MemorySizeGb int64  `json:"memory_size_gb,omitempty"`
	RedisVersion string `json:"redis_version,omitempty"`
	Host         string `json:"host,omitempty"`
	LocationID   string `json:"location_id,omitempty"`
}

type ListMemorystoreInstancesResponse struct {
	Instances []MemorystoreInstanceSummary `json:"instances"`
}
