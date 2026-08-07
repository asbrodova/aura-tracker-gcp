package models

type QueryRecentLogsRequest struct {
	ProjectID string `json:"project_id"`
	// Filter is an optional native Cloud Logging filter. The adapter always
	// appends a bounded timestamp clause. When Filter is set, the structured
	// resource fields below are ignored except for MinSeverity.
	Filter          string            `json:"filter,omitempty"`
	ResourceType    string            `json:"resource_type,omitempty"`
	ResourceName    string            `json:"resource_name,omitempty"`
	ResourceLabels  map[string]string `json:"resource_labels,omitempty"`
	MinSeverity     string            `json:"min_severity,omitempty"`
	MaxEntries      int               `json:"max_entries"`
	LookbackMinutes int               `json:"lookback_minutes"`
}

type LogHTTPRequest struct {
	Method       string  `json:"method,omitempty"`
	Status       int     `json:"status,omitempty"`
	LatencyMS    float64 `json:"latency_ms,omitempty"`
	RequestSize  int64   `json:"request_size_bytes,omitempty"`
	ResponseSize int64   `json:"response_size_bytes,omitempty"`
}

type IAMBindingDelta struct {
	Action string `json:"action"`
	Role   string `json:"role,omitempty"`
	Member string `json:"member,omitempty"`
}

type AuditLogDetails struct {
	Category       string            `json:"category"`
	ServiceName    string            `json:"service_name,omitempty"`
	MethodName     string            `json:"method_name,omitempty"`
	ResourceName   string            `json:"resource_name,omitempty"`
	PrincipalEmail string            `json:"principal_email,omitempty"`
	Succeeded      bool              `json:"succeeded"`
	StatusCode     int32             `json:"status_code,omitempty"`
	StatusMessage  string            `json:"status_message,omitempty"`
	ChangedFields  []string          `json:"changed_fields,omitempty"`
	BindingDeltas  []IAMBindingDelta `json:"binding_deltas,omitempty"`
}

type LogEntry struct {
	Timestamp      string            `json:"timestamp"`
	Severity       string            `json:"severity"`
	Message        string            `json:"message"`
	PayloadType    string            `json:"payload_type,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	ResourceType   string            `json:"resource_type,omitempty"`
	ResourceLabels map[string]string `json:"resource_labels,omitempty"`
	LogName        string            `json:"log_name,omitempty"`
	InsertID       string            `json:"insert_id,omitempty"`
	Trace          string            `json:"trace,omitempty"`
	SpanID         string            `json:"span_id,omitempty"`
	TraceSampled   bool              `json:"trace_sampled,omitempty"`
	HTTPRequest    *LogHTTPRequest   `json:"http_request,omitempty"`
	Audit          *AuditLogDetails  `json:"audit,omitempty"`
}

type QueryRecentLogsResponse struct {
	Entries       []LogEntry `json:"entries"`
	TotalFetched  int        `json:"total_fetched"`
	Truncated     bool       `json:"truncated"`
	AppliedFilter string     `json:"applied_filter"`
}
