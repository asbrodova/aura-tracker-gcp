package gcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// TestNeededClientsForServerlessGraphModule guards the root cause of the
// --modules=serverlessgraph crash: the module had no moduleClientDeps entry,
// so selecting it alone never constructed the clients its collectors call.
func TestNeededClientsForServerlessGraphModule(t *testing.T) {
	t.Parallel()
	needed := neededClients(map[string]bool{"serverlessgraph": true})
	for _, client := range []clientKey{
		clientRunSvc,
		clientRunJobs,
		clientFunctionsV1,
		clientEventarc,
		clientScheduler,
		clientWorkflows,
		clientTasks,
		clientSecretMgr,
		clientVPCAccess,
		clientSQLAdmin,
		clientPubSub,
	} {
		if !needed[client] {
			t.Errorf("serverlessgraph module did not initialize %s", client)
		}
	}
}

// TestAdapterMethodsReturnErrorWhenClientMissing ensures adapter methods
// return a typed error instead of panicking when their SDK client was not
// constructed. Collectors call these from errgroup goroutines, where a panic
// terminates the whole MCP server process.
func TestAdapterMethodsReturnErrorWhenClientMissing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const project = "test-project"
	calls := map[string]func(a *gcpAdapter) error{
		"ListJobs": func(a *gcpAdapter) error {
			_, err := a.ListJobs(ctx, models.ListJobsRequest{ProjectID: project, Region: "-"})
			return err
		},
		"GetJobDetails": func(a *gcpAdapter) error {
			_, err := a.GetJobDetails(ctx, models.GetJobDetailsRequest{ProjectID: project, Region: "us-central1", JobName: "job"})
			return err
		},
		"ListJobExecutions": func(a *gcpAdapter) error {
			_, err := a.ListJobExecutions(ctx, models.ListJobExecutionsRequest{ProjectID: project, Region: "us-central1", JobName: "job", Limit: 5})
			return err
		},
		"ListClusters": func(a *gcpAdapter) error {
			_, err := a.ListClusters(ctx, models.ListClustersRequest{ProjectID: project, Location: "-"})
			return err
		},
		"GetClusterDetails": func(a *gcpAdapter) error {
			_, err := a.GetClusterDetails(ctx, models.GetClusterDetailsRequest{ProjectID: project, Location: "us-central1", ClusterName: "cluster"})
			return err
		},
		"ScaleDeployment": func(a *gcpAdapter) error {
			_, err := a.ScaleDeployment(ctx, models.ScaleDeploymentRequest{ProjectID: project, Location: "us-central1", ClusterName: "cluster", NodePoolName: "pool", NodeCount: 1})
			return err
		},
		"QueryRecentLogs": func(a *gcpAdapter) error {
			_, err := a.QueryRecentLogs(ctx, models.QueryRecentLogsRequest{ProjectID: project, MaxEntries: 10, LookbackMinutes: 10})
			return err
		},
		"ListMetricDescriptors": func(a *gcpAdapter) error {
			_, err := a.ListMetricDescriptors(ctx, models.ListMetricDescriptorsRequest{ProjectID: project})
			return err
		},
		"GetMetrics": func(a *gcpAdapter) error {
			_, err := a.GetMetrics(ctx, models.GetMetricsRequest{ProjectID: project, MetricType: "run.googleapis.com/request_count", LookbackMinutes: 10})
			return err
		},
		"ListLoadBalancers": func(a *gcpAdapter) error {
			_, err := a.ListLoadBalancers(ctx, models.ListLoadBalancersRequest{ProjectID: project})
			return err
		},
		"ListURLMaps": func(a *gcpAdapter) error {
			_, err := a.ListURLMaps(ctx, models.ListURLMapsRequest{ProjectID: project})
			return err
		},
		"ListNEGs": func(a *gcpAdapter) error {
			_, err := a.ListNEGs(ctx, models.ListNEGsRequest{ProjectID: project})
			return err
		},
		"ListAPIGateways": func(a *gcpAdapter) error {
			_, err := a.ListAPIGateways(ctx, models.ListAPIGatewaysRequest{ProjectID: project})
			return err
		},
		"ListVPCNetworks": func(a *gcpAdapter) error {
			_, err := a.ListVPCNetworks(ctx, models.ListVPCNetworksRequest{ProjectID: project})
			return err
		},
		"ListVPCSubnets": func(a *gcpAdapter) error {
			_, err := a.ListVPCSubnets(ctx, models.ListVPCSubnetsRequest{ProjectID: project})
			return err
		},
		"ListPSCEndpoints": func(a *gcpAdapter) error {
			_, err := a.ListPSCEndpoints(ctx, models.ListPSCEndpointsRequest{ProjectID: project})
			return err
		},
		"ListTopics": func(a *gcpAdapter) error {
			_, err := a.ListTopics(ctx, models.ListTopicsRequest{ProjectID: project})
			return err
		},
		"InspectTopicHealth": func(a *gcpAdapter) error {
			_, err := a.InspectTopicHealth(ctx, models.InspectTopicHealthRequest{ProjectID: project, TopicName: "topic"})
			return err
		},
		"ListTaggedResources": func(a *gcpAdapter) error {
			_, err := a.ListTaggedResources(ctx, models.ListTaggedResourcesRequest{ProjectID: project, TagKey: "tagKeys/123"})
			return err
		},
		"ListTraceDependencyEdges": func(a *gcpAdapter) error {
			_, err := a.ListTraceDependencyEdges(ctx, models.ListTraceDependencyEdgesRequest{ProjectID: project})
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s panicked with a nil client: %v", name, r)
				}
			}()
			err := call(&gcpAdapter{callTimeout: time.Second})
			if err == nil {
				t.Fatalf("%s succeeded without an initialized client", name)
			}
			if !strings.Contains(err.Error(), "is not initialized") {
				t.Fatalf("%s error = %q, want a client-not-initialized error", name, err)
			}
		})
	}
}
