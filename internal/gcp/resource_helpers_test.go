package gcp

import (
	"slices"
	"testing"

	eventarcpb "cloud.google.com/go/eventarc/apiv1/eventarcpb"
	schedulerpb "cloud.google.com/go/scheduler/apiv1/schedulerpb"
)

func TestRegionalResourceNameParsers(t *testing.T) {
	type parser func(string) (string, string)
	for _, tc := range []struct {
		name       string
		parse      parser
		full       string
		wantRegion string
		wantName   string
	}{
		{"scheduler", parseSchedulerResourceName, "projects/p/locations/us-central1/jobs/nightly", "us-central1", "nightly"},
		{"tasks", parseTaskQueueResourceName, "projects/p/locations/europe-west1/queues/emails", "europe-west1", "emails"},
		{"workflows", parseWorkflowResourceName, "projects/p/locations/asia-east1/workflows/etl", "asia-east1", "etl"},
		{"eventarc", parseEventarcResourceName, "projects/p/locations/us-east1/triggers/on-upload", "us-east1", "on-upload"},
		{"vpcaccess", parseVPCConnectorResourceName, "projects/p/locations/us-west1/connectors/egress", "us-west1", "egress"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			region, name := tc.parse(tc.full)
			if region != tc.wantRegion || name != tc.wantName {
				t.Fatalf("parse(%q) = (%q, %q), want (%q, %q)", tc.full, region, name, tc.wantRegion, tc.wantName)
			}
			// Anything that is not the exact resource shape is returned
			// unchanged as the name, with no region.
			for _, malformed := range []string{"bare-name", "projects/p/locations/us-central1/other/x", "projects/p/locations/us-central1"} {
				if region, name := tc.parse(malformed); region != "" || name != malformed {
					t.Errorf("parse(%q) = (%q, %q), want (\"\", input)", malformed, region, name)
				}
			}
		})
	}
}

func TestParseWorkflowExecutionResourceName(t *testing.T) {
	workflow, name := parseWorkflowExecutionResourceName("projects/p/locations/us-central1/workflows/etl/executions/run-1")
	if workflow != "etl" || name != "run-1" {
		t.Fatalf("got (%q, %q)", workflow, name)
	}
	if workflow, name := parseWorkflowExecutionResourceName("run-1"); workflow != "" || name != "run-1" {
		t.Fatalf("malformed input: got (%q, %q)", workflow, name)
	}
}

func TestParseSecretResourceName(t *testing.T) {
	if got := parseSecretResourceName("projects/p/secrets/db-password"); got != "db-password" {
		t.Fatalf("got %q", got)
	}
	for _, input := range []string{"db-password", "projects/p/secrets/db-password/versions/1"} {
		if got := parseSecretResourceName(input); got != input {
			t.Errorf("parseSecretResourceName(%q) = %q, want input unchanged", input, got)
		}
	}
}

func TestLocationFromNameHelpers(t *testing.T) {
	for _, fn := range []func(string) string{locationFromResourceName, locationFromName} {
		if got := fn("projects/p/locations/us-central1/instances/db"); got != "us-central1" {
			t.Errorf("location = %q, want us-central1", got)
		}
		for _, input := range []string{"projects/p/instances/db", "projects/p/locations", ""} {
			if got := fn(input); got != "" {
				t.Errorf("location(%q) = %q, want empty", input, got)
			}
		}
	}
}

func TestScopeAndZoneHelpers(t *testing.T) {
	if got := regionFromScopeKey("regions/us-central1"); got != "us-central1" {
		t.Errorf("regionFromScopeKey = %q", got)
	}
	if got := regionFromScopeKey("global"); got != "" {
		t.Errorf("regionFromScopeKey(global) = %q, want empty", got)
	}
	if got := zoneFromScopeKey("zones/us-central1-a"); got != "us-central1-a" {
		t.Errorf("zoneFromScopeKey = %q", got)
	}
	for zone, want := range map[string]string{"us-central1-a": "us-central1", "europe-west4-b": "europe-west4", "global": "global", "-a": "-a"} {
		if got := regionFromZone(zone); got != want {
			t.Errorf("regionFromZone(%q) = %q, want %q", zone, got, want)
		}
	}
}

