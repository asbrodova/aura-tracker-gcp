package models

// --- Artifact Registry ---

type ListArtifactRegistryReposRequest struct {
	ProjectID string `json:"project_id"`
	Location  string `json:"location,omitempty"`
}

type ArtifactRegistryRepoSummary struct {
	Name        string `json:"name"`
	Format      string `json:"format"`
	Location    string `json:"location,omitempty"`
	Description string `json:"description,omitempty"`
	CreateTime  string `json:"create_time,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
}

type ListArtifactRegistryReposResponse struct {
	Repositories []ArtifactRegistryRepoSummary `json:"repositories"`
}

type ListArtifactRegistryImagesRequest struct {
	ProjectID  string `json:"project_id"`
	Location   string `json:"location"`
	Repository string `json:"repository"`
}

type ArtifactRegistryImageSummary struct {
	Name       string   `json:"name"`
	URI        string   `json:"uri,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	BuildTime  string   `json:"build_time,omitempty"`
	UploadTime string   `json:"upload_time,omitempty"`
	SizeBytes  int64    `json:"size_bytes,omitempty"`
}

type ListArtifactRegistryImagesResponse struct {
	Images []ArtifactRegistryImageSummary `json:"images"`
}

// --- Cloud Build ---

type ListCloudBuildTriggersRequest struct {
	ProjectID string `json:"project_id"`
	Region    string `json:"region,omitempty"`
}

type CloudBuildTriggerSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	EventType   string   `json:"event_type,omitempty"`
	Disabled    bool     `json:"disabled,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	CreateTime  string   `json:"create_time,omitempty"`
	Filename    string   `json:"filename,omitempty"`
}

type ListCloudBuildTriggersResponse struct {
	Triggers []CloudBuildTriggerSummary `json:"triggers"`
}

// --- Service Directory ---

type ListServiceDirectoryNamespacesRequest struct {
	ProjectID string `json:"project_id"`
	Location  string `json:"location,omitempty"`
}

type ServiceDirectoryService struct {
	Name string `json:"name"`
}

type ServiceDirectoryNamespaceSummary struct {
	Name     string                    `json:"name"`
	Location string                    `json:"location,omitempty"`
	Services []ServiceDirectoryService `json:"services,omitempty"`
}

type ListServiceDirectoryNamespacesResponse struct {
	Namespaces []ServiceDirectoryNamespaceSummary `json:"namespaces"`
}
