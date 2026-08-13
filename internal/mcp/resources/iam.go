package resources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/internal/environments"
	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// MyPermissions returns a static resource exposing the server identity's effective permissions.
// URI: gcp://{project}/iam/my-permissions
//
// The AI reads this resource to understand which tools will succeed before attempting
// tool calls, and to explain precisely which IAM role is missing when access is denied.
func (r *IAMResources) MyPermissions(environment environments.Environment) server.ServerResource {
	uri := fmt.Sprintf("gcp://%s/iam/my-permissions", environment.DisplayName())
	return server.ServerResource{
		Resource: mcp.NewResource(uri, "My IAM Permissions",
			mcp.WithResourceDescription(
				"Effective permissions of this server's identity, split into granted and denied. "+
					"Read this before calling tools to understand capability gaps and explain missing roles.",
			),
			mcp.WithMIMEType("application/json"),
		),
		Handler: func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			r.log.InfoContext(ctx, "resource read", "uri", uri)
			resp, err := r.svc.TestPermissions(ctx, models.TestPermissionsRequest{
				ProjectID:   environment.ProjectID,
				Permissions: r.permissions,
			})
			if err != nil {
				return nil, fmt.Errorf("iam my-permissions: %w", err)
			}
			report := buildPermissionsReport(environment.ProjectID, resp.Results)
			data, _ := json.Marshal(report)
			return []mcp.ResourceContents{mcp.TextResourceContents{
				URI: uri, MIMEType: "application/json", Text: string(data),
			}}, nil
		},
	}
}

func buildPermissionsReport(project string, results []models.PermissionResult) models.MyPermissionsReport {
	report := models.MyPermissionsReport{
		ProjectID: project,
		Granted:   make([]models.PermissionResult, 0),
		Denied:    make([]models.PermissionResult, 0),
	}
	for _, r := range results {
		if r.Allowed {
			report.Granted = append(report.Granted, r)
		} else {
			report.Denied = append(report.Denied, r)
		}
	}
	return report
}
