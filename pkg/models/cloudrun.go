package models

type ListServicesRequest struct {
	ProjectID string `json:"project_id"`
	Region    string `json:"region"`
}

type ServiceSummary struct {
	Name         string `json:"name"`
	Region       string `json:"region"`
	URL          string `json:"url"`
	LastModified string `json:"last_modified"`
}

type ListServicesResponse struct {
	Services []ServiceSummary `json:"services"`
}

type GetServiceDetailsRequest struct {
	ProjectID   string `json:"project_id"`
	Region      string `json:"region"`
	ServiceName string `json:"service_name"`
}

type TrafficTarget struct {
	Revision string `json:"revision"`
	Percent  int32  `json:"percent"`
	Tag      string `json:"tag,omitempty"`
}

type ServiceDetails struct {
	ServiceSummary
	Traffic        []TrafficTarget   `json:"traffic"`
	LatestRevision string            `json:"latest_revision"`
	Labels         map[string]string `json:"labels,omitempty"`
}

type UpdateTrafficRequest struct {
	ProjectID   string          `json:"project_id"`
	Region      string          `json:"region"`
	ServiceName string          `json:"service_name"`
	Traffic     []TrafficTarget `json:"traffic"`
	DryRun      bool            `json:"dry_run"`
}

type UpdateTrafficResponse struct {
	DryRun      bool            `json:"dry_run"`
	ServiceName string          `json:"service_name"`
	Before      []TrafficTarget `json:"before"`
	After       []TrafficTarget `json:"after"`
	Description string          `json:"description"`
}
