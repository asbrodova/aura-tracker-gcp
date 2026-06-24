package gcp

import (
	"context"
	"fmt"
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

	regions, err := a.discoverRegions(ctx, req.ProjectID)
	if err != nil {
		return models.ListVPCConnectorsResponse{}, err
	}
	if req.Region != "" && req.Region != "-" {
		regions = []string{req.Region}
	}

	g, gctx := errgroup.WithContext(ctx)
	var mu sync.Mutex
	var connectors []models.VPCConnectorSummary
	var errs []models.ToolError

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
	_ = g.Wait()

	if connectors == nil {
		connectors = []models.VPCConnectorSummary{}
	}
	return models.ListVPCConnectorsResponse{Connectors: connectors, Errors: errs}, nil
}

func (a *gcpAdapter) listVPCConnectorsForRegion(ctx context.Context, projectID, region string) ([]models.VPCConnectorSummary, error) {
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	parent := fmt.Sprintf("projects/%s/locations/%s", projectID, region)
	resp, err := a.vpcAccess.Projects.Locations.Connectors.List(parent).Context(ctx).Do()
	if err != nil {
		return nil, wrapGCPError("vpcaccess.listConnectorsForRegion", err)
	}

	var connectors []models.VPCConnectorSummary
	for _, c := range resp.Connectors {
		_, name := parseVPCConnectorResourceName(c.Name)
		connectors = append(connectors, models.VPCConnectorSummary{
			Name:    name,
			Region:  region,
			Network: c.Network,
			State:   c.State,
		})
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
