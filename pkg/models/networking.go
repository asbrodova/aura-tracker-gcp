package models

// --- Load Balancers ---

type LoadBalancerSummary struct {
	Name                string `json:"name"`
	Scope               string `json:"scope"` // "global" or "regional"
	Region              string `json:"region,omitempty"`
	IPAddress           string `json:"ip_address,omitempty"`
	Protocol            string `json:"protocol,omitempty"`
	Ports               string `json:"ports,omitempty"`
	LoadBalancingScheme string `json:"load_balancing_scheme,omitempty"` // EXTERNAL, INTERNAL, etc.
	Target              string `json:"target,omitempty"`                // target proxy self-link (trimmed)
	NetworkTier         string `json:"network_tier,omitempty"`
}

type ListLoadBalancersRequest struct {
	ProjectID string `json:"project_id"`
	Region    string `json:"region,omitempty"` // omit for global + all regions
}

type ListLoadBalancersResponse struct {
	LoadBalancers []LoadBalancerSummary `json:"load_balancers"`
	Truncated     bool                  `json:"truncated,omitempty"`
}

// --- URL Maps ---

type URLMapSummary struct {
	Name           string `json:"name"`
	Scope          string `json:"scope"` // "global" or "regional"
	Region         string `json:"region,omitempty"`
	DefaultBackend string `json:"default_backend,omitempty"`
	HostRuleCount  int    `json:"host_rule_count"`
}

type ListURLMapsRequest struct {
	ProjectID string `json:"project_id"`
	Region    string `json:"region,omitempty"`
}

type ListURLMapsResponse struct {
	URLMaps   []URLMapSummary `json:"url_maps"`
	Truncated bool            `json:"truncated,omitempty"`
}

// --- Network Endpoint Groups ---

type NEGSummary struct {
	Name                string `json:"name"`
	Region              string `json:"region,omitempty"`
	Zone                string `json:"zone,omitempty"`
	NetworkEndpointType string `json:"network_endpoint_type"`
	Size                int64  `json:"size"`
	Network             string `json:"network,omitempty"`
	CloudRunService     string `json:"cloud_run_service,omitempty"` // for SERVERLESS NEGs
	CloudRunURLMask     string `json:"cloud_run_url_mask,omitempty"`
}

type ListNEGsRequest struct {
	ProjectID string `json:"project_id"`
	Region    string `json:"region,omitempty"`
}

type ListNEGsResponse struct {
	NEGs      []NEGSummary `json:"negs"`
	Truncated bool         `json:"truncated,omitempty"`
}

// --- API Gateway ---

type APIGatewaySummary struct {
	Name            string `json:"name"`
	Location        string `json:"location"`
	State           string `json:"state"`
	APIConfigID     string `json:"api_config_id,omitempty"`
	DefaultHostname string `json:"default_hostname,omitempty"`
	DisplayName     string `json:"display_name,omitempty"`
}

type ListAPIGatewaysRequest struct {
	ProjectID string `json:"project_id"`
	Location  string `json:"location,omitempty"` // omit for all locations
}

type ListAPIGatewaysResponse struct {
	Gateways  []APIGatewaySummary `json:"gateways"`
	Truncated bool                `json:"truncated,omitempty"`
}

// --- VPC Networks ---

type VPCNetworkSummary struct {
	Name              string `json:"name"`
	AutoCreateSubnets bool   `json:"auto_create_subnets"`
	SubnetCount       int    `json:"subnet_count"`
	PeeringCount      int    `json:"peering_count"`
	MTU               int64  `json:"mtu,omitempty"`
	SelfLink          string `json:"self_link"`
}

type ListVPCNetworksRequest struct {
	ProjectID string `json:"project_id"`
}

type ListVPCNetworksResponse struct {
	Networks  []VPCNetworkSummary `json:"networks"`
	Truncated bool                `json:"truncated,omitempty"`
}

// --- VPC Subnets ---

type VPCSubnetSummary struct {
	Name                  string `json:"name"`
	Region                string `json:"region"`
	Network               string `json:"network"`
	IPCIDRRange           string `json:"ip_cidr_range"`
	PrivateIPGoogleAccess bool   `json:"private_ip_google_access"`
	Purpose               string `json:"purpose,omitempty"`
	SecondaryRangeCount   int    `json:"secondary_range_count,omitempty"`
}

type ListVPCSubnetsRequest struct {
	ProjectID string `json:"project_id"`
	Network   string `json:"network,omitempty"` // filter by network name or self-link
	Region    string `json:"region,omitempty"`
}

type ListVPCSubnetsResponse struct {
	Subnets   []VPCSubnetSummary `json:"subnets"`
	Truncated bool               `json:"truncated,omitempty"`
}

// --- PSC Endpoints ---

type PSCEndpointSummary struct {
	Name            string `json:"name"`
	Region          string `json:"region"`
	IPAddress       string `json:"ip_address,omitempty"`
	Target          string `json:"target,omitempty"` // service attachment or target
	Network         string `json:"network,omitempty"`
	PscConnectionID string `json:"psc_connection_id,omitempty"`
}

type ListPSCEndpointsRequest struct {
	ProjectID string `json:"project_id"`
	Region    string `json:"region,omitempty"`
}

type ListPSCEndpointsResponse struct {
	Endpoints []PSCEndpointSummary `json:"endpoints"`
	Truncated bool                 `json:"truncated,omitempty"`
}
