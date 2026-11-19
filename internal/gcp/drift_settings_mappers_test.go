package gcp

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	cloudtaskspb "cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	workflowspb "cloud.google.com/go/workflows/apiv1/workflowspb"
	"google.golang.org/api/sqladmin/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestTaskQueueSummaryMapsRateRetryAndLogging(t *testing.T) {
	summary := taskQueueSummary(&cloudtaskspb.Queue{
		Name:       "projects/p/locations/us-central1/queues/emails",
		State:      cloudtaskspb.Queue_RUNNING,
		RateLimits: &cloudtaskspb.RateLimits{MaxDispatchesPerSecond: 5, MaxBurstSize: 10, MaxConcurrentDispatches: 20},
		RetryConfig: &cloudtaskspb.RetryConfig{
			MaxAttempts: -1, MaxRetryDuration: durationpb.New(time.Hour),
			MinBackoff: durationpb.New(100 * time.Millisecond), MaxBackoff: durationpb.New(time.Minute), MaxDoublings: 4,
		},
		StackdriverLoggingConfig: &cloudtaskspb.StackdriverLoggingConfig{SamplingRatio: 0.5},
	}, "emails", "us-central1")
	if summary.State != "RUNNING" || summary.RateLimits == nil || summary.RateLimits.MaxConcurrentDispatches != 20 {
		t.Fatalf("summary = %+v", summary)
	}
	retry := summary.RetryConfig
	if retry == nil || retry.MaxAttempts != -1 || retry.MaxRetryDuration != "1h0m0s" || retry.MinBackoff != "100ms" || retry.MaxDoublings != 4 {
		t.Fatalf("retry = %+v", retry)
	}
	if summary.LoggingSamplingRatio == nil || *summary.LoggingSamplingRatio != 0.5 {
		t.Fatalf("logging = %v", summary.LoggingSamplingRatio)
	}
	if empty := taskQueueSummary(&cloudtaskspb.Queue{}, "q", "r"); empty.RateLimits != nil || empty.RetryConfig != nil || empty.LoggingSamplingRatio != nil {
		t.Fatalf("unset settings were invented: %+v", empty)
	}
}

func TestWorkflowSummaryHashesSourceAndOmitsEnvValues(t *testing.T) {
	source := "main:\n  steps:\n    - done:\n        return: ok\n"
	summary := workflowSummary(&workflowspb.Workflow{
		Name: "projects/p/locations/us-central1/workflows/nightly", State: workflowspb.Workflow_ACTIVE,
		ServiceAccount: "projects/p/serviceAccounts/wf@p.iam.gserviceaccount.com",
		SourceCode:     &workflowspb.Workflow_SourceContents{SourceContents: source},
		CallLogLevel:   workflowspb.Workflow_LOG_ERRORS_ONLY,
		UserEnvVars:    map[string]string{"API_TOKEN": "super-secret", "MODE": "prod"},
	}, "nightly", "us-central1", "")
	sum := sha256.Sum256([]byte(source))
	if summary.SourceSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("source hash = %q", summary.SourceSHA256)
	}
	if strings.Join(summary.UserEnvVarNames, ",") != "API_TOKEN,MODE" || summary.CallLogLevel != "LOG_ERRORS_ONLY" {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.ExecutionHistoryLevel != "" {
		t.Fatalf("unspecified history level reported as %q", summary.ExecutionHistoryLevel)
	}
	if strings.Contains(strings.Join([]string{summary.Description, summary.SourceSHA256, strings.Join(summary.UserEnvVarNames, "")}, " "), "super-secret") {
		t.Fatal("an environment variable value leaked into the summary")
	}
}

func TestSQLInstanceSettingsSummarizesNetworkAndFlags(t *testing.T) {
	autoResize := true
	settings := sqlInstanceSettings(&sqladmin.Settings{
		AvailabilityType: "REGIONAL", Edition: "ENTERPRISE_PLUS", DataDiskSizeGb: 512, StorageAutoResize: &autoResize,
		DeletionProtectionEnabled: true,
		BackupConfiguration: &sqladmin.BackupConfiguration{Enabled: true, PointInTimeRecoveryEnabled: true,
			BackupRetentionSettings: &sqladmin.BackupRetentionSettings{RetainedBackups: 14, RetentionUnit: "COUNT"}},
		IpConfiguration: &sqladmin.IpConfiguration{Ipv4Enabled: true, SslMode: "ENCRYPTED_ONLY",
			AuthorizedNetworks: []*sqladmin.AclEntry{{Value: "10.0.0.0/8"}, {Value: "0.0.0.0/0"}}},
		MaintenanceWindow: &sqladmin.MaintenanceWindow{Day: 7, Hour: 3, UpdateTrack: "stable"},
		DatabaseFlags:     []*sqladmin.DatabaseFlags{{Name: "max_connections", Value: "200"}},
		InsightsConfig:    &sqladmin.InsightsConfig{QueryInsightsEnabled: true},
	})
	if settings == nil || settings.AvailabilityType != "REGIONAL" || !settings.DeletionProtection || !settings.QueryInsightsEnabled {
		t.Fatalf("settings = %+v", settings)
	}
	if settings.Backup == nil || !settings.Backup.PointInTimeRecovery || settings.Backup.RetainedBackups != 14 {
		t.Fatalf("backup = %+v", settings.Backup)
	}
	if settings.Network == nil || settings.Network.AuthorizedNetworkCount != 2 || !settings.Network.OpenToInternet || settings.Network.SSLMode != "ENCRYPTED_ONLY" {
		t.Fatalf("network = %+v", settings.Network)
	}
	if settings.DatabaseFlags["max_connections"] != "200" || settings.MaintenanceWindow.Day != 7 {
		t.Fatalf("flags/maintenance = %+v / %+v", settings.DatabaseFlags, settings.MaintenanceWindow)
	}
	if sqlInstanceSettings(nil) != nil {
		t.Fatal("nil settings were invented")
	}
}
