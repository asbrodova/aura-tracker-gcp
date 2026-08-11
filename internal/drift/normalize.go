package drift

import (
	"encoding/json"
	"sort"
	"strings"
)

var volatileFields = map[string]bool{
	"create_time": true, "created": true, "creator": true,
	"delete_time": true, "endpoint": true, "etag": true,
	"host": true, "ip_address": true, "last_modified": true,
	"latest_execution": true, "latest_revision": true,
	"num_rows": true, "observed_generation": true,
	"ready": true, "ready_replicas": true, "reconciling": true,
	"revision_id": true, "self_link": true, "size_bytes": true,
	"state": true, "status": true, "subscription_count": true,
	"terminal_condition": true, "unique_id": true,
	"update_time": true, "url": true,
}

var volatileMapKeys = map[string]bool{
	"run.googleapis.com/client-name":              true,
	"run.googleapis.com/client-version":           true,
	"run.googleapis.com/creator":                  true,
	"run.googleapis.com/ingress-status":           true,
	"run.googleapis.com/lastmodifier":             true,
	"run.googleapis.com/operation-id":             true,
	"run.googleapis.com/urls":                     true,
	"serving.knative.dev/configurationgeneration": true,
}

var orderedArrays = map[string]bool{
	"args": true, "command": true, "commands": true,
}

func normalizeConfig(value map[string]any, projectID string) map[string]any {
	normalized, _ := normalizeValue(value, "", projectID).(map[string]any)
	if normalized == nil {
		return map[string]any{}
	}
	return normalized
}

func normalizeValue(value any, field, projectID string) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			lowerKey := strings.ToLower(key)
			if volatileFields[lowerKey] || volatileMapKeys[lowerKey] {
				continue
			}
			normalized := normalizeValue(child, key, projectID)
			if isEmptyCollection(normalized) {
				continue
			}
			out[key] = normalized
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, child := range typed {
			out = append(out, normalizeValue(child, field, projectID))
		}
		if !orderedArrays[strings.ToLower(field)] {
			sort.SliceStable(out, func(i, j int) bool {
				return arraySortKey(out[i]) < arraySortKey(out[j])
			})
		}
		return out
	case string:
		if projectID == "" {
			return typed
		}
		return strings.ReplaceAll(typed, projectID, "${PROJECT}")
	default:
		return value
	}
}

func arraySortKey(value any) string {
	if object, ok := value.(map[string]any); ok {
		for _, key := range []string{"name", "id", "type", "kind", "path", "tag", "host"} {
			if identity, ok := object[key].(string); ok && identity != "" {
				return key + ":" + identity + "\x00" + canonicalJSON(value)
			}
		}
	}
	return canonicalJSON(value)
}

func isEmptyCollection(value any) bool {
	switch typed := value.(type) {
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}

func canonicalJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func configMap(value any) map[string]any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}
