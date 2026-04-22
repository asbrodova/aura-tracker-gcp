package models

type QueryRecentLogsRequest struct {
	ProjectID       string `json:"project_id"`
	ResourceType    string `json:"resource_type"`
	ResourceName    string `json:"resource_name"`
	MinSeverity     string `json:"min_severity"`
	MaxEntries      int    `json:"max_entries"`
	LookbackMinutes int    `json:"lookback_minutes"`
}

type LogEntry struct {
	Timestamp string            `json:"timestamp"`
	Severity  string            `json:"severity"`
	Message   string            `json:"message"`
	Labels    map[string]string `json:"labels,omitempty"`
}

type QueryRecentLogsResponse struct {
	Entries      []LogEntry `json:"entries"`
	TotalFetched int        `json:"total_fetched"`
	Truncated    bool       `json:"truncated"`
}
