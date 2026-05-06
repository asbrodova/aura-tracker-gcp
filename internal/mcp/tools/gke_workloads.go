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

// GKEWorkloadTools provides MCP tool definitions for GKE workload introspection.
// These tools connect to the cluster's Kubernetes API server directly (not the
// GKE management API), so they surface Deployments, Services, Ingresses, and
// NetworkPolicies rather than cluster-level metadata.
type GKEWorkloadTools struct {
	svc ports.GCPService
	log *slog.Logger
}

func NewGKEWorkloadTools(svc ports.GCPService, log *slog.Logger) *GKEWorkloadTools {
	return &GKEWorkloadTools{svc: svc, log: log}
}

func (t *GKEWorkloadTools) Name() string { return "gke_workloads" }

func (t *GKEWorkloadTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.ListWorkloads(),
		t.GetWorkloadDetails(),
		t.ListServices(),
		t.ListIngresses(),
		t.ListNetworkPolicies(),
	}
}

func (t *GKEWorkloadTools) ListWorkloads() server.ServerTool {
	tool := mcp.NewTool("gcp_gke_list_workloads",
		mcp.WithDescription(
			"List Kubernetes workloads (Deployments, StatefulSets, DaemonSets, CronJobs, Jobs) "+
				"running in a GKE cluster. Returns image, replica count, service account, "+
				"referenced Secrets, and whether OpenTelemetry instrumentation is detected. "+
				"Requires the cluster's Kubernetes API to be accessible; returns an error for "+
				"clusters with a private endpoint and no external access.",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("cluster_name", mcp.Required(), mcp.Description("GKE cluster name")),
		mcp.WithString("location", mcp.Required(), mcp.Description("GCP region or zone of the cluster")),
		mcp.WithString("namespace", mcp.Description("Kubernetes namespace; omit for all namespaces")),
		mcp.WithString("kind",
			mcp.Description("Filter by workload kind: Deployment, StatefulSet, DaemonSet, CronJob, or Job. Omit for all."),
		),
		mcp.WithNumber("page_size", mcp.Description("Max workloads to return per kind (default 500)")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List GKE Workloads",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listWorkloadsHandler),
	}
}

func (t *GKEWorkloadTools) listWorkloadsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListGKEWorkloadsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_gke_list_workloads",
		"project", args.ProjectID, "cluster", args.ClusterName,
		"location", args.Location, "namespace", args.Namespace, "kind", args.Kind,
	)
	resp, err := t.svc.ListGKEWorkloads(ctx, args)
	if err != nil {
		return handleServiceError("gcp_gke_list_workloads", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_gke_list_workloads: marshal: %w", err)
	}
	return result, nil
}

func (t *GKEWorkloadTools) GetWorkloadDetails() server.ServerTool {
	tool := mcp.NewTool("gcp_gke_get_workload_details",
		mcp.WithDescription(
			"Get full details for a single GKE workload: all container specs, environment variables "+
				"(with secret references masked), resource requests/limits, node selector, tolerations, "+
				"and annotations. Useful for diagnosing misconfigurations or tracing secret dependencies.",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("cluster_name", mcp.Required(), mcp.Description("GKE cluster name")),
		mcp.WithString("location", mcp.Required(), mcp.Description("GCP region or zone of the cluster")),
		mcp.WithString("namespace", mcp.Required(), mcp.Description("Kubernetes namespace")),
		mcp.WithString("name", mcp.Required(), mcp.Description("Workload name")),
		mcp.WithString("kind", mcp.Required(), mcp.Description("Workload kind: Deployment, StatefulSet, DaemonSet, CronJob, or Job")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Get GKE Workload Details",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.getWorkloadDetailsHandler),
	}
}

func (t *GKEWorkloadTools) getWorkloadDetailsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.GetGKEWorkloadDetailsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_gke_get_workload_details",
		"project", args.ProjectID, "cluster", args.ClusterName,
		"namespace", args.Namespace, "name", args.Name, "kind", args.Kind,
	)
	resp, err := t.svc.GetGKEWorkloadDetails(ctx, args)
	if err != nil {
		return handleServiceError("gcp_gke_get_workload_details", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_gke_get_workload_details: marshal: %w", err)
	}
	return result, nil
}

func (t *GKEWorkloadTools) ListServices() server.ServerTool {
	tool := mcp.NewTool("gcp_gke_list_services",
		mcp.WithDescription(
			"List Kubernetes Services in a GKE cluster. Returns service type (ClusterIP, NodePort, "+
				"LoadBalancer), selector labels, ports, and — when present — the cloud.google.com/neg "+
				"annotation that links the service to a GCP Network Endpoint Group.",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("cluster_name", mcp.Required(), mcp.Description("GKE cluster name")),
		mcp.WithString("location", mcp.Required(), mcp.Description("GCP region or zone of the cluster")),
		mcp.WithString("namespace", mcp.Description("Kubernetes namespace; omit for all namespaces")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List GKE Services",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listServicesHandler),
	}
}

func (t *GKEWorkloadTools) listServicesHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListGKEServicesRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_gke_list_services",
		"project", args.ProjectID, "cluster", args.ClusterName, "namespace", args.Namespace,
	)
	resp, err := t.svc.ListGKEServices(ctx, args)
	if err != nil {
		return handleServiceError("gcp_gke_list_services", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_gke_list_services: marshal: %w", err)
	}
	return result, nil
}

func (t *GKEWorkloadTools) ListIngresses() server.ServerTool {
	tool := mcp.NewTool("gcp_gke_list_ingresses",
		mcp.WithDescription(
			"List Kubernetes Ingress resources and Gateway API HTTPRoutes in a GKE cluster. "+
				"Returns hosts, TLS status, routing rules, and — when the GKE Ingress controller "+
				"has provisioned a load balancer — the linked GCP LB name. Gateway API HTTPRoutes "+
				"are included when the Gateway CRD is installed; a 404 from the API server is "+
				"treated as 'not installed' and silently skipped.",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("cluster_name", mcp.Required(), mcp.Description("GKE cluster name")),
		mcp.WithString("location", mcp.Required(), mcp.Description("GCP region or zone of the cluster")),
		mcp.WithString("namespace", mcp.Description("Kubernetes namespace; omit for all namespaces")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List GKE Ingresses and HTTPRoutes",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listIngressesHandler),
	}
}

func (t *GKEWorkloadTools) listIngressesHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListGKEIngressesRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_gke_list_ingresses",
		"project", args.ProjectID, "cluster", args.ClusterName, "namespace", args.Namespace,
	)
	resp, err := t.svc.ListGKEIngresses(ctx, args)
	if err != nil {
		return handleServiceError("gcp_gke_list_ingresses", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_gke_list_ingresses: marshal: %w", err)
	}
	return result, nil
}

func (t *GKEWorkloadTools) ListNetworkPolicies() server.ServerTool {
	tool := mcp.NewTool("gcp_gke_list_network_policies",
		mcp.WithDescription(
			"List Kubernetes NetworkPolicies in a GKE cluster. Returns pod selector, "+
				"ingress/egress rule counts, and policy types. Useful for understanding "+
				"which pods are isolated and whether observed traffic edges in the architecture "+
				"graph are permitted by policy.",
		),
		mcp.WithString("project_id", mcp.Required(), mcp.Description("GCP project ID")),
		mcp.WithString("cluster_name", mcp.Required(), mcp.Description("GKE cluster name")),
		mcp.WithString("location", mcp.Required(), mcp.Description("GCP region or zone of the cluster")),
		mcp.WithString("namespace", mcp.Description("Kubernetes namespace; omit for all namespaces")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List GKE NetworkPolicies",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listNetworkPoliciesHandler),
	}
}

func (t *GKEWorkloadTools) listNetworkPoliciesHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListGKENetworkPoliciesRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_gke_list_network_policies",
		"project", args.ProjectID, "cluster", args.ClusterName, "namespace", args.Namespace,
	)
	resp, err := t.svc.ListGKENetworkPolicies(ctx, args)
	if err != nil {
		return handleServiceError("gcp_gke_list_network_policies", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_gke_list_network_policies: marshal: %w", err)
	}
	return result, nil
}
