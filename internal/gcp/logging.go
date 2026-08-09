package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/logging/logadmin"
	"google.golang.org/api/iterator"
	auditpb "google.golang.org/genproto/googleapis/cloud/audit"
	iamloggingpb "google.golang.org/genproto/googleapis/iam/v1/logging"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

const (
	maxLoggingFilterLength = 4096
	maxLogMessageRunes     = 8192
)

var (
	loggingLabelKeyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	validSeverities   = map[string]bool{
		"DEFAULT": true, "DEBUG": true, "INFO": true, "NOTICE": true,
		"WARNING": true, "ERROR": true, "CRITICAL": true, "ALERT": true,
		"EMERGENCY": true,
	}
	resourceNameLabel = map[string]string{
		"cloud_run_revision":  "service_name",
		"cloud_run_job":       "job_name",
		"k8s_cluster":         "cluster_name",
		"k8s_container":       "cluster_name",
		"cloud_function":      "function_name",
		"pubsub_topic":        "topic_id",
		"pubsub_subscription": "subscription_id",
	}
)

func (a *gcpAdapter) QueryRecentLogs(ctx context.Context, req models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error) {
	if err := a.rateWait(ctx, "logging.QueryRecentLogs"); err != nil {
		return models.QueryRecentLogsResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	if req.LookbackMinutes <= 0 {
		req.LookbackMinutes = 60
	}
	if req.LookbackMinutes > 1440 {
		return models.QueryRecentLogsResponse{}, fmt.Errorf("logging.QueryRecentLogs: lookback_minutes must be at most 1440")
	}
	if req.MaxEntries <= 0 {
		req.MaxEntries = 50
	}
	if req.MaxEntries > 500 {
		return models.QueryRecentLogsResponse{}, fmt.Errorf("logging.QueryRecentLogs: max_entries must be at most 500")
	}

	filter, err := buildLogFilter(req, time.Now().UTC())
	if err != nil {
		return models.QueryRecentLogsResponse{}, fmt.Errorf("logging.QueryRecentLogs: %w", err)
	}

	it := a.logAdmin.Entries(ctx,
		logadmin.ProjectIDs([]string{req.ProjectID}),
		logadmin.Filter(filter),
		logadmin.NewestFirst(),
		logadmin.PageSize(int32(req.MaxEntries+1)), // fetch one extra to detect truncation
	)

	var entries []models.LogEntry
	truncated := false
	for {
		entry, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return models.QueryRecentLogsResponse{}, wrapGCPError("logging.QueryRecentLogs", err)
		}

		if len(entries) >= req.MaxEntries {
			truncated = true
			break
		}

		msg, payloadType := formatLogPayload(entry.Payload)
		auditDetails := auditLogDetails(entry.Payload)
		if auditDetails != nil {
			msg = strings.TrimSpace(auditDetails.MethodName + " " + auditDetails.ResourceName)
		}
		severity := entry.Severity.String()
		ts := ""
		if !entry.Timestamp.IsZero() {
			ts = entry.Timestamp.UTC().Format(time.RFC3339)
		}

		out := models.LogEntry{
			Timestamp:    ts,
			Severity:     severity,
			Message:      msg,
			PayloadType:  payloadType,
			Labels:       entry.Labels,
			LogName:      entry.LogName,
			InsertID:     entry.InsertID,
			Trace:        entry.Trace,
			SpanID:       entry.SpanID,
			TraceSampled: entry.TraceSampled,
			Audit:        auditDetails,
		}
		if entry.Resource != nil {
			out.ResourceType = entry.Resource.Type
			out.ResourceLabels = entry.Resource.Labels
		}
		if entry.HTTPRequest != nil {
			httpReq := &models.LogHTTPRequest{
				Status:       entry.HTTPRequest.Status,
				LatencyMS:    float64(entry.HTTPRequest.Latency) / float64(time.Millisecond),
				RequestSize:  entry.HTTPRequest.RequestSize,
				ResponseSize: entry.HTTPRequest.ResponseSize,
			}
			if entry.HTTPRequest.Request != nil {
				httpReq.Method = entry.HTTPRequest.Request.Method
			}
			out.HTTPRequest = httpReq
		}
		entries = append(entries, out)
	}
	if entries == nil {
		entries = []models.LogEntry{}
	}
	return models.QueryRecentLogsResponse{
		Entries:       entries,
		TotalFetched:  len(entries),
		Truncated:     truncated,
		AppliedFilter: filter,
	}, nil
}

func buildLogFilter(req models.QueryRecentLogsRequest, now time.Time) (string, error) {
	if len(req.Filter) > maxLoggingFilterLength {
		return "", fmt.Errorf("filter exceeds %d bytes", maxLoggingFilterLength)
	}

	parts := make([]string, 0, 6)
	if strings.TrimSpace(req.Filter) != "" {
		parts = append(parts, "("+strings.TrimSpace(req.Filter)+")")
	} else {
		if req.ResourceType != "" {
			parts = append(parts, `resource.type="`+escapeLoggingString(req.ResourceType)+`"`)
		}

		labels := make(map[string]string, len(req.ResourceLabels)+1)
		for key, value := range req.ResourceLabels {
			labels[key] = value
		}
		if req.ResourceName != "" {
			label, ok := resourceNameLabel[req.ResourceType]
			if !ok {
				return "", fmt.Errorf("resource_name is not supported for resource_type %q; pass resource_labels instead", req.ResourceType)
			}
			if existing, ok := labels[label]; ok && existing != req.ResourceName {
				return "", fmt.Errorf("resource_name conflicts with resource_labels.%s", label)
			}
			labels[label] = req.ResourceName
		}
		for _, key := range sortedMapKeys(labels) {
			if !loggingLabelKeyRE.MatchString(key) {
				return "", fmt.Errorf("invalid resource label key %q", key)
			}
			parts = append(parts, `resource.labels.`+key+`="`+escapeLoggingString(labels[key])+`"`)
		}
	}

	severity := strings.ToUpper(strings.TrimSpace(req.MinSeverity))
	if severity == "" && strings.TrimSpace(req.Filter) == "" {
		severity = "WARNING"
	}
	if severity != "" {
		if !validSeverities[severity] {
			return "", fmt.Errorf("invalid min_severity %q", req.MinSeverity)
		}
		parts = append(parts, `severity>="`+severity+`"`)
	}

	since := now.Add(-time.Duration(req.LookbackMinutes) * time.Minute)
	parts = append(parts, `timestamp>="`+since.Format(time.RFC3339)+`"`)
	return strings.Join(parts, " AND "), nil
}

