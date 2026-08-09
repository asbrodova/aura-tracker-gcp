package gcp

import (
	"context"
	"sort"
	"strings"

	runpb "cloud.google.com/go/run/apiv2/runpb"
	containerpb "google.golang.org/genproto/googleapis/container/v1"
)

const regionalFanoutConcurrency = 8

// defaultGCPRegions is the fallback list used when neither Cloud Run nor GKE
// surfaces any active regions for a project.
var defaultGCPRegions = []string{
	"us-central1", "us-east1", "us-east4", "us-west1", "us-west2",
	"europe-west1", "europe-west2", "europe-west3", "europe-west4",
	"asia-east1", "asia-east2", "asia-northeast1", "asia-southeast1",
	"australia-southeast1", "northamerica-northeast1",
}

// discoverRegions returns the set of GCP regions that are active in a project,
// using a three-tier fallback strategy and a 10-minute TTL cache.
//
// Tier 1: list Cloud Run services with location="-" → extract unique locations.
// Tier 2: list GKE clusters with location="-" → extract unique regions (strips zones).
// Tier 3: return defaultGCPRegions.
func (a *gcpAdapter) discoverRegions(ctx context.Context, projectID string) ([]string, error) {
	if cached, ok := a.regionsCache.get(projectID); ok {
		return cached, nil
	}

	regions := a.regionsFromCloudRun(ctx, projectID)

	if len(regions) == 0 {
		regions = a.regionsFromGKE(ctx, projectID)
	}

	if len(regions) == 0 {
		regions = defaultGCPRegions
	}

	a.regionsCache.set(projectID, regions)
	return regions, nil
}

func (a *gcpAdapter) regionsFromCloudRun(ctx context.Context, projectID string) []string {
	if a.runSvc == nil {
		return nil
	}
	if err := a.rateWait(ctx, "regions.cloudrun"); err != nil {
		return nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	seen := make(map[string]bool)
	it := a.runSvc.ListServices(ctx, &runpb.ListServicesRequest{
		Parent: "projects/" + projectID + "/locations/-",
	})
	for {
		svc, err := it.Next()
		if err != nil {
			break
		}
		if region, _ := parseSvcResourceName(svc.Name); region != "" {
			seen[region] = true
		}
	}
	return mapKeys(seen)
}

func (a *gcpAdapter) regionsFromGKE(ctx context.Context, projectID string) []string {
	if a.clusterMgr == nil {
		return nil
	}
	if err := a.rateWait(ctx, "regions.gke"); err != nil {
		return nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	resp, err := a.clusterMgr.ListClusters(ctx, &containerpb.ListClustersRequest{
		Parent: "projects/" + projectID + "/locations/-",
	})
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	for _, c := range resp.Clusters {
		// c.Location can be a region ("us-central1") or a zone ("us-central1-a").
		// Strip the zone suffix if present.
		region := zoneToRegion(c.Location)
		if region != "" {
			seen[region] = true
		}
	}
	return mapKeys(seen)
}

// zoneToRegion converts a GCP zone (us-central1-a) to its parent region
// (us-central1). If the input is already a region it is returned unchanged.
func zoneToRegion(location string) string {
	parts := strings.Split(location, "-")
	if len(parts) > 3 {
		// e.g. "northamerica-northeast1-a" → 4 parts → drop last
		return strings.Join(parts[:len(parts)-1], "-")
	}
	if len(parts) == 3 {
		// Check if last part is a single letter (zone suffix like "a", "b", "c")
		last := parts[len(parts)-1]
		if len(last) == 1 && last[0] >= 'a' && last[0] <= 'z' {
			return strings.Join(parts[:2], "-")
		}
	}
	return location
}

func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
