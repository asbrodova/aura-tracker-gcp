package gcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (a *gcpAdapter) ListVPCConnectors(ctx context.Context, req models.ListVPCConnectorsRequest) (models.ListVPCConnectorsResponse, error) {
	if err := a.rateWait(ctx, "vpcaccess.ListVPCConnectors"); err != nil {
		return models.ListVPCConnectorsResponse{}, err
	}
	if a.vpcAccess == nil {
		return models.ListVPCConnectorsResponse{Connectors: []models.VPCConnectorSummary{}}, nil
	}

	discovery := a.discoverRegions(ctx, req.ProjectID, regionServiceVPCAccess)
	if err := ctx.Err(); err != nil {
		return models.ListVPCConnectorsResponse{}, fmt.Errorf("vpcaccess.ListVPCConnectors: %w", err)
	}
	regions := discovery.Regions
	if req.Region != "" && req.Region != "-" {
		regions = []string{req.Region}
		discovery = regionDiscovery{Regions: regions, Source: "request", Complete: true}
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(regionalFanoutConcurrency)
	var mu sync.Mutex
	var connectors []models.VPCConnectorSummary
	errs := discoveryToolError(discovery, "vpcaccess.projects.locations.list")

	for _, r := range regions {
		r := r
		g.Go(func() error {
			items, rerr := a.listVPCConnectorsForRegion(gctx, req.ProjectID, r)
			mu.Lock()
			defer mu.Unlock()
			if rerr != nil {
				errs = append(errs, models.ToolError{
					FailingAPI: "vpcaccess.projects.locations.connectors.list",
					Message:    fmt.Sprintf("region %s: %v", r, rerr),
				})
				return nil
			}
			connectors = append(connectors, items...)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return models.ListVPCConnectorsResponse{}, fmt.Errorf("vpcaccess.ListVPCConnectors: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return models.ListVPCConnectorsResponse{}, fmt.Errorf("vpcaccess.ListVPCConnectors: %w", err)
	}

	if connectors == nil {
		connectors = []models.VPCConnectorSummary{}
	}
	sort.Slice(connectors, func(i, j int) bool {
		if connectors[i].Region != connectors[j].Region {
			return connectors[i].Region < connectors[j].Region
		}
		return connectors[i].Name < connectors[j].Name
	})
	sortToolErrors(errs)
	return models.ListVPCConnectorsResponse{Connectors: connectors, Errors: errs}, nil
}

func (a *gcpAdapter) listVPCConnectorsForRegion(ctx context.Context, projectID, region string) ([]models.VPCConnectorSummary, error) {
	if err := a.rateWait(ctx, "vpcaccess.listConnectorsForRegion"); err != nil {
		return nil, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	parent := fmt.Sprintf("projects/%s/locations/%s", projectID, region)
	connectors := []models.VPCConnectorSummary{}
	for pageToken := ""; ; {
		call := a.vpcAccess.Projects.Locations.Connectors.List(parent).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, wrapGCPError("vpcaccess.listConnectorsForRegion", err)
		}
		for _, c := range resp.Connectors {
			_, name := parseVPCConnectorResourceName(c.Name)
			connectors = append(connectors, models.VPCConnectorSummary{
				Name: name, Region: region, Network: c.Network, State: c.State,
			})
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return connectors, nil
}

// parseVPCConnectorResourceName extracts region and name from:
// "projects/P/locations/REGION/connectors/NAME"
func parseVPCConnectorResourceName(fullName string) (region, name string) {
	parts := strings.Split(fullName, "/")
	if len(parts) == 6 && parts[0] == "projects" && parts[2] == "locations" && parts[4] == "connectors" {
		return parts[3], parts[5]
	}
	return "", fullName
}
