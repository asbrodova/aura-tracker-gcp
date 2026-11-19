package mcp

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/internal/costreasoning"
	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// deprecatedProjectArguments lists tools that still accept the pre-project_id
// "project" argument for older clients. New tools must use project_id.
var deprecatedProjectArguments = map[string]bool{"gcp_get_service_topology": true}

var timeUnitSuffix = regexp.MustCompile(`_(seconds|minutes|hours|days)$`)

// Parameter names are part of the contract with the model, so they follow one
// convention: a project is selected with project_id, and every duration says
// its unit in the name and in the description.
func TestToolParametersFollowNamingConventions(t *testing.T) {
	s := New(&mockSvc{}, slog.Default(), "test",
		WithMutations(), WithRecommenderExport("recommendations"), WithCostReasoning(costreasoning.Config{}))
	for name, tool := range s.ListTools() {
		for property, raw := range tool.Tool.InputSchema.Properties {
			prop, _ := raw.(map[string]any)
			description, _ := prop["description"].(string)

			if property == "project" && !deprecatedProjectArguments[name] {
				t.Errorf("%s: selects a project with %q; use project_id", name, property)
			}
			if property == "project" && !strings.HasPrefix(description, "Deprecated") {
				// The environment wrapper rewrites project descriptions only
				// when environments are configured, so this is the tool's own.
				t.Errorf("%s: the deprecated project argument must say so in its description", name)
			}

			kind, _ := prop["type"].(string)
			if kind != "number" && kind != "integer" {
				continue
			}
			lower := strings.ToLower(property)
			if strings.Contains(lower, "lookback") || strings.Contains(lower, "window") ||
				strings.Contains(lower, "baseline") || strings.Contains(lower, "period") {
				if !timeUnitSuffix.MatchString(property) {
					t.Errorf("%s: duration parameter %q must end in _seconds, _minutes, _hours or _days", name, property)
					continue
				}
			}
			if match := timeUnitSuffix.FindStringSubmatch(property); match != nil {
				unit := strings.TrimSuffix(match[1], "s")
				if !strings.Contains(strings.ToLower(description), unit) {
					t.Errorf("%s: %q description %q does not name its unit (%s)", name, property, description, match[1])
				}
			}
		}
	}
}

type topologyCaptureSvc struct {
	*mockSvc
	projects []string
}

func (s *topologyCaptureSvc) GetServiceTopology(_ context.Context, req models.GetServiceTopologyRequest) (models.ServiceTopologyReport, error) {
	s.projects = append(s.projects, req.EffectiveProjectID())
	return models.ServiceTopologyReport{ProjectID: req.EffectiveProjectID(), Project: req.EffectiveProjectID(), RootService: req.ServiceName}, nil
}

// project_id and the deprecated project argument must route to the same
// configured environment, and an omitted one must not silently pick the
// default environment while the other names a different one.
func TestTopologyProjectAliasesRouteToTheNamedEnvironment(t *testing.T) {
	svc := &topologyCaptureSvc{mockSvc: &mockSvc{}}
	s := New(svc, slog.Default(), "test",
		WithModules(map[string]bool{ModuleTopology: true}),
		WithEnvironments(testEnvironmentRegistry(t)),
	)
	call := func(args string) string {
		return handleJSON(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"gcp_get_service_topology","arguments":{"region":"us-central1","service_name":"api"`+args+`}}}`)
	}

	for _, args := range []string{`,"project_id":"PROD"`, `,"project":"prod"`, `,"project_id":"prod","project":"private-prod-345"`} {
		response := call(args)
		if strings.Contains(response, "private-prod-345") || strings.Contains(response, `"isError":true`) {
			t.Fatalf("args %s: response leaked the project ID or failed: %s", args, response)
		}
	}
	call(``)
	want := []string{"private-prod-345", "private-prod-345", "private-prod-345", "private-dev-123"}
	if strings.Join(svc.projects, ",") != strings.Join(want, ",") {
		t.Fatalf("service saw projects %v, want %v", svc.projects, want)
	}

	response := call(`,"project_id":"prod","project":"dev"`)
	if !strings.Contains(response, "same configured environment") {
		t.Fatalf("conflicting selectors were not refused: %s", response)
	}
	if len(svc.projects) != len(want) {
		t.Fatalf("service was called for conflicting selectors: %v", svc.projects)
	}
}
