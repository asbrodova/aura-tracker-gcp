package tools

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

// NetworkingTools provides MCP tool definitions for GCP networking resources:
// Compute Load Balancers, URL Maps, NEGs, VPC Networks, VPC Subnets,
// PSC Endpoints, and API Gateway.
type NetworkingTools struct {
	svc ports.GCPService
	log *slog.Logger
}

func NewNetworkingTools(svc ports.GCPService, log *slog.Logger) *NetworkingTools {
	return &NetworkingTools{svc: svc, log: log}
}

func (t *NetworkingTools) Name() string { return "networking" }

func (t *NetworkingTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.ListLoadBalancers(),
		t.ListURLMaps(),
		t.ListNEGs(),
		t.ListAPIGateways(),
		t.ListVPCNetworks(),
		t.ListVPCSubnets(),
		t.ListPSCEndpoints(),
	}
}

func (t *NetworkingTools) ListLoadBalancers() server.ServerTool {
	tool := mcp.NewTool("gcp_compute_list_loadbalancers",
		mcp.WithDescription(
			"List Compute Engine forwarding rules (Load Balancers) in a project. "+
				"Returns both global (external HTTPS/HTTP) and regional (internal/external TCP/UDP) "+
				"load balancers with their IP address, protocol, ports, load-balancing scheme, "+
				"and target proxy name. Omit region to return all global and regional LBs; "+
				"set region='global' for global-only.",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("region", mcp.Description("GCP region, 'global', or omit for all")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Compute Load Balancers",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listLoadBalancersHandler),
	}
}

func (t *NetworkingTools) listLoadBalancersHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListLoadBalancersRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_compute_list_loadbalancers", "project", args.ProjectID, "region", args.Region)
	resp, err := t.svc.ListLoadBalancers(ctx, args)
	if err != nil {
		return handleServiceError("gcp_compute_list_loadbalancers", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_compute_list_loadbalancers: marshal: %w", err)
	}
	return result, nil
}

func (t *NetworkingTools) ListURLMaps() server.ServerTool {
	tool := mcp.NewTool("gcp_compute_list_url_maps",
		mcp.WithDescription(
			"List Compute Engine URL maps in a project. URL maps define host-based and "+
				"path-based routing rules for HTTP(S) Load Balancers. Returns name, scope "+
				"(global/regional), default backend service, and host rule count. "+
				"Omit region for global URL maps; specify region for regional ones.",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("region", mcp.Description("GCP region for regional URL maps; omit for global")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Compute URL Maps",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listURLMapsHandler),
	}
}

func (t *NetworkingTools) listURLMapsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListURLMapsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_compute_list_url_maps", "project", args.ProjectID, "region", args.Region)
	resp, err := t.svc.ListURLMaps(ctx, args)
	if err != nil {
		return handleServiceError("gcp_compute_list_url_maps", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_compute_list_url_maps: marshal: %w", err)
	}
	return result, nil
}

func (t *NetworkingTools) ListNEGs() server.ServerTool {
	tool := mcp.NewTool("gcp_compute_list_negs",
		mcp.WithDescription(
			"List Compute Engine Network Endpoint Groups (NEGs) across all zones in a project. "+
				"Returns NEG name, type (GCE_VM_IP_PORT, SERVERLESS, INTERNET_IP_PORT, etc.), "+
				"size, zone, and — for SERVERLESS NEGs — the linked Cloud Run service name. "+
				"SERVERLESS NEGs are the backend attachment point between an HTTP(S) LB and "+
				"a Cloud Run or Cloud Functions service.",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("region", mcp.Description("Filter by GCP region; omit for all regions")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Compute NEGs",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listNEGsHandler),
	}
}

func (t *NetworkingTools) listNEGsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListNEGsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_compute_list_negs", "project", args.ProjectID, "region", args.Region)
	resp, err := t.svc.ListNEGs(ctx, args)
	if err != nil {
		return handleServiceError("gcp_compute_list_negs", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_compute_list_negs: marshal: %w", err)
	}
	return result, nil
}

func (t *NetworkingTools) ListAPIGateways() server.ServerTool {
	tool := mcp.NewTool("gcp_apigateway_list",
		mcp.WithDescription(
			"List API Gateway instances in a project. Returns gateway name, location, "+
				"state (ACTIVE, CREATING, FAILED), the API config it serves, and the default "+
				"hostname (e.g. <id>-<hash>-uc.a.run.app). Omit location to list all regions. "+
				"Requires the API Gateway API to be enabled and roles/apigateway.viewer.",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("location", mcp.Description("GCP region; omit for all locations")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List API Gateways",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listAPIGatewaysHandler),
	}
}

func (t *NetworkingTools) listAPIGatewaysHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListAPIGatewaysRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_apigateway_list", "project", args.ProjectID, "location", args.Location)
	resp, err := t.svc.ListAPIGateways(ctx, args)
	if err != nil {
		return handleServiceError("gcp_apigateway_list", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_apigateway_list: marshal: %w", err)
	}
	return result, nil
}

func (t *NetworkingTools) ListVPCNetworks() server.ServerTool {
	tool := mcp.NewTool("gcp_vpc_list_networks",
		mcp.WithDescription(
			"List VPC networks in a project. Returns network name, auto-create-subnets mode, "+
				"number of subnets and VPC peerings, and MTU. Use gcp_vpc_list_subnets to see "+
				"the subnets within each network.",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List VPC Networks",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listVPCNetworksHandler),
	}
}

func (t *NetworkingTools) listVPCNetworksHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListVPCNetworksRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_vpc_list_networks", "project", args.ProjectID)
	resp, err := t.svc.ListVPCNetworks(ctx, args)
	if err != nil {
		return handleServiceError("gcp_vpc_list_networks", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_vpc_list_networks: marshal: %w", err)
	}
	return result, nil
}

func (t *NetworkingTools) ListVPCSubnets() server.ServerTool {
	tool := mcp.NewTool("gcp_vpc_list_subnets",
		mcp.WithDescription(
			"List VPC subnets across all regions in a project. Returns subnet name, region, "+
				"parent network, CIDR range, Private Google Access status, purpose "+
				"(PRIVATE, PRIVATE_SERVICE_CONNECT, etc.), and secondary range count. "+
				"Filter by network name or region to narrow results.",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("network", mcp.Description("Filter by VPC network name or self-link")),
		mcp.WithString("region", mcp.Description("Filter by GCP region")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List VPC Subnets",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listVPCSubnetsHandler),
	}
}

func (t *NetworkingTools) listVPCSubnetsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListVPCSubnetsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_vpc_list_subnets", "project", args.ProjectID, "network", args.Network, "region", args.Region)
	resp, err := t.svc.ListVPCSubnets(ctx, args)
	if err != nil {
		return handleServiceError("gcp_vpc_list_subnets", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_vpc_list_subnets: marshal: %w", err)
	}
	return result, nil
}

func (t *NetworkingTools) ListPSCEndpoints() server.ServerTool {
	tool := mcp.NewTool("gcp_psc_list_endpoints",
		mcp.WithDescription(
			"List Private Service Connect (PSC) consumer endpoints (forwarding rules with "+
				"purpose=PRIVATE_SERVICE_CONNECT) across all regions in a project. Returns "+
				"endpoint name, region, IP address, target service attachment, network, and "+
				"PSC connection ID. PSC endpoints allow VMs and serverless workloads to reach "+
				"Google APIs or producer services over a private IP address.",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("region", mcp.Description("Filter by GCP region; omit for all regions")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List PSC Endpoints",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listPSCEndpointsHandler),
	}
}

func (t *NetworkingTools) listPSCEndpointsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListPSCEndpointsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_psc_list_endpoints", "project", args.ProjectID, "region", args.Region)
	resp, err := t.svc.ListPSCEndpoints(ctx, args)
	if err != nil {
		return handleServiceError("gcp_psc_list_endpoints", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_psc_list_endpoints: marshal: %w", err)
	}
	return result, nil
}
