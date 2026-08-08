package resources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// allKnownPermissions is the union of every GCP permission any tool in this server
// might ever require. Add a permission here whenever a new tool is added.
var allKnownPermissions = []string{
	// GKE
	"container.clusters.list",
	"container.clusters.get",
	"container.clusters.update",
	// Cloud Run
	"run.services.list",
	"run.services.get",
	"run.services.update",
	// Pub/Sub
	"pubsub.topics.list",
	"pubsub.topics.get",
	"pubsub.subscriptions.list",
	"pubsub.subscriptions.get",
	// Cloud Logging
	"logging.logEntries.list",
	// Cloud Monitoring
	"monitoring.timeSeries.list",
	// IAM / Resource Manager
	"resourcemanager.projects.get",
	"resourcemanager.projects.getIamPolicy",
	// Recommender
	"recommender.runServiceCostRecommendations.list",
	"recommender.cloudsqlIdleInstanceRecommendations.list",
	"recommender.cloudsqlOverprovisionedInstanceRecommendations.list",
	"recommender.computeInstanceIdleResourceRecommendations.list",
	"recommender.computeDiskIdleResourceRecommendations.list",
	"recommender.computeAddressIdleResourceRecommendations.list",
	"recommender.computeImageIdleResourceRecommendations.list",
	"recommender.computeIdleResourceRecommendations.list",
	"recommender.containerDiagnosisRecommendations.list",
	// BigQuery
	"bigquery.jobs.create",
	"bigquery.datasets.get",
	"bigquery.datasets.getIamPolicy",
	"bigquery.tables.list",
	"bigquery.tables.get",
	"bigquery.tables.getData",
	// Cost reasoning enrichment
	"cloudasset.assets.searchAllResources",
	"serviceusage.services.use",
	// Cloud Storage
	"storage.buckets.list",
	"storage.buckets.get",
}

// MyPermissions returns a static resource exposing the server identity's effective permissions.
// URI: gcp://{project}/iam/my-permissions
//
// The AI reads this resource to understand which tools will succeed before attempting
// tool calls, and to explain precisely which IAM role is missing when access is denied.
func (r *IAMResources) MyPermissions(project string) server.ServerResource {
	uri := fmt.Sprintf("gcp://%s/iam/my-permissions", project)
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
				ProjectID:   project,
				Permissions: allKnownPermissions,
			})
			if err != nil {
				return nil, fmt.Errorf("iam my-permissions: %w", err)
			}
			report := buildPermissionsReport(project, resp.Results)
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
