package drift

import (
	"context"
	"fmt"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (c *GCPCollector) collectGKE(ctx context.Context, req CollectionRequest) (CollectionResult, error) {
	response, err := c.source.ListClusters(ctx, models.ListClustersRequest{ProjectID: req.ProjectID, Location: "-"})
	if err != nil {
		return CollectionResult{}, err
	}
	result := CollectionResult{Resources: []Resource{}}
	for _, cluster := range response.Clusters {
		if !includeResource(req, cluster.Name, cluster.Location) {
			continue
		}
		details, detailErr := c.source.GetClusterDetails(ctx, models.GetClusterDetailsRequest{ProjectID: req.ProjectID, Location: cluster.Location, ClusterName: cluster.Name})
		if detailErr != nil {
			result.Partial = true
			result.Warnings = append(result.Warnings, fmt.Sprintf("GKE cluster %s details: %v", cluster.Name, detailErr))
			result.Resources = append(result.Resources, resource("gke", "gke.cluster", cluster.Name, cluster.Location, "", cluster))
			continue
		}
		result.Resources = append(result.Resources, resource("gke", "gke.cluster", cluster.Name, cluster.Location, "", details))
	}
	return result, nil
}

func (c *GCPCollector) collectGKEWorkloads(ctx context.Context, req CollectionRequest) (CollectionResult, error) {
	clusters, err := c.source.ListClusters(ctx, models.ListClustersRequest{ProjectID: req.ProjectID, Location: "-"})
	if err != nil {
		return CollectionResult{}, err
	}
	result := CollectionResult{Resources: []Resource{}}
	for _, cluster := range clusters.Clusters {
		if len(req.Locations) > 0 && !containsFold(req.Locations, cluster.Location) {
			continue
		}
		workloads, listErr := c.source.ListGKEWorkloads(ctx, models.ListGKEWorkloadsRequest{
			ProjectID: req.ProjectID, Location: cluster.Location, ClusterName: cluster.Name, PageSize: maxResourcesPerComponent,
		})
		if listErr != nil {
			result.Partial = true
			result.Warnings = append(result.Warnings, fmt.Sprintf("cluster %s workloads: %v", cluster.Name, listErr))
			continue
		}
		for _, workload := range workloads.Workloads {
			if !includeKubernetesResource(req, workload.Name, workload.Namespace) {
				continue
			}
			details, detailErr := c.source.GetGKEWorkloadDetails(ctx, models.GetGKEWorkloadDetailsRequest{
				ProjectID: req.ProjectID, Location: cluster.Location, ClusterName: cluster.Name,
				Namespace: workload.Namespace, Name: workload.Name, Kind: workload.Kind,
			})
			if detailErr != nil {
				result.Partial = true
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s %s/%s details: %v", workload.Kind, workload.Namespace, workload.Name, detailErr))
				result.Resources = append(result.Resources, kubernetesResource("gke_workloads", "gke.workload."+workload.Kind, cluster, workload.Name, workload.Namespace, workload.Kind, workload))
				continue
			}
			result.Resources = append(result.Resources, kubernetesResource("gke_workloads", "gke.workload."+workload.Kind, cluster, workload.Name, workload.Namespace, workload.Kind, details))
		}
		partial, warnings := toolErrors(workloads.Errors)
		mergePartial(&result, partial, warnings)

		services, serviceErr := c.source.ListGKEServices(ctx, models.ListGKEServicesRequest{ProjectID: req.ProjectID, Location: cluster.Location, ClusterName: cluster.Name})
		if serviceErr != nil {
			result.Partial = true
			result.Warnings = append(result.Warnings, fmt.Sprintf("cluster %s services: %v", cluster.Name, serviceErr))
		} else {
			for _, service := range services.Services {
				if includeKubernetesResource(req, service.Name, service.Namespace) {
					result.Resources = append(result.Resources, kubernetesResource("gke_workloads", "gke.service", cluster, service.Name, service.Namespace, "Service", service))
				}
			}
			partial, warnings := toolErrors(services.Errors)
			mergePartial(&result, partial, warnings)
		}

		ingresses, ingressErr := c.source.ListGKEIngresses(ctx, models.ListGKEIngressesRequest{ProjectID: req.ProjectID, Location: cluster.Location, ClusterName: cluster.Name})
		if ingressErr != nil {
			result.Partial = true
			result.Warnings = append(result.Warnings, fmt.Sprintf("cluster %s ingresses: %v", cluster.Name, ingressErr))
		} else {
			for _, ingress := range ingresses.Ingresses {
				if includeKubernetesResource(req, ingress.Name, ingress.Namespace) {
					result.Resources = append(result.Resources, kubernetesResource("gke_workloads", "gke.ingress."+ingress.Kind, cluster, ingress.Name, ingress.Namespace, ingress.Kind, ingress))
				}
			}
			partial, warnings := toolErrors(ingresses.Errors)
			mergePartial(&result, partial, warnings)
		}

		policies, policyErr := c.source.ListGKENetworkPolicies(ctx, models.ListGKENetworkPoliciesRequest{ProjectID: req.ProjectID, Location: cluster.Location, ClusterName: cluster.Name})
		if policyErr != nil {
			result.Partial = true
			result.Warnings = append(result.Warnings, fmt.Sprintf("cluster %s network policies: %v", cluster.Name, policyErr))
		} else {
			for _, policy := range policies.Policies {
				if includeKubernetesResource(req, policy.Name, policy.Namespace) {
					result.Resources = append(result.Resources, kubernetesResource("gke_workloads", "gke.network_policy", cluster, policy.Name, policy.Namespace, "NetworkPolicy", policy))
				}
			}
			partial, warnings := toolErrors(policies.Errors)
			mergePartial(&result, partial, warnings)
		}
	}
	return result, nil
}

func includeKubernetesResource(req CollectionRequest, name, namespace string) bool {
	if len(req.ResourceNames) > 0 && !containsFold(req.ResourceNames, name) {
		return false
	}
	if len(req.Namespaces) > 0 && !containsFold(req.Namespaces, namespace) {
		return false
	}
	return true
}

func kubernetesResource(component, kind string, cluster models.ClusterSummary, name, namespace, objectKind string, config any) Resource {
	qualifier := cluster.Name + "/" + namespace + "/" + objectKind
	return resource(component, kind, name, cluster.Location, qualifier, config)
}

func (c *GCPCollector) collectNetworking(ctx context.Context, req CollectionRequest) (CollectionResult, error) {
	result := CollectionResult{Resources: []Resource{}}
	loadBalancers, err := c.source.ListLoadBalancers(ctx, models.ListLoadBalancersRequest{ProjectID: req.ProjectID})
	if err != nil {
		result.Partial = true
		result.Warnings = append(result.Warnings, "load balancers: "+err.Error())
	} else {
		for _, value := range loadBalancers.LoadBalancers {
			if includeResource(req, value.Name, value.Region) {
				result.Resources = append(result.Resources, resource("networking", "compute.load_balancer", value.Name, value.Region, value.Scope, value))
			}
		}
	}
	urlMaps, err := c.source.ListURLMaps(ctx, models.ListURLMapsRequest{ProjectID: req.ProjectID})
	if err != nil {
		result.Partial = true
		result.Warnings = append(result.Warnings, "URL maps: "+err.Error())
	} else {
		for _, value := range urlMaps.URLMaps {
			if includeResource(req, value.Name, value.Region) {
				result.Resources = append(result.Resources, resource("networking", "compute.url_map", value.Name, value.Region, value.Scope, value))
			}
		}
	}
	negs, err := c.source.ListNEGs(ctx, models.ListNEGsRequest{ProjectID: req.ProjectID})
	if err != nil {
		result.Partial = true
		result.Warnings = append(result.Warnings, "network endpoint groups: "+err.Error())
	} else {
		for _, value := range negs.NEGs {
			location := value.Region
			if location == "" {
				location = value.Zone
			}
			if includeResource(req, value.Name, location) {
				result.Resources = append(result.Resources, resource("networking", "compute.neg", value.Name, location, value.NetworkEndpointType, value))
			}
		}
	}
	gateways, err := c.source.ListAPIGateways(ctx, models.ListAPIGatewaysRequest{ProjectID: req.ProjectID})
	if err != nil {
		result.Partial = true
		result.Warnings = append(result.Warnings, "API gateways: "+err.Error())
	} else {
		for _, value := range gateways.Gateways {
			if includeResource(req, value.Name, value.Location) {
				result.Resources = append(result.Resources, resource("networking", "apigateway.gateway", value.Name, value.Location, "", value))
			}
		}
	}
	networks, err := c.source.ListVPCNetworks(ctx, models.ListVPCNetworksRequest{ProjectID: req.ProjectID})
	if err != nil {
		result.Partial = true
		result.Warnings = append(result.Warnings, "VPC networks: "+err.Error())
	} else {
		for _, value := range networks.Networks {
			if includeResource(req, value.Name, "") {
				result.Resources = append(result.Resources, resource("networking", "compute.network", value.Name, "", "", value))
			}
		}
	}
	subnets, err := c.source.ListVPCSubnets(ctx, models.ListVPCSubnetsRequest{ProjectID: req.ProjectID})
	if err != nil {
		result.Partial = true
		result.Warnings = append(result.Warnings, "VPC subnets: "+err.Error())
	} else {
		for _, value := range subnets.Subnets {
			if includeResource(req, value.Name, value.Region) {
				result.Resources = append(result.Resources, resource("networking", "compute.subnet", value.Name, value.Region, "", value))
			}
		}
	}
	psc, err := c.source.ListPSCEndpoints(ctx, models.ListPSCEndpointsRequest{ProjectID: req.ProjectID})
	if err != nil {
		result.Partial = true
		result.Warnings = append(result.Warnings, "Private Service Connect endpoints: "+err.Error())
	} else {
		for _, value := range psc.Endpoints {
			if includeResource(req, value.Name, value.Region) {
				result.Resources = append(result.Resources, resource("networking", "compute.psc_endpoint", value.Name, value.Region, "", value))
			}
		}
	}
	return result, nil
}