func TestMatchesTagFilter(t *testing.T) {
	const tag = "123456789/environment/production"
	for _, tc := range []struct {
		namespaced, key, value string
		want                   bool
	}{
		{tag, "environment", "production", true},
		{tag, "environment", "", true},
		{tag, "environment", "staging", false},
		{tag, "team", "", false},
		{"", "environment", "", false},
		{tag, "", "", false},
		{"production", "environment", "", false},
	} {
		if got := matchesTagFilter(tc.namespaced, tc.key, tc.value); got != tc.want {
			t.Errorf("matchesTagFilter(%q, %q, %q) = %v, want %v", tc.namespaced, tc.key, tc.value, got, tc.want)
		}
	}
}

func TestAppendUnique(t *testing.T) {
	got := appendUnique(appendUnique(appendUnique(nil, "a"), "b"), "a")
	if !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("appendUnique = %v", got)
	}
}

func TestResolveSchedulerTarget(t *testing.T) {
	for _, tc := range []struct {
		job      *schedulerpb.Job
		kind     string
		ref      string
		scenario string
	}{
		{&schedulerpb.Job{Target: &schedulerpb.Job_HttpTarget{HttpTarget: &schedulerpb.HttpTarget{Uri: "https://api.example/run"}}}, "http", "https://api.example/run", "http"},
		{&schedulerpb.Job{Target: &schedulerpb.Job_PubsubTarget{PubsubTarget: &schedulerpb.PubsubTarget{TopicName: "projects/p/topics/t"}}}, "pubsub", "projects/p/topics/t", "pubsub"},
		{&schedulerpb.Job{Target: &schedulerpb.Job_AppEngineHttpTarget{AppEngineHttpTarget: &schedulerpb.AppEngineHttpTarget{RelativeUri: "/tasks/run"}}}, "app_engine", "/tasks/run", "app engine"},
		{&schedulerpb.Job{}, "unknown", "", "no target"},
	} {
		if kind, ref := resolveSchedulerTarget(tc.job); kind != tc.kind || ref != tc.ref {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", tc.scenario, kind, ref, tc.kind, tc.ref)
		}
	}
}

func TestResolveEventarcDestination(t *testing.T) {
	for _, tc := range []struct {
		dest     *eventarcpb.Destination
		kind     string
		urn      string
		scenario string
	}{
		{nil, "unknown", "", "nil"},
		{&eventarcpb.Destination{Descriptor_: &eventarcpb.Destination_CloudRun{CloudRun: &eventarcpb.CloudRun{Service: "api", Region: "us-central1"}}}, "cloud_run_service", "api", "cloud run"},
		{&eventarcpb.Destination{Descriptor_: &eventarcpb.Destination_CloudFunction{CloudFunction: "projects/p/locations/l/functions/f"}}, "cloud_function", "projects/p/locations/l/functions/f", "function"},
		{&eventarcpb.Destination{Descriptor_: &eventarcpb.Destination_Workflow{Workflow: "projects/p/locations/l/workflows/w"}}, "workflow", "projects/p/locations/l/workflows/w", "workflow"},
		{&eventarcpb.Destination{Descriptor_: &eventarcpb.Destination_Gke{Gke: &eventarcpb.GKE{Cluster: "c", Namespace: "ns", Service: "svc"}}}, "gke", "c/ns/svc", "gke"},
		{&eventarcpb.Destination{Descriptor_: &eventarcpb.Destination_HttpEndpoint{HttpEndpoint: &eventarcpb.HttpEndpoint{Uri: "https://hook.example"}}}, "http_endpoint", "https://hook.example", "http endpoint"},
		{&eventarcpb.Destination{}, "unknown", "", "empty"},
	} {
		if kind, urn := resolveEventarcDestination(tc.dest, "p"); kind != tc.kind || urn != tc.urn {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", tc.scenario, kind, urn, tc.kind, tc.urn)
		}
	}
}
