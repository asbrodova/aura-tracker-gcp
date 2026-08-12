package models

// TriggerSummary is a lightweight view of an Eventarc trigger.
type TriggerSummary struct {
	Name            string            `json:"name"`
	Region          string            `json:"region"`
	ServiceAccount  string            `json:"service_account,omitempty"`
	DestinationKind string            `json:"destination_kind"` // cloud_run_service | cloud_function | workflow | gke | http_endpoint | unknown
	DestinationURN  string            `json:"destination_urn,omitempty"`
	TransportTopic  string            `json:"transport_topic,omitempty"` // Pub/Sub topic used as Eventarc transport
	EventFilters    []EventFilter     `json:"event_filters,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	LastModified    string            `json:"last_modified,omitempty"`
}

// EventFilter represents a single event attribute filter on an Eventarc trigger.
type EventFilter struct {
	Attribute string `json:"attribute"`
	Value     string `json:"value"`
	Operator  string `json:"operator,omitempty"`
}

// TriggerDetails extends TriggerSummary (reserved for future fields).
type TriggerDetails struct {
	TriggerSummary
}

// ListTriggersRequest lists Eventarc triggers.
type ListTriggersRequest struct {
	ProjectID string `json:"project_id"`
	Region    string `json:"region,omitempty"` // omit or "-" for all regions
}

// ListTriggersResponse holds the list result with any per-region errors.
type ListTriggersResponse struct {
	Triggers  []TriggerSummary `json:"triggers"`
	Errors    []ToolError      `json:"errors,omitempty"`
	Truncated bool             `json:"truncated,omitempty"`
}

// GetTriggerRequest fetches a single trigger.
type GetTriggerRequest struct {
	ProjectID   string `json:"project_id"`
	Region      string `json:"region"`
	TriggerName string `json:"trigger_name"`
}
