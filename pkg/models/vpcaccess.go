package models

// VPCConnectorSummary is a lightweight view of a Serverless VPC Access connector.
type VPCConnectorSummary struct {
	Name    string `json:"name"`
	Region  string `json:"region"`
	Network string `json:"network,omitempty"`
	State   string `json:"state,omitempty"`
}

// ListVPCConnectorsRequest lists VPC Access connectors.
type ListVPCConnectorsRequest struct {
	ProjectID string `json:"project_id"`
	Region    string `json:"region,omitempty"` // omit or "-" for all regions
}

// ListVPCConnectorsResponse holds the list result with any per-region errors.
type ListVPCConnectorsResponse struct {
	Connectors []VPCConnectorSummary `json:"connectors"`
	Errors     []ToolError           `json:"errors,omitempty"`
}