func escapeLoggingString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func formatLogPayload(payload any) (string, string) {
	if payload == nil {
		return "", "nil"
	}

	var message string
	switch value := payload.(type) {
	case string:
		message = value
	case []byte:
		message = string(value)
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			message = fmt.Sprintf("%v", value)
		} else {
			message = string(encoded)
		}
	}

	runes := []rune(message)
	if len(runes) > maxLogMessageRunes {
		message = string(runes[:maxLogMessageRunes]) + "…"
	}
	return message, reflect.TypeOf(payload).String()
}

func auditLogDetails(payload any) *models.AuditLogDetails {
	auditLog, ok := payload.(*auditpb.AuditLog)
	if !ok || auditLog == nil {
		return nil
	}
	details := &models.AuditLogDetails{
		ServiceName:  auditLog.ServiceName,
		MethodName:   auditLog.MethodName,
		ResourceName: auditLog.ResourceName,
		Succeeded:    auditLog.Status == nil || auditLog.Status.Code == 0,
	}
	if auditLog.AuthenticationInfo != nil {
		details.PrincipalEmail = auditLog.AuthenticationInfo.PrincipalEmail
	}
	if auditLog.Status != nil {
		details.StatusCode = auditLog.Status.Code
		details.StatusMessage = auditLog.Status.Message
	}

	changed := make(map[string]bool)
	if auditLog.Request != nil {
		collectChangedFieldPaths("", auditLog.Request.AsMap(), changed)
	}
	details.ChangedFields = make([]string, 0, len(changed))
	for field := range changed {
		details.ChangedFields = append(details.ChangedFields, field)
	}
	sort.Strings(details.ChangedFields)
	if len(details.ChangedFields) > 40 {
		details.ChangedFields = details.ChangedFields[:40]
	}

	if auditLog.ServiceData != nil {
		var iamData iamloggingpb.AuditData
		if err := auditLog.ServiceData.UnmarshalTo(&iamData); err == nil && iamData.PolicyDelta != nil {
			for _, delta := range iamData.PolicyDelta.BindingDeltas {
				if delta == nil {
					continue
				}
				details.BindingDeltas = append(details.BindingDeltas, models.IAMBindingDelta{
					Action: delta.Action.String(),
					Role:   delta.Role,
					Member: delta.Member,
				})
			}
		}
	}
	if auditLog.Metadata != nil {
		collectBindingDeltas(auditLog.Metadata.AsMap(), &details.BindingDeltas)
	}
	details.Category = auditChangeCategory(details)
	return details
}

func collectChangedFieldPaths(prefix string, value any, fields map[string]bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "@type" {
				continue
			}
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			if (key == "updateMask" || key == "update_mask") && child != nil {
				if mask, ok := child.(string); ok {
					for _, field := range strings.Split(mask, ",") {
						field = strings.TrimSpace(field)
						if field != "" {
							fields["update_mask."+field] = true
						}
					}
				}
			}
			collectChangedFieldPaths(path, child, fields)
		}
	case []any:
		if prefix != "" {
			fields[prefix] = true
		}
		for _, child := range typed {
			if _, ok := child.(map[string]any); ok {
				collectChangedFieldPaths(prefix+"[]", child, fields)
			}
		}
	default:
		if prefix != "" {
			fields[prefix] = true
		}
	}
}

func collectBindingDeltas(value any, deltas *[]models.IAMBindingDelta) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "bindingDeltas" || key == "binding_deltas" {
				if items, ok := child.([]any); ok {
					for _, item := range items {
						if delta, ok := item.(map[string]any); ok {
							*deltas = append(*deltas, models.IAMBindingDelta{
								Action: stringMapValue(delta, "action"),
								Role:   stringMapValue(delta, "role"),
								Member: stringMapValue(delta, "member"),
							})
						}
					}
				}
			}
			collectBindingDeltas(child, deltas)
		}
	case []any:
		for _, child := range typed {
			collectBindingDeltas(child, deltas)
		}
	}
}

func stringMapValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func auditChangeCategory(details *models.AuditLogDetails) string {
	method := strings.ToLower(details.MethodName)
	service := strings.ToLower(details.ServiceName)
	if strings.Contains(method, "setiampolicy") || strings.Contains(service, "iam.googleapis.com") {
		return "iam_change"
	}
	if strings.Contains(service, "secretmanager.googleapis.com") {
		return "secret_change"
	}
	if strings.Contains(service, "run.googleapis.com") {
		for _, field := range details.ChangedFields {
			if strings.Contains(strings.ToLower(field), "traffic") {
				return "traffic_change"
			}
		}
		if strings.Contains(method, "create") || strings.Contains(method, "update") || strings.Contains(method, "replace") || strings.Contains(method, "delete") {
			return "deployment_change"
		}
	}
	return "configuration_change"
}
