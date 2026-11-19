package gcp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/api/alloydb/v1"
	"google.golang.org/api/artifactregistry/v1"
	"google.golang.org/api/cloudbuild/v1"
	"google.golang.org/api/cloudresourcemanager/v1"
	crmv3 "google.golang.org/api/cloudresourcemanager/v3"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/firestore/v1"
	"google.golang.org/api/iam/v1"
	"google.golang.org/api/redis/v1"
	"google.golang.org/api/spanner/v1"
	"google.golang.org/api/sqladmin/v1"
	"google.golang.org/api/vpcaccess/v1"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func must[T any](t *testing.T, value T, err error) T {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

var forbidden = apiError(http.StatusForbidden, "PERMISSION_DENIED", "Permission 'x.y.list' denied on resource")

func TestListSQLInstancesFollowsPagesAndMapsSettings(t *testing.T) {
	fake := newRESTFake(t,
		restRoute{method: "GET", path: "/projects/p/instances", query: map[string]string{"pageToken": "page-2"}, body: map[string]any{
			"items": []any{map[string]any{"name": "replica", "databaseVersion": "POSTGRES_16", "region": "europe-west1", "state": "RUNNABLE"}},
		}},
		restRoute{method: "GET", path: "/projects/p/instances", body: map[string]any{
			"items": []any{map[string]any{"name": "orders", "databaseVersion": "POSTGRES_16", "region": "us-central1", "state": "RUNNABLE",
				"settings": map[string]any{"tier": "db-custom-2-7680", "userLabels": map[string]string{"env": "prod"}}}},
			"nextPageToken": "page-2",
		}},
	)
	a := testAdapter()
	a.sqlAdmin = must(t, sqladmin.NewService(context.Background(), fake.options()...))
	response, err := a.ListSQLInstances(context.Background(), models.ListSQLInstancesRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Instances) != 2 {
		t.Fatalf("instances = %+v, want both pages", response.Instances)
	}
	first := response.Instances[0]
	if first.Name != "orders" || first.Tier != "db-custom-2-7680" || first.Labels["env"] != "prod" || first.Region != "us-central1" {
		t.Fatalf("first instance = %+v", first)
	}
	if response.Instances[1].Tier != "" {
		t.Fatalf("an instance without settings got a tier: %+v", response.Instances[1])
	}
}

func TestListSQLInstancesMapsPermissionDenied(t *testing.T) {
	fake := newRESTFake(t, restRoute{method: "GET", path: "/projects/p/instances", status: http.StatusForbidden, body: forbidden})
	a := testAdapter()
	a.sqlAdmin = must(t, sqladmin.NewService(context.Background(), fake.options()...))
	_, err := a.ListSQLInstances(context.Background(), models.ListSQLInstancesRequest{ProjectID: "p"})
	var denied *PermissionDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("err = %v (%T), want *PermissionDeniedError", err, err)
	}
}

func TestDatastoreListsWithoutClientsReturnEmpty(t *testing.T) {
	a := testAdapter()
	ctx := context.Background()
	if r, err := a.ListSQLInstances(ctx, models.ListSQLInstancesRequest{ProjectID: "p"}); err != nil || r.Instances == nil || len(r.Instances) != 0 {
		t.Errorf("sql = %+v, %v", r, err)
	}
	if r, err := a.ListSpannerInstances(ctx, models.ListSpannerInstancesRequest{ProjectID: "p"}); err != nil || r.Instances == nil || len(r.Instances) != 0 {
		t.Errorf("spanner = %+v, %v", r, err)
	}
	if r, err := a.ListAlloyDBClusters(ctx, models.ListAlloyDBClustersRequest{ProjectID: "p"}); err != nil || r.Clusters == nil || len(r.Clusters) != 0 {
		t.Errorf("alloydb = %+v, %v", r, err)
	}
	if r, err := a.ListFirestoreDatabases(ctx, models.ListFirestoreDatabasesRequest{ProjectID: "p"}); err != nil || r.Databases == nil || len(r.Databases) != 0 {
		t.Errorf("firestore = %+v, %v", r, err)
	}
	if r, err := a.ListMemorystoreInstances(ctx, models.ListMemorystoreInstancesRequest{ProjectID: "p"}); err != nil || r.Instances == nil || len(r.Instances) != 0 {
		t.Errorf("redis = %+v, %v", r, err)
	}
}

