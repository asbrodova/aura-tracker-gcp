package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

// CloudRunTools provides MCP tool definitions and handlers for Cloud Run operations.
type CloudRunTools struct {
	svc          ports.CloudRunService
	log          *slog.Logger
	skipApproval bool
}

// DisableMutationApproval makes the mutation tool execute without asking the
// user. Only for SAFETY_ENABLED=false (local stdio), where svc is the raw
// adapter without the safety decorator.
func (t *CloudRunTools) DisableMutationApproval() { t.skipApproval = true }

func NewCloudRunTools(svc ports.CloudRunService, log *slog.Logger) *CloudRunTools {
	return &CloudRunTools{svc: svc, log: log}
}

func (t *CloudRunTools) Name() string { return "cloudrun" }

func (t *CloudRunTools) GetTools() []server.ServerTool {
	return []server.ServerTool{
		t.ListServices(),
		t.GetServiceDetails(),
		t.UpdateTraffic(),
		t.ListJobs(),
		t.GetJobDetails(),
		t.ListJobExecutions(),
	}
}

func (t *CloudRunTools) ListServices() server.ServerTool {
	tool := mcp.NewTool("gcp_cloudrun_list_services",
		mcp.WithDescription("List all Cloud Run services in a GCP project and region"),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("region", mcp.Required(), mcp.Description("GCP region, e.g. us-central1")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Cloud Run Services",
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

func (t *CloudRunTools) listServicesHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListServicesRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_cloudrun_list_services", "project", args.ProjectID, "region", args.Region)
	resp, err := t.svc.ListServices(ctx, args)
	if err != nil {
		return handleServiceError("gcp_cloudrun_list_services", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_cloudrun_list_services: marshal: %w", err)
	}
	return result, nil
}

func (t *CloudRunTools) GetServiceDetails() server.ServerTool {
	tool := mcp.NewTool("gcp_cloudrun_get_service_details",
		mcp.WithDescription("Get detailed information about a specific Cloud Run service including traffic splits and latest revision"),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("region", mcp.Required(), mcp.Description("GCP region")),
		mcp.WithString("service_name", mcp.Required(), mcp.Description("Cloud Run service name")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Get Cloud Run Service Details",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.getServiceDetailsHandler),
	}
}

func (t *CloudRunTools) getServiceDetailsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.GetServiceDetailsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_cloudrun_get_service_details", "project", args.ProjectID, "service", args.ServiceName)
	resp, err := t.svc.GetServiceDetails(ctx, args)
	if err != nil {
		return handleServiceError("gcp_cloudrun_get_service_details", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_cloudrun_get_service_details: marshal: %w", err)
	}
	return result, nil
}

func (t *CloudRunTools) UpdateTraffic() server.ServerTool {
	tool := mcp.NewTool("gcp_cloudrun_update_traffic",
		mcp.WithDescription(
			"Update the traffic split for a Cloud Run service. "+
				"Mutation: the server previews the before/after split and asks the user to approve it in the MCP client; "+
				"nothing changes unless the user accepts. dry_run=true only previews.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("region", mcp.Required(), mcp.Description("GCP region")),
		mcp.WithString("service_name", mcp.Required(), mcp.Description("Cloud Run service name")),
		mcp.WithArray("traffic",
			mcp.MinItems(1),
			mcp.MaxItems(100),
			mcp.Description("Desired traffic targets. Percentages must be positive and sum to 100."),
			mcp.Items(map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"revision", "percent"},
				"properties": map[string]any{
					"revision": map[string]any{"type": "string", "description": "Existing Cloud Run revision name"},
					"percent":  map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
					"tag":      map[string]any{"type": "string", "description": "Optional unique traffic tag"},
				},
			}),
		),
		mcp.WithBoolean("dry_run",
			mcp.Description("Only preview the before/after split; never asks for approval and never executes. Default: false."),
			mcp.DefaultBool(false),
		),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Update Cloud Run Traffic Split",
			ReadOnlyHint:    boolPtr(false),
			DestructiveHint: boolPtr(true),
			IdempotentHint:  boolPtr(false),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.updateTrafficHandler),
	}
}

func (t *CloudRunTools) updateTrafficHandler(ctx context.Context, req mcp.CallToolRequest, args models.UpdateTrafficRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_cloudrun_update_traffic",
		"project", args.ProjectID,
		"service", args.ServiceName,
		"dry_run", args.DryRun,
	)
	return runMutation(ctx, req, mutation[models.UpdateTrafficResponse]{
		tool:          "gcp_cloudrun_update_traffic",
		previewOnly:   args.DryRun,
		confirmPlanID: args.ConfirmPlanID,
		skipApproval:  t.skipApproval,
		preview: func(ctx context.Context) (models.UpdateTrafficResponse, error) {
			r := args
			r.DryRun, r.ConfirmPlanID = true, ""
			return t.svc.UpdateTraffic(ctx, r)
		},
		confirm: func(ctx context.Context, planID string) (models.UpdateTrafficResponse, error) {
			return t.svc.UpdateTraffic(ctx, models.UpdateTrafficRequest{ConfirmPlanID: planID})
		},
		execute: func(ctx context.Context) (models.UpdateTrafficResponse, error) {
			r := args
			r.DryRun, r.ConfirmPlanID = false, ""
			return t.svc.UpdateTraffic(ctx, r)
		},
		plan: func(r models.UpdateTrafficResponse) (string, bool) { return r.PlanID, r.NoChangeNeeded },
		strip: func(r *models.UpdateTrafficResponse) {
			r.PlanID, r.ExpiresIn = "", ""
			r.Description = "PREVIEW ONLY: " + trafficPreviewText(args, *r)
		},
		prompt: func(r models.UpdateTrafficResponse) string {
			return "Approve this Cloud Run change? " + trafficPreviewText(args, r)
		},
	})
}

// trafficPreviewText describes a traffic change for the user approving it.
// Project IDs in it are replaced by environment aliases before it leaves the
// server.
func trafficPreviewText(args models.UpdateTrafficRequest, r models.UpdateTrafficResponse) string {
	return fmt.Sprintf("Change the traffic split of Cloud Run service %q (%s, project %s) from %s to %s.",
		args.ServiceName, args.Region, args.ProjectID, formatTrafficSplit(r.Before), formatTrafficSplit(r.After))
}

func formatTrafficSplit(targets []models.TrafficTarget) string {
	if len(targets) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(targets))
	for _, target := range targets {
		part := fmt.Sprintf("%d%% %s", target.Percent, target.Revision)
		if target.Tag != "" {
			part += " (tag " + target.Tag + ")"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}

func (t *CloudRunTools) ListJobs() server.ServerTool {
	tool := mcp.NewTool("gcp_cloudrun_list_jobs",
		mcp.WithDescription("List all Cloud Run jobs in a GCP project. Use region='-' or omit to list across all regions."),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("region", mcp.Description("GCP region (e.g. us-central1). Omit or use '-' for all regions.")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Cloud Run Jobs",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listJobsHandler),
	}
}

func (t *CloudRunTools) listJobsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListJobsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_cloudrun_list_jobs", "project", args.ProjectID, "region", args.Region)
	resp, err := t.svc.ListJobs(ctx, args)
	if err != nil {
		return handleServiceError("gcp_cloudrun_list_jobs", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_cloudrun_list_jobs: marshal: %w", err)
	}
	return result, nil
}

func (t *CloudRunTools) GetJobDetails() server.ServerTool {
	tool := mcp.NewTool("gcp_cloudrun_get_job_details",
		mcp.WithDescription("Get detailed information about a specific Cloud Run job including container image, parallelism, and latest execution"),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("region", mcp.Required(), mcp.Description("GCP region")),
		mcp.WithString("job_name", mcp.Required(), mcp.Description("Cloud Run job name")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Get Cloud Run Job Details",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.getJobDetailsHandler),
	}
}

func (t *CloudRunTools) getJobDetailsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.GetJobDetailsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_cloudrun_get_job_details", "project", args.ProjectID, "region", args.Region, "job", args.JobName)
	resp, err := t.svc.GetJobDetails(ctx, args)
	if err != nil {
		return handleServiceError("gcp_cloudrun_get_job_details", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_cloudrun_get_job_details: marshal: %w", err)
	}
	return result, nil
}

func (t *CloudRunTools) ListJobExecutions() server.ServerTool {
	tool := mcp.NewTool("gcp_cloudrun_list_job_executions",
		mcp.WithDescription("List recent executions of a Cloud Run job including status, start/completion times, and task counts"),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithString("region", mcp.Required(), mcp.Description("GCP region")),
		mcp.WithString("job_name", mcp.Required(), mcp.Description("Cloud Run job name")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of executions to return (default 20)")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "List Cloud Run Job Executions",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{
		Tool:    tool,
		Handler: mcp.NewTypedToolHandler(t.listJobExecutionsHandler),
	}
}

func (t *CloudRunTools) listJobExecutionsHandler(ctx context.Context, _ mcp.CallToolRequest, args models.ListJobExecutionsRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_cloudrun_list_job_executions", "project", args.ProjectID, "region", args.Region, "job", args.JobName)
	resp, err := t.svc.ListJobExecutions(ctx, args)
	if err != nil {
		return handleServiceError("gcp_cloudrun_list_job_executions", err)
	}
	result, err := mcp.NewToolResultJSON(resp)
	if err != nil {
		return nil, fmt.Errorf("gcp_cloudrun_list_job_executions: marshal: %w", err)
	}
	return result, nil
}
