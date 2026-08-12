package gcp

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	runpb "cloud.google.com/go/run/apiv2/runpb"
	locationpb "google.golang.org/genproto/googleapis/cloud/location"
	containerpb "google.golang.org/genproto/googleapis/container/v1"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

const regionalFanoutConcurrency = 8

const (
	regionServiceScheduler = "scheduler"
	regionServiceEventarc  = "eventarc"
	regionServiceWorkflows = "workflows"
	regionServiceTasks     = "tasks"
	regionServiceVPCAccess = "vpcaccess"
)

// defaultGCPRegions is used only as a partial-coverage fallback when the
// service's authoritative ListLocations call is unavailable. It must never be
// presented as complete discovery.
var defaultGCPRegions = []string{
	"africa-south1",
	"asia-east1", "asia-east2", "asia-northeast1", "asia-northeast2", "asia-northeast3",
	"asia-south1", "asia-south2", "asia-southeast1", "asia-southeast2",
	"australia-southeast1", "australia-southeast2",
	"europe-central2", "europe-north1", "europe-southwest1", "europe-west1", "europe-west2",
	"europe-west3", "europe-west4", "europe-west6", "europe-west8", "europe-west9", "europe-west10", "europe-west12",
	"me-central1", "me-central2", "me-west1",
	"northamerica-northeast1", "northamerica-northeast2", "northamerica-south1",
	"southamerica-east1", "southamerica-west1",
	"us-central1", "us-east1", "us-east4", "us-east5", "us-south1", "us-west1", "us-west2", "us-west3", "us-west4",
}

var gcpRegionRE = regexp.MustCompile(`^[a-z]+(?:-[a-z0-9]+)+[0-9]$`)

type regionDiscovery struct {
	Regions  []string
	Source   string
	Complete bool
	Warning  string
}

// discoverRegions enumerates supported locations from the service being
// queried. Cross-service resource presence is deliberately not used as the
// authoritative region universe.
func (a *gcpAdapter) discoverRegions(ctx context.Context, projectID, service string) regionDiscovery {
	cacheKey := projectID + "|" + service
	if cached, ok := a.regionsCache.get(cacheKey); ok {
		return cloneRegionDiscovery(cached)
	}

	regions, err := a.serviceRegions(ctx, projectID, service)
	if err == nil && len(regions) > 0 {
		discovery := regionDiscovery{Regions: regions, Source: service + ".locations.list", Complete: true}
		a.regionsCache.set(cacheKey, discovery)
		return cloneRegionDiscovery(discovery)
	}

	fallback := append([]string(nil), defaultGCPRegions...)
	fallback = append(fallback, a.regionsFromCloudRun(ctx, projectID)...)
	fallback = append(fallback, a.regionsFromGKE(ctx, projectID)...)
	fallback = uniqueRegions(fallback)
	warning := fmt.Sprintf("authoritative %s location discovery returned no regions; scanned a fallback region set and coverage is partial", service)
	if err != nil {
		warning = fmt.Sprintf("authoritative %s location discovery failed: %v; scanned a fallback region set and coverage is partial", service, err)
	}
	discovery := regionDiscovery{Regions: fallback, Source: "fallback", Complete: false, Warning: warning}
	// Cache degraded discovery briefly through the normal cache. The response
	// remains explicitly partial for every consumer.
	a.regionsCache.set(cacheKey, discovery)
	return cloneRegionDiscovery(discovery)
}