func TestDatastoreListsMapResources(t *testing.T) {
	fake := newRESTFake(t,
		restRoute{method: "GET", path: "/projects/p/instances", body: map[string]any{
			"instances": []any{map[string]any{"name": "projects/p/instances/main", "displayName": "Main", "state": "READY", "nodeCount": 3, "config": "projects/p/instanceConfigs/regional-us-central1"}},
		}},
		restRoute{method: "GET", path: "/projects/p/locations/-/clusters", body: map[string]any{
			"clusters": []any{map[string]any{"name": "projects/p/locations/us-east1/clusters/ledger", "state": "READY", "databaseVersion": "POSTGRES_15"}},
		}},
		restRoute{method: "GET", path: "/projects/p/databases", body: map[string]any{
			"databases": []any{map[string]any{"name": "projects/p/databases/(default)", "type": "FIRESTORE_NATIVE", "locationId": "nam5"}},
		}},
		restRoute{method: "GET", path: "/projects/p/locations/europe-west1/instances", body: map[string]any{
			"instances": []any{map[string]any{"name": "projects/p/locations/europe-west1/instances/cache", "tier": "STANDARD_HA", "memorySizeGb": 5, "redisVersion": "REDIS_7_2"}},
		}},
	)
	ctx := context.Background()
	a := testAdapter()
	a.spannerSvc = must(t, spanner.NewService(ctx, fake.options()...))
	a.alloydbSvc = must(t, alloydb.NewService(ctx, fake.options()...))
	a.firestoreSvc = must(t, firestore.NewService(ctx, fake.options()...))
	a.redisSvc = must(t, redis.NewService(ctx, fake.options()...))

	spannerInstances := must(t, a.ListSpannerInstances(ctx, models.ListSpannerInstancesRequest{ProjectID: "p"}))
	if len(spannerInstances.Instances) != 1 || spannerInstances.Instances[0].NodeCount != 3 || spannerInstances.Instances[0].DisplayName != "Main" {
		t.Errorf("spanner = %+v", spannerInstances)
	}
	clusters := must(t, a.ListAlloyDBClusters(ctx, models.ListAlloyDBClustersRequest{ProjectID: "p"}))
	if len(clusters.Clusters) != 1 || clusters.Clusters[0].Location != "us-east1" {
		t.Errorf("alloydb = %+v (an omitted location must list all with \"-\")", clusters)
	}
	databases := must(t, a.ListFirestoreDatabases(ctx, models.ListFirestoreDatabasesRequest{ProjectID: "p"}))
	if len(databases.Databases) != 1 || databases.Databases[0].LocationID != "nam5" {
		t.Errorf("firestore = %+v", databases)
	}
	caches := must(t, a.ListMemorystoreInstances(ctx, models.ListMemorystoreInstancesRequest{ProjectID: "p", Location: "europe-west1"}))
	if len(caches.Instances) != 1 || caches.Instances[0].MemorySizeGb != 5 || caches.Instances[0].Tier != "STANDARD_HA" {
		t.Errorf("redis = %+v", caches)
	}
}

func TestListVPCConnectorsDiscoversRegionsAndKeepsPartialResults(t *testing.T) {
	fake := newRESTFake(t,
		restRoute{method: "GET", path: "/projects/p/locations", body: map[string]any{
			"locations": []any{map[string]any{"locationId": "us-central1"}, map[string]any{"locationId": "europe-west1"}},
		}},
		restRoute{method: "GET", path: "/projects/p/locations/us-central1/connectors", body: map[string]any{
			"connectors": []any{map[string]any{"name": "projects/p/locations/us-central1/connectors/serverless", "network": "default", "state": "READY"}},
		}},
		restRoute{method: "GET", path: "/projects/p/locations/europe-west1/connectors", status: http.StatusForbidden, body: forbidden},
	)
	a := testAdapter()
	a.vpcAccess = must(t, vpcaccess.NewService(context.Background(), fake.options()...))
	response, err := a.ListVPCConnectors(context.Background(), models.ListVPCConnectorsRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Connectors) != 1 || response.Connectors[0].Name != "serverless" || response.Connectors[0].Region != "us-central1" {
		t.Fatalf("connectors = %+v", response.Connectors)
	}
	if len(response.Errors) != 1 || !strings.Contains(response.Errors[0].Message, "europe-west1") {
		t.Fatalf("errors = %+v, want the denied region reported", response.Errors)
	}
}

func TestListVPCConnectorsHonoursExplicitRegion(t *testing.T) {
	fake := newRESTFake(t,
		restRoute{method: "GET", path: "/projects/p/locations", body: map[string]any{"locations": []any{map[string]any{"locationId": "us-central1"}}}},
		restRoute{method: "GET", path: "/projects/p/locations/asia-east1/connectors", body: map[string]any{"connectors": []any{}}},
	)
	a := testAdapter()
	a.vpcAccess = must(t, vpcaccess.NewService(context.Background(), fake.options()...))
	response := must(t, a.ListVPCConnectors(context.Background(), models.ListVPCConnectorsRequest{ProjectID: "p", Region: "asia-east1"}))
	if response.Connectors == nil || len(response.Connectors) != 0 || len(response.Errors) != 0 {
		t.Fatalf("response = %+v", response)
	}
	for _, request := range fake.requests() {
		if strings.Contains(request, "/us-central1/connectors") {
			t.Fatalf("a discovered region was scanned despite the explicit region: %v", fake.requests())
		}
	}
}

