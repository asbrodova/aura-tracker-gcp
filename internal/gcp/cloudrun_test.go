package gcp

import (
	"strings"
	"testing"
	"time"

	runpb "cloud.google.com/go/run/apiv2/runpb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRevisionSummaryFromProtoIsSafeAndUseful(t *testing.T) {
	revision := &runpb.Revision{
		Name:                          "projects/proj/locations/us-central1/services/payments/revisions/payments-00042",
		CreateTime:                    timestamppb.New(time.Date(2026, time.August, 7, 9, 30, 0, 0, time.UTC)),
		Creator:                       "deployer@example.com",
		ServiceAccount:                "runtime@proj.iam.gserviceaccount.com",
		MaxInstanceRequestConcurrency: 80,
		Timeout:                       durationpb.New(30 * time.Second),
		Scaling:                       &runpb.RevisionScaling{MinInstanceCount: 1, MaxInstanceCount: 20},
		VpcAccess:                     &runpb.VpcAccess{Connector: "projects/proj/locations/us-central1/connectors/prod", Egress: runpb.VpcAccess_ALL_TRAFFIC},
		Containers: []*runpb.Container{{
			Name:  "app",
			Image: "us-docker.pkg.dev/proj/app/payments@sha256:abc",
			Env: []*runpb.EnvVar{
				{Name: "API_TOKEN", Values: &runpb.EnvVar_Value{Value: "never-return-this"}},
				{Name: "DB_PASSWORD", Values: &runpb.EnvVar_ValueSource{ValueSource: &runpb.EnvVarSource{SecretKeyRef: &runpb.SecretKeySelector{Secret: "db-password", Version: "7"}}}},
			},
		}},
		Conditions: []*runpb.Condition{{Type: "Ready", State: runpb.Condition_CONDITION_SUCCEEDED}},
	}

	summary := revisionSummaryFromProto(revision)
	if summary.Name != "payments-00042" || summary.ServiceName != "payments" || summary.Region != "us-central1" {
		t.Fatalf("unexpected identity: %+v", summary)
	}
	if !summary.Ready || summary.TimeoutSeconds != 30 || summary.ConfigFingerprint == "" {
		t.Fatalf("unexpected operational fields: %+v", summary)
	}
	if len(summary.Containers) != 1 || len(summary.Containers[0].EnvironmentNames) != 2 {
		t.Fatalf("unexpected container summary: %+v", summary.Containers)
	}
	if strings.Contains(summary.ConfigFingerprint, "never-return-this") {
		t.Fatal("configuration value leaked through fingerprint")
	}
}

func TestRevisionConfigFingerprintDetectsValueChangeButIgnoresEnvOrder(t *testing.T) {
	envA := &runpb.EnvVar{Name: "A", Values: &runpb.EnvVar_Value{Value: "one"}}
	envB := &runpb.EnvVar{Name: "B", Values: &runpb.EnvVar_Value{Value: "two"}}
	first := &runpb.Revision{Containers: []*runpb.Container{{Image: "image", Env: []*runpb.EnvVar{envA, envB}}}}
	reordered := &runpb.Revision{Containers: []*runpb.Container{{Image: "image", Env: []*runpb.EnvVar{envB, envA}}}}
	changed := &runpb.Revision{Containers: []*runpb.Container{{Image: "image", Env: []*runpb.EnvVar{
		{Name: "A", Values: &runpb.EnvVar_Value{Value: "changed"}}, envB,
	}}}}

	if revisionConfigFingerprint(first) != revisionConfigFingerprint(reordered) {
		t.Fatal("environment ordering should not change the fingerprint")
	}
	if revisionConfigFingerprint(first) == revisionConfigFingerprint(changed) {
		t.Fatal("environment value change should change the fingerprint")
	}
}

func TestParseRevisionResourceName(t *testing.T) {
	region, service, revision := parseRevisionResourceName("projects/p/locations/europe-west1/services/api/revisions/api-00003")
	if region != "europe-west1" || service != "api" || revision != "api-00003" {
		t.Fatalf("unexpected parse result: %q %q %q", region, service, revision)
	}
}
