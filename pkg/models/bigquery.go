package models

type ListDatasetsRequest struct {
	ProjectID string `json:"project_id"`
	PageSize  int    `json:"page_size,omitempty"`
	PageToken string `json:"page_token,omitempty"`
}

type DatasetSummary struct {
	ID       string            `json:"id"`
	Location string            `json:"location"`
	Labels   map[string]string `json:"labels,omitempty"`
}

type ListDatasetsResponse struct {
	ProjectID     string           `json:"project_id"`
	Datasets      []DatasetSummary `json:"datasets"`
	NextPageToken string           `json:"next_page_token,omitempty"`
	Truncated     bool             `json:"truncated,omitempty"`
	Errors        []ToolError      `json:"errors,omitempty"`
}

type ListTablesRequest struct {
	ProjectID string `json:"project_id"`
	DatasetID string `json:"dataset_id"`
	PageSize  int    `json:"page_size,omitempty"`
	PageToken string `json:"page_token,omitempty"`
}

type TableSummary struct {
	ID             string  `json:"id"`
	Type           string  `json:"type"`
	NumRows        uint64  `json:"num_rows"`
	SizeGB         float64 `json:"size_gb"`
	PartitionField string  `json:"partition_field,omitempty"`
}

type ListTablesResponse struct {
	ProjectID     string         `json:"project_id"`
	DatasetID     string         `json:"dataset_id"`
	Tables        []TableSummary `json:"tables"`
	NextPageToken string         `json:"next_page_token,omitempty"`
	Truncated     bool           `json:"truncated,omitempty"`
	Errors        []ToolError    `json:"errors,omitempty"`
}

type GetTableSchemaRequest struct {
	ProjectID string `json:"project_id"`
	DatasetID string `json:"dataset_id"`
	TableID   string `json:"table_id"`
}

type FieldSchema struct {
	Name        string        `json:"name"`
	Type        string        `json:"type"`
	Mode        string        `json:"mode"`
	Description string        `json:"description,omitempty"`
	Fields      []FieldSchema `json:"fields,omitempty"`
}

type TableSchemaResponse struct {
	ProjectID string        `json:"project_id"`
	DatasetID string        `json:"dataset_id"`
	TableID   string        `json:"table_id"`
	Fields    []FieldSchema `json:"fields"`
	Truncated bool          `json:"truncated,omitempty"`
}