func TestListTaggedResourcesFiltersAndPages(t *testing.T) {
	fake := newRESTFake(t,
		restRoute{method: "GET", path: "/tagBindings", query: map[string]string{"pageToken": "next"}, body: map[string]any{
			"tagBindings": []any{map[string]any{"parent": "//run.googleapis.com/projects/p/locations/us-central1/services/api", "tagValue": "tagValues/2", "tagValueNamespacedName": "123/env/prod"}},
		}},
		restRoute{method: "GET", path: "/tagBindings", body: map[string]any{
			"tagBindings": []any{
				map[string]any{"parent": "//storage.googleapis.com/projects/_/buckets/logs", "tagValue": "tagValues/1", "tagValueNamespacedName": "123/env/dev"},
				map[string]any{"parent": "//storage.googleapis.com/projects/_/buckets/media", "tagValue": "tagValues/2", "tagValueNamespacedName": "123/env/prod"},
			},
			"nextPageToken": "next",
		}},
	)
	a := testAdapter()
	a.crmV3Svc = must(t, crmv3.NewService(context.Background(), fake.options()...))
	response := must(t, a.ListTaggedResources(context.Background(), models.ListTaggedResourcesRequest{ProjectID: "p", TagKey: "env", TagValue: "prod"}))
	if len(response.Resources) != 2 || response.Truncated {
		t.Fatalf("resources = %+v", response)
	}
	if !strings.Contains(fake.requests()[0], "parent=%2F%2Fcloudresourcemanager.googleapis.com%2Fprojects%2Fp") {
		t.Fatalf("tag bindings were not listed for the project: %v", fake.requests())
	}
	if _, err := testAdapter().ListTaggedResources(context.Background(), models.ListTaggedResourcesRequest{ProjectID: "p", TagKey: "env"}); err == nil {
		t.Fatal("a missing CRM v3 client did not return an error")
	}
}

func TestListServiceAccountsAndProjectBindings(t *testing.T) {
	fake := newRESTFake(t,
		restRoute{method: "GET", path: "/projects/p/serviceAccounts", body: map[string]any{
			"accounts":      []any{map[string]any{"name": "projects/p/serviceAccounts/api@p.iam.gserviceaccount.com", "email": "api@p.iam.gserviceaccount.com", "disabled": true, "uniqueId": "42"}},
			"nextPageToken": "more",
		}},
		restRoute{method: "POST", path: "/projects/p:getIamPolicy", body: map[string]any{
			"bindings": []any{map[string]any{"role": "roles/run.invoker", "members": []string{"serviceAccount:api@p.iam.gserviceaccount.com"}}},
		}},
	)
	ctx := context.Background()
	a := testAdapter()
	a.iamAdminSvc = must(t, iam.NewService(ctx, fake.options()...))
	a.crm = must(t, cloudresourcemanager.NewService(ctx, fake.options()...))

	accounts := must(t, a.ListServiceAccounts(ctx, models.ListServiceAccountsRequest{ProjectID: "p"}))
	if len(accounts.ServiceAccounts) != 1 || !accounts.ServiceAccounts[0].Disabled || accounts.ServiceAccounts[0].UniqueID != "42" || !accounts.Truncated || accounts.NextPageToken != "more" {
		t.Fatalf("accounts = %+v", accounts)
	}
	bindings := must(t, a.GetResourceIAMBindings(ctx, models.GetResourceIAMBindingsRequest{ProjectID: "p", URN: "urn:gcp:project:global:p:p"}))
	if len(bindings.Bindings) != 1 || bindings.Bindings[0].Role != "roles/run.invoker" {
		t.Fatalf("bindings = %+v", bindings)
	}
	for _, urn := range []string{"urn:gcp:project:global:other:other", "urn:gcp:project:global:p:other", "urn:gcp:pubsub_topic:global:p:t", "not-a-urn"} {
		if _, err := a.GetResourceIAMBindings(ctx, models.GetResourceIAMBindingsRequest{ProjectID: "p", URN: urn}); err == nil {
			t.Errorf("URN %q was accepted", urn)
		}
	}
}

