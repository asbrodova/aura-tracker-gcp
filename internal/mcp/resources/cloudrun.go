package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// ServiceList returns a static resource listing all Cloud Run services in the project.
// URI: gcp://{project}/cloudrun/services
func (r *CloudRunResources) ServiceList(project string) server.ServerResource {
	uri := fmt.Sprintf("gcp://%s/cloudrun/services", project)
	return server.ServerResource{
		Resource: mcp.NewResource(uri, "Cloud Run Services",
			mcp.WithResourceDescription("All Cloud Run services across all regions — discover service names before reading details or revisions"),
			mcp.WithMIMEType("application/json"),
		),
		Handler: func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			r.log.InfoContext(ctx, "resource read", "uri", uri)
			resp, err := r.svc.ListServices(ctx, models.ListServicesRequest{
				ProjectID: project,
				Region:    "-",
			})
			if err != nil {
				return nil, fmt.Errorf("cloudrun services: %w", err)
			}
			data, _ := json.Marshal(resp)
			return []mcp.ResourceContents{mcp.TextResourceContents{
				URI: uri, MIMEType: "application/json", Text: string(data),
			}}, nil
		},
	}
}

// ServiceSnapshotTemplate returns a resource template for a service config snapshot.
// URI template: gcp://{project}/cloudrun/{region}/{service}
func (r *CloudRunResources) ServiceSnapshotTemplate() server.ServerResourceTemplate {
	return server.ServerResourceTemplate{
		Template: mcp.NewResourceTemplate(
			"gcp://{project}/cloudrun/{region}/{service}",
			"Cloud Run Service Snapshot",
			mcp.WithTemplateDescription("URL, traffic splits, latest revision, and labels for a Cloud Run service"),
			mcp.WithTemplateMIMEType("application/json"),
		),
		Handler: func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			project, region, service, err := parseCRServiceURI(req.Params.URI)
			if err != nil {
				return nil, err
			}
			r.log.InfoContext(ctx, "resource read", "uri", req.Params.URI)
			resp, err := r.svc.GetServiceDetails(ctx, models.GetServiceDetailsRequest{
				ProjectID:   project,
				Region:      region,
				ServiceName: service,
			})
			if err != nil {
				return nil, fmt.Errorf("cloudrun service: %w", err)
			}
			data, _ := json.Marshal(resp)
			return []mcp.ResourceContents{mcp.TextResourceContents{
				URI: req.Params.URI, MIMEType: "application/json", Text: string(data),
			}}, nil
		},
	}
}

// RevisionsTemplate returns a resource template scoped to revision and traffic info.
// URI template: gcp://{project}/cloudrun/{region}/{service}/revisions
func (r *CloudRunResources) RevisionsTemplate() server.ServerResourceTemplate {
	return server.ServerResourceTemplate{
		Template: mcp.NewResourceTemplate(
			"gcp://{project}/cloudrun/{region}/{service}/revisions",
			"Cloud Run Service Revisions",
			mcp.WithTemplateDescription("Latest revision name and traffic allocation for a Cloud Run service"),
			mcp.WithTemplateMIMEType("application/json"),
		),
		Handler: func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			rawURI := strings.TrimSuffix(req.Params.URI, "/revisions")
			project, region, service, err := parseCRServiceURI(rawURI)
			if err != nil {
				return nil, err
			}
			r.log.InfoContext(ctx, "resource read", "uri", req.Params.URI)
			resp, err := r.svc.GetServiceDetails(ctx, models.GetServiceDetailsRequest{
				ProjectID:   project,
				Region:      region,
				ServiceName: service,
			})
			if err != nil {
				return nil, fmt.Errorf("cloudrun revisions: %w", err)
			}
			result := map[string]any{
				"service":         resp.Name,
				"latest_revision": resp.LatestRevision,
				"traffic":         resp.Traffic,
			}
			data, _ := json.Marshal(result)
			return []mcp.ResourceContents{mcp.TextResourceContents{
				URI: req.Params.URI, MIMEType: "application/json", Text: string(data),
			}}, nil
		},
	}
}

// parseCRServiceURI parses "gcp://project/cloudrun/region/service" → project, region, service.
func parseCRServiceURI(uri string) (project, region, service string, err error) {
	path := strings.TrimPrefix(uri, "gcp://")
	parts := strings.SplitN(path, "/", 5)
	if len(parts) < 4 || parts[1] != "cloudrun" {
		return "", "", "", fmt.Errorf("invalid Cloud Run service URI: %q", uri)
	}
	return parts[0], parts[2], parts[3], nil
}