func (a *gcpAdapter) serviceRegions(ctx context.Context, projectID, service string) ([]string, error) {
	if err := a.rateWait(ctx, "regions."+service); err != nil {
		return nil, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()
	request := &locationpb.ListLocationsRequest{Name: "projects/" + projectID, PageSize: 1000}

	switch service {
	case regionServiceScheduler:
		if a.schedulerClient == nil {
			return nil, fmt.Errorf("scheduler client is not initialized")
		}
		return collectServiceLocations(a.schedulerClient.ListLocations(ctx, request).Next)
	case regionServiceEventarc:
		if a.eventarcClient == nil {
			return nil, fmt.Errorf("eventarc client is not initialized")
		}
		return collectServiceLocations(a.eventarcClient.ListLocations(ctx, request).Next)
	case regionServiceWorkflows:
		if a.workflowsClient == nil {
			return nil, fmt.Errorf("workflows client is not initialized")
		}
		return collectServiceLocations(a.workflowsClient.ListLocations(ctx, request).Next)
	case regionServiceTasks:
		if a.tasksClient == nil {
			return nil, fmt.Errorf("cloud tasks client is not initialized")
		}
		return collectServiceLocations(a.tasksClient.ListLocations(ctx, request).Next)
	case regionServiceVPCAccess:
		if a.vpcAccess == nil {
			return nil, fmt.Errorf("VPC Access client is not initialized")
		}
		var regions []string
		for pageToken := ""; ; {
			call := a.vpcAccess.Projects.Locations.List("projects/" + projectID).Context(ctx).PageSize(1000)
			if pageToken != "" {
				call = call.PageToken(pageToken)
			}
			response, err := call.Do()
			if err != nil {
				return nil, wrapGCPError("regions.vpcaccess", err)
			}
			for _, location := range response.Locations {
				if !appendInventoryBounded(&regions, location.LocationId) {
					return uniqueRegions(regions), errInventoryLimitReached
				}
			}
			if response.NextPageToken == "" {
				return uniqueRegions(regions), nil
			}
			pageToken = response.NextPageToken
		}
	default:
		return nil, fmt.Errorf("unsupported location service %q", service)
	}
}

func collectServiceLocations(next func() (*locationpb.Location, error)) ([]string, error) {
	var regions []string
	for scanned := 0; ; scanned++ {
		location, err := next()
		if err != nil {
			if isIteratorDone(err) {
				return uniqueRegions(regions), nil
			}
			return nil, err
		}
		if scanned >= maxUnpagedInventoryItems {
			return uniqueRegions(regions), errInventoryLimitReached
		}
		regions = append(regions, location.LocationId)
	}
}

func uniqueRegions(values []string) []string {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if gcpRegionRE.MatchString(value) {
			seen[value] = true
		}
	}
	regions := make([]string, 0, len(seen))
	for region := range seen {
		regions = append(regions, region)
	}
	sort.Strings(regions)
	return regions
}

func cloneRegionDiscovery(value regionDiscovery) regionDiscovery {
	value.Regions = append([]string(nil), value.Regions...)
	return value
}

func discoveryToolError(discovery regionDiscovery, api string) []models.ToolError {
	if discovery.Complete || discovery.Warning == "" {
		return nil
	}
	return []models.ToolError{{FailingAPI: api, Message: discovery.Warning, Retriable: true}}
}

func (a *gcpAdapter) regionsFromCloudRun(ctx context.Context, projectID string) []string {
	if a.runSvc == nil || a.rateWait(ctx, "regions.cloudrun") != nil {
		return nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	var regions []string
	it := a.runSvc.ListServices(ctx, &runpb.ListServicesRequest{Parent: "projects/" + projectID + "/locations/-"})
	for len(regions) < maxUnpagedInventoryItems {
		svc, err := it.Next()
		if err != nil {
			break
		}
		if region, _ := parseSvcResourceName(svc.Name); region != "" {
			regions = append(regions, region)
		}
	}
	return uniqueRegions(regions)
}

func (a *gcpAdapter) regionsFromGKE(ctx context.Context, projectID string) []string {
	if a.clusterMgr == nil || a.rateWait(ctx, "regions.gke") != nil {
		return nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	resp, err := a.clusterMgr.ListClusters(ctx, &containerpb.ListClustersRequest{Parent: "projects/" + projectID + "/locations/-"})
	if err != nil {
		return nil
	}
	regions := make([]string, 0, len(resp.Clusters))
	for _, cluster := range resp.Clusters {
		regions = append(regions, zoneToRegion(cluster.Location))
	}
	return uniqueRegions(regions)
}

// zoneToRegion converts a GCP zone (us-central1-a) to its parent region.
func zoneToRegion(location string) string {
	parts := strings.Split(location, "-")
	if len(parts) >= 3 {
		last := parts[len(parts)-1]
		if len(last) == 1 && last[0] >= 'a' && last[0] <= 'z' {
			return strings.Join(parts[:len(parts)-1], "-")
		}
	}
	return location
}