func TestListVPCNetworksAndSubnets(t *testing.T) {
	fake := newRESTFake(t,
		restRoute{method: "GET", path: "/projects/p/global/networks", body: map[string]any{
			"items": []any{map[string]any{"name": "default", "autoCreateSubnetworks": true, "mtu": 1460,
				"subnetworks": []string{"a", "b"}, "peerings": []any{map[string]any{"name": "peer"}}}},
		}},
		restRoute{method: "GET", path: "/projects/p/aggregated/subnetworks", body: map[string]any{
			"items": map[string]any{
				"regions/us-central1": map[string]any{"subnetworks": []any{
					map[string]any{"name": "app", "network": "https://www.googleapis.com/compute/v1/projects/p/global/networks/default", "ipCidrRange": "10.0.0.0/20", "privateIpGoogleAccess": true},
					map[string]any{"name": "other", "network": "https://www.googleapis.com/compute/v1/projects/p/global/networks/shared", "ipCidrRange": "10.1.0.0/20"},
				}},
				"regions/europe-west1": map[string]any{"subnetworks": []any{
					map[string]any{"name": "eu", "network": "https://www.googleapis.com/compute/v1/projects/p/global/networks/default", "ipCidrRange": "10.2.0.0/20"},
				}},
			},
		}},
	)
	ctx := context.Background()
	a := testAdapter()
	a.computeSvc = must(t, compute.NewService(ctx, fake.options()...))
	networks := must(t, a.ListVPCNetworks(ctx, models.ListVPCNetworksRequest{ProjectID: "p"}))
	if len(networks.Networks) != 1 || networks.Networks[0].SubnetCount != 2 || networks.Networks[0].PeeringCount != 1 || !networks.Networks[0].AutoCreateSubnets {
		t.Fatalf("networks = %+v", networks)
	}
	subnets := must(t, a.ListVPCSubnets(ctx, models.ListVPCSubnetsRequest{ProjectID: "p", Region: "us-central1", Network: "default"}))
	if len(subnets.Subnets) != 1 || subnets.Subnets[0].Name != "app" || subnets.Subnets[0].Network != "default" || !subnets.Subnets[0].PrivateIPGoogleAccess {
		t.Fatalf("subnets = %+v, want only the default network in us-central1", subnets)
	}
	if _, err := testAdapter().ListVPCNetworks(ctx, models.ListVPCNetworksRequest{ProjectID: "p"}); err == nil {
		t.Fatal("a missing compute client did not return an error")
	}
}

func TestSupplyChainListsMapResources(t *testing.T) {
	fake := newRESTFake(t,
		restRoute{method: "GET", path: "/projects/p/locations/-/repositories", body: map[string]any{
			"repositories": []any{map[string]any{"name": "projects/p/locations/us/repositories/images", "format": "DOCKER", "sizeBytes": "2048"}},
		}},
		restRoute{method: "GET", path: "/projects/p/locations/us/repositories/images/dockerImages", body: map[string]any{
			"dockerImages": []any{map[string]any{"name": "img@sha256:abc", "uri": "us-docker.pkg.dev/p/images/api@sha256:abc", "tags": []string{"v1"}, "imageSizeBytes": "1024"}},
		}},
		restRoute{method: "GET", path: "/projects/p/locations/-/triggers", body: map[string]any{
			"triggers": []any{
				map[string]any{"id": "1", "name": "deploy", "github": map[string]any{"owner": "o"}, "filename": "cloudbuild.yaml"},
				map[string]any{"id": "2", "name": "nightly", "pubsubConfig": map[string]any{"topic": "t"}, "disabled": true},
				map[string]any{"id": "3", "name": "hook", "webhookConfig": map[string]any{}},
			},
		}},
	)
	ctx := context.Background()
	a := testAdapter()
	a.artifactRegistrySvc = must(t, artifactregistry.NewService(ctx, fake.options()...))
	a.cloudBuildSvc = must(t, cloudbuild.NewService(ctx, fake.options()...))

	repos := must(t, a.ListArtifactRegistryRepos(ctx, models.ListArtifactRegistryReposRequest{ProjectID: "p"}))
	if len(repos.Repositories) != 1 || repos.Repositories[0].Location != "us" || repos.Repositories[0].SizeBytes != 2048 {
		t.Fatalf("repos = %+v", repos)
	}
	images := must(t, a.ListArtifactRegistryImages(ctx, models.ListArtifactRegistryImagesRequest{ProjectID: "p", Location: "us", Repository: "images"}))
	if len(images.Images) != 1 || images.Images[0].SizeBytes != 1024 || images.Images[0].Tags[0] != "v1" {
		t.Fatalf("images = %+v", images)
	}
	triggers := must(t, a.ListCloudBuildTriggers(ctx, models.ListCloudBuildTriggersRequest{ProjectID: "p"}))
	if len(triggers.Triggers) != 3 {
		t.Fatalf("triggers = %+v", triggers)
	}
	events := map[string]string{}
	for _, trigger := range triggers.Triggers {
		events[trigger.Name] = trigger.EventType
	}
	if events["deploy"] != "github" || events["nightly"] != "pubsub" || events["hook"] != "webhook" {
		t.Fatalf("event types = %v", events)
	}
}
