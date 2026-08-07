package gcp

import (
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/iam/apiv1/iampb"
	auditpb "google.golang.org/genproto/googleapis/cloud/audit"
	iamloggingpb "google.golang.org/genproto/googleapis/iam/v1/logging"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func TestBuildLogFilterCloudRunResourceName(t *testing.T) {
	now := time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC)
	filter, err := buildLogFilter(models.QueryRecentLogsRequest{
		ResourceType:    "cloud_run_revision",
		ResourceName:    "payments-api",
		ResourceLabels:  map[string]string{"location": "us-central1"},
		MinSeverity:     "error",
		LookbackMinutes: 120,
	}, now)
	if err != nil {
		t.Fatalf("buildLogFilter: %v", err)
	}
	want := `resource.type="cloud_run_revision" AND resource.labels.location="us-central1" AND resource.labels.service_name="payments-api" AND severity>="ERROR" AND timestamp>="2026-08-07T08:00:00Z"`
	if filter != want {
		t.Fatalf("filter mismatch\n got: %s\nwant: %s", filter, want)
	}
}

func TestBuildLogFilterNativeDoesNotAddDefaultSeverity(t *testing.T) {
	now := time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC)
	filter, err := buildLogFilter(models.QueryRecentLogsRequest{
		Filter:          `logName:"cloudaudit.googleapis.com%2Factivity"`,
		LookbackMinutes: 60,
	}, now)
	if err != nil {
		t.Fatalf("buildLogFilter: %v", err)
	}
	if strings.Contains(filter, "severity") {
		t.Fatalf("native filter unexpectedly received default severity: %s", filter)
	}
	if !strings.Contains(filter, `timestamp>="2026-08-07T09:00:00Z"`) {
		t.Fatalf("native filter missing bounded timestamp: %s", filter)
	}
}

func TestBuildLogFilterRejectsInvalidStructuredInput(t *testing.T) {
	tests := []models.QueryRecentLogsRequest{
		{ResourceType: "unknown", ResourceName: "name", LookbackMinutes: 60},
		{ResourceLabels: map[string]string{"bad.key": "value"}, LookbackMinutes: 60},
		{MinSeverity: "loud", LookbackMinutes: 60},
	}
	for _, req := range tests {
		if _, err := buildLogFilter(req, time.Now()); err == nil {
			t.Fatalf("expected error for request: %+v", req)
		}
	}
}

func TestFormatLogPayloadJSONAndTruncation(t *testing.T) {
	message, payloadType := formatLogPayload(map[string]any{"message": "boom", "code": 500})
	if !strings.Contains(message, `"message":"boom"`) || !strings.Contains(message, `"code":500`) {
		t.Fatalf("unexpected JSON payload: %s", message)
	}
	if payloadType == "" {
		t.Fatal("payload type must be present")
	}

	message, _ = formatLogPayload(strings.Repeat("x", maxLogMessageRunes+20))
	if len([]rune(message)) != maxLogMessageRunes+1 || !strings.HasSuffix(message, "…") {
		t.Fatalf("payload was not bounded correctly: %d runes", len([]rune(message)))
	}
}

func TestAuditLogDetailsDeploymentAndChangedFields(t *testing.T) {
	request, err := structpb.NewStruct(map[string]any{
		"updateMask": "template.containers,traffic",
		"service": map[string]any{
			"traffic": []any{map[string]any{"revision": "payments-00042", "percent": 100}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	details := auditLogDetails(&auditpb.AuditLog{
		ServiceName:  "run.googleapis.com",
		MethodName:   "google.cloud.run.v2.Services.UpdateService",
		ResourceName: "projects/proj/locations/us-central1/services/payments",
		AuthenticationInfo: &auditpb.AuthenticationInfo{
			PrincipalEmail: "deployer@example.com",
		},
		Request: request,
	})
	if details == nil || details.Category != "traffic_change" || !details.Succeeded {
		t.Fatalf("unexpected audit details: %+v", details)
	}
	if details.PrincipalEmail != "deployer@example.com" || len(details.ChangedFields) == 0 {
		t.Fatalf("missing audit evidence: %+v", details)
	}
}

func TestAuditLogDetailsIAMBindingDelta(t *testing.T) {
	serviceData, err := anypb.New(&iamloggingpb.AuditData{PolicyDelta: &iampb.PolicyDelta{
		BindingDeltas: []*iampb.BindingDelta{{
			Action: iampb.BindingDelta_ADD,
			Role:   "roles/run.invoker",
			Member: "serviceAccount:caller@example.com",
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	details := auditLogDetails(&auditpb.AuditLog{
		ServiceName: "run.googleapis.com",
		MethodName:  "google.cloud.run.v2.Services.SetIamPolicy",
		ServiceData: serviceData,
	})
	if details == nil || details.Category != "iam_change" || len(details.BindingDeltas) != 1 {
		t.Fatalf("unexpected IAM audit details: %+v", details)
	}
	if details.BindingDeltas[0].Action != "ADD" || details.BindingDeltas[0].Role != "roles/run.invoker" {
		t.Fatalf("unexpected binding delta: %+v", details.BindingDeltas[0])
	}
}
