package gcp

import (
	"context"
	"fmt"
	"path"
	"strings"

	"google.golang.org/api/apigateway/v1beta"
	"google.golang.org/api/compute/v1"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// --- Load Balancers ---

func (a *gcpAdapter) ListLoadBalancers(ctx context.Context, req models.ListLoadBalancersRequest) (models.ListLoadBalancersResponse, error) {
	if err := a.rateWait(ctx, "networking.ListLoadBalancers"); err != nil {
		return models.ListLoadBalancersResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	var lbs []models.LoadBalancerSummary

	// Global forwarding rules (always included when region is unspecified or "global")
	if req.Region == "" || req.Region == "global" {
		err := a.computeSvc.GlobalForwardingRules.List(req.ProjectID).
			Context(ctx).Pages(ctx, func(page *compute.ForwardingRuleList) error {
			for _, fr := range page.Items {
				lbs = append(lbs, forwardingRuleToLB(fr, "global", ""))
			}
			return nil
		})
		if err != nil {
			return models.ListLoadBalancersResponse{}, wrapGCPError("networking.ListLoadBalancers.global", err)
		}
	}

	// Regional forwarding rules
	if req.Region != "" && req.Region != "global" {
		err := a.computeSvc.ForwardingRules.List(req.ProjectID, req.Region).
			Context(ctx).Pages(ctx, func(page *compute.ForwardingRuleList) error {
			for _, fr := range page.Items {
				lbs = append(lbs, forwardingRuleToLB(fr, "regional", req.Region))
			}
			return nil
		})
		if err != nil {
			return models.ListLoadBalancersResponse{}, wrapGCPError("networking.ListLoadBalancers.regional", err)
		}
	} else if req.Region == "" {
		// All regions via aggregated list
		err := a.computeSvc.ForwardingRules.AggregatedList(req.ProjectID).
			Context(ctx).Pages(ctx, func(page *compute.ForwardingRuleAggregatedList) error {
			for scopeKey, scopedList := range page.Items {
				region := regionFromScopeKey(scopeKey)
				for _, fr := range scopedList.ForwardingRules {
					lbs = append(lbs, forwardingRuleToLB(fr, "regional", region))
				}
			}
			return nil
		})
		if err != nil {
			return models.ListLoadBalancersResponse{}, wrapGCPError("networking.ListLoadBalancers.aggregated", err)
		}
	}

	if lbs == nil {
		lbs = []models.LoadBalancerSummary{}
	}
	return models.ListLoadBalancersResponse{LoadBalancers: lbs}, nil
}

func forwardingRuleToLB(fr *compute.ForwardingRule, scope, region string) models.LoadBalancerSummary {
	r := region
	if r == "" && fr.Region != "" {
		r = path.Base(fr.Region)
	}
	return models.LoadBalancerSummary{
		Name:                fr.Name,
		Scope:               scope,
		Region:              r,
		IPAddress:           fr.IPAddress,
		Protocol:            fr.IPProtocol,
		Ports:               strings.Join(fr.Ports, ","),
		LoadBalancingScheme: fr.LoadBalancingScheme,
		Target:              path.Base(fr.Target),
		NetworkTier:         fr.NetworkTier,
	}
}

// --- URL Maps ---

func (a *gcpAdapter) ListURLMaps(ctx context.Context, req models.ListURLMapsRequest) (models.ListURLMapsResponse, error) {
	if err := a.rateWait(ctx, "networking.ListURLMaps"); err != nil {
		return models.ListURLMapsResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	var maps []models.URLMapSummary

	if req.Region == "" || req.Region == "global" {
		err := a.computeSvc.UrlMaps.List(req.ProjectID).
			Context(ctx).Pages(ctx, func(page *compute.UrlMapList) error {
			for _, um := range page.Items {
				maps = append(maps, urlMapToSummary(um, "global", ""))
			}
			return nil
		})
		if err != nil {
			return models.ListURLMapsResponse{}, wrapGCPError("networking.ListURLMaps.global", err)
		}
	}

	if req.Region != "" && req.Region != "global" {
		err := a.computeSvc.RegionUrlMaps.List(req.ProjectID, req.Region).
			Context(ctx).Pages(ctx, func(page *compute.UrlMapList) error {
			for _, um := range page.Items {
				maps = append(maps, urlMapToSummary(um, "regional", req.Region))
			}
			return nil
		})
		if err != nil {
			return models.ListURLMapsResponse{}, wrapGCPError("networking.ListURLMaps.regional", err)
		}
	}

	if maps == nil {
		maps = []models.URLMapSummary{}
	}
	return models.ListURLMapsResponse{URLMaps: maps}, nil
}

func urlMapToSummary(um *compute.UrlMap, scope, region string) models.URLMapSummary {
	return models.URLMapSummary{
		Name:           um.Name,
		Scope:          scope,
		Region:         region,
		DefaultBackend: path.Base(um.DefaultService),
		HostRuleCount:  len(um.HostRules),
	}
}

// --- NEGs ---

func (a *gcpAdapter) ListNEGs(ctx context.Context, req models.ListNEGsRequest) (models.ListNEGsResponse, error) {
	if err := a.rateWait(ctx, "networking.ListNEGs"); err != nil {
		return models.ListNEGsResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	var negs []models.NEGSummary

	err := a.computeSvc.NetworkEndpointGroups.AggregatedList(req.ProjectID).
		Context(ctx).Pages(ctx, func(page *compute.NetworkEndpointGroupAggregatedList) error {
		for scopeKey, scopedList := range page.Items {
			zone := zoneFromScopeKey(scopeKey)
			region := regionFromZone(zone)
			for _, neg := range scopedList.NetworkEndpointGroups {
				if req.Region != "" && region != req.Region {
					continue
				}
				negs = append(negs, negToSummary(neg, region, zone))
			}
		}
		return nil
	})
	if err != nil {
		return models.ListNEGsResponse{}, wrapGCPError("networking.ListNEGs", err)
	}

	if negs == nil {
		negs = []models.NEGSummary{}
	}
	return models.ListNEGsResponse{NEGs: negs}, nil
}

func negToSummary(neg *compute.NetworkEndpointGroup, region, zone string) models.NEGSummary {
	s := models.NEGSummary{
		Name:                neg.Name,
		Region:              region,
		Zone:                zone,
		NetworkEndpointType: neg.NetworkEndpointType,
		Size:                neg.Size,
		Network:             path.Base(neg.Network),
	}
	if neg.CloudRun != nil {
		s.CloudRunService = neg.CloudRun.Service
		s.CloudRunURLMask = neg.CloudRun.UrlMask
	}
	return s
}

// --- API Gateway ---

func (a *gcpAdapter) ListAPIGateways(ctx context.Context, req models.ListAPIGatewaysRequest) (models.ListAPIGatewaysResponse, error) {
	if err := a.rateWait(ctx, "networking.ListAPIGateways"); err != nil {
		return models.ListAPIGatewaysResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	location := req.Location
	if location == "" {
		location = "-" // all locations
	}
	parent := fmt.Sprintf("projects/%s/locations/%s", req.ProjectID, location)

	var gateways []models.APIGatewaySummary
	err := a.apiGatewaySvc.Projects.Locations.Gateways.List(parent).
		Context(ctx).Pages(ctx, func(page *apigateway.ApigatewayListGatewaysResponse) error {
		for _, gw := range page.Gateways {
			gateways = append(gateways, apiGatewayToSummary(gw))
		}
		return nil
	})
	if err != nil {
		return models.ListAPIGatewaysResponse{}, wrapGCPError("networking.ListAPIGateways", err)
	}

	if gateways == nil {
		gateways = []models.APIGatewaySummary{}
	}
	return models.ListAPIGatewaysResponse{Gateways: gateways}, nil
}

func apiGatewayToSummary(gw *apigateway.ApigatewayGateway) models.APIGatewaySummary {
	// Name format: projects/{p}/locations/{l}/gateways/{id}
	parts := strings.Split(gw.Name, "/")
	shortName := gw.Name
	if len(parts) > 0 {
		shortName = parts[len(parts)-1]
	}
	loc := ""
	if len(parts) >= 4 {
		loc = parts[3]
	}
	configID := ""
	if gw.ApiConfig != "" {
		configID = path.Base(gw.ApiConfig)
	}
	return models.APIGatewaySummary{
		Name:            shortName,
		Location:        loc,
		State:           gw.State,
		APIConfigID:     configID,
		DefaultHostname: gw.DefaultHostname,
		DisplayName:     gw.DisplayName,
	}
}

// --- VPC Networks ---

func (a *gcpAdapter) ListVPCNetworks(ctx context.Context, req models.ListVPCNetworksRequest) (models.ListVPCNetworksResponse, error) {
	if err := a.rateWait(ctx, "networking.ListVPCNetworks"); err != nil {
		return models.ListVPCNetworksResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	var networks []models.VPCNetworkSummary
	err := a.computeSvc.Networks.List(req.ProjectID).
		Context(ctx).Pages(ctx, func(page *compute.NetworkList) error {
		for _, n := range page.Items {
			networks = append(networks, vpcNetworkToSummary(n))
		}
		return nil
	})
	if err != nil {
		return models.ListVPCNetworksResponse{}, wrapGCPError("networking.ListVPCNetworks", err)
	}

	if networks == nil {
		networks = []models.VPCNetworkSummary{}
	}
	return models.ListVPCNetworksResponse{Networks: networks}, nil
}

func vpcNetworkToSummary(n *compute.Network) models.VPCNetworkSummary {
	return models.VPCNetworkSummary{
		Name:              n.Name,
		AutoCreateSubnets: n.AutoCreateSubnetworks,
		SubnetCount:       len(n.Subnetworks),
		PeeringCount:      len(n.Peerings),
		MTU:               n.Mtu,
		SelfLink:          n.SelfLink,
	}
}

// --- VPC Subnets ---

func (a *gcpAdapter) ListVPCSubnets(ctx context.Context, req models.ListVPCSubnetsRequest) (models.ListVPCSubnetsResponse, error) {
	if err := a.rateWait(ctx, "networking.ListVPCSubnets"); err != nil {
		return models.ListVPCSubnetsResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	var subnets []models.VPCSubnetSummary
	err := a.computeSvc.Subnetworks.AggregatedList(req.ProjectID).
		Context(ctx).Pages(ctx, func(page *compute.SubnetworkAggregatedList) error {
		for scopeKey, scopedList := range page.Items {
			region := regionFromScopeKey(scopeKey)
			if req.Region != "" && region != req.Region {
				continue
			}
			for _, sn := range scopedList.Subnetworks {
				networkName := path.Base(sn.Network)
				if req.Network != "" && networkName != req.Network && sn.Network != req.Network {
					continue
				}
				subnets = append(subnets, subnetToSummary(sn, region))
			}
		}
		return nil
	})
	if err != nil {
		return models.ListVPCSubnetsResponse{}, wrapGCPError("networking.ListVPCSubnets", err)
	}

	if subnets == nil {
		subnets = []models.VPCSubnetSummary{}
	}
	return models.ListVPCSubnetsResponse{Subnets: subnets}, nil
}

func subnetToSummary(sn *compute.Subnetwork, region string) models.VPCSubnetSummary {
	return models.VPCSubnetSummary{
		Name:                  sn.Name,
		Region:                region,
		Network:               path.Base(sn.Network),
		IPCIDRRange:           sn.IpCidrRange,
		PrivateIPGoogleAccess: sn.PrivateIpGoogleAccess,
		Purpose:               sn.Purpose,
		SecondaryRangeCount:   len(sn.SecondaryIpRanges),
	}
}

// --- PSC Endpoints ---

func (a *gcpAdapter) ListPSCEndpoints(ctx context.Context, req models.ListPSCEndpointsRequest) (models.ListPSCEndpointsResponse, error) {
	if err := a.rateWait(ctx, "networking.ListPSCEndpoints"); err != nil {
		return models.ListPSCEndpointsResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	var endpoints []models.PSCEndpointSummary
	call := a.computeSvc.ForwardingRules.AggregatedList(req.ProjectID).
		Filter(`purpose="PRIVATE_SERVICE_CONNECT"`)

	err := call.Context(ctx).Pages(ctx, func(page *compute.ForwardingRuleAggregatedList) error {
		for scopeKey, scopedList := range page.Items {
			region := regionFromScopeKey(scopeKey)
			if req.Region != "" && region != req.Region {
				continue
			}
			for _, fr := range scopedList.ForwardingRules {
				endpoints = append(endpoints, pscEndpointToSummary(fr, region))
			}
		}
		return nil
	})
	if err != nil {
		return models.ListPSCEndpointsResponse{}, wrapGCPError("networking.ListPSCEndpoints", err)
	}

	if endpoints == nil {
		endpoints = []models.PSCEndpointSummary{}
	}
	return models.ListPSCEndpointsResponse{Endpoints: endpoints}, nil
}

func pscEndpointToSummary(fr *compute.ForwardingRule, region string) models.PSCEndpointSummary {
	return models.PSCEndpointSummary{
		Name:            fr.Name,
		Region:          region,
		IPAddress:       fr.IPAddress,
		Target:          fr.Target,
		Network:         path.Base(fr.Network),
		PscConnectionID: fmt.Sprintf("%d", fr.PscConnectionId),
	}
}

// --- Scope key helpers ---

// regionFromScopeKey extracts the region name from keys like "regions/us-central1"
// or returns "" for "global".
func regionFromScopeKey(key string) string {
	if key == "global" {
		return ""
	}
	return strings.TrimPrefix(key, "regions/")
}

// zoneFromScopeKey extracts the zone name from keys like "zones/us-central1-a".
func zoneFromScopeKey(key string) string {
	return strings.TrimPrefix(key, "zones/")
}

// regionFromZone returns the region for a zone name (e.g. "us-central1-a" → "us-central1").
func regionFromZone(zone string) string {
	if idx := strings.LastIndex(zone, "-"); idx > 0 {
		return zone[:idx]
	}
	return zone
}
