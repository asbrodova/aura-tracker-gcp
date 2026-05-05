package gcp

import (
	"context"
	"fmt"
	"strings"
	"sync"

	executionspb "cloud.google.com/go/workflows/executions/apiv1/executionspb"
	workflowspb "cloud.google.com/go/workflows/apiv1/workflowspb"
	"golang.org/x/sync/errgroup"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (a *gcpAdapter) ListWorkflows(ctx context.Context, req models.ListWorkflowsRequest) (models.ListWorkflowsResponse, error) {
	if err := a.rateWait(ctx, "workflows.ListWorkflows"); err != nil {
		return models.ListWorkflowsResponse{}, err
	}

	regions, err := a.discoverRegions(ctx, req.ProjectID)
	if err != nil {
		return models.ListWorkflowsResponse{}, err
	}
	if req.Region != "" && req.Region != "-" {
		regions = []string{req.Region}
	}

	g, gctx := errgroup.WithContext(ctx)
	var mu sync.Mutex
	var workflows []models.WorkflowSummary
	var errs []models.ToolError

	for _, r := range regions {
		r := r
		g.Go(func() error {
			items, rerr := a.listWorkflowsForRegion(gctx, req.ProjectID, r)
			mu.Lock()
			defer mu.Unlock()
			if rerr != nil {
				errs = append(errs, models.ToolError{
					FailingAPI: "workflows.projects.locations.workflows.list",
					Message:    fmt.Sprintf("region %s: %v", r, rerr),
				})
				return nil
			}
			workflows = append(workflows, items...)
			return nil
		})
	}
	g.Wait()

	if workflows == nil {
		workflows = []models.WorkflowSummary{}
	}
	return models.ListWorkflowsResponse{Workflows: workflows, Errors: errs}, nil
}

func (a *gcpAdapter) listWorkflowsForRegion(ctx context.Context, projectID, region string) ([]models.WorkflowSummary, error) {
	if a.workflowsClient == nil {
		return nil, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	parent := fmt.Sprintf("projects/%s/locations/%s", projectID, region)
	it := a.workflowsClient.ListWorkflows(ctx, &workflowspb.ListWorkflowsRequest{Parent: parent})

	var workflows []models.WorkflowSummary
	for {
		wf, err := it.Next()
		if err != nil {
			if isIteratorDone(err) {
				break
			}
			return nil, wrapGCPError("workflows.listForRegion", err)
		}
		_, name := parseWorkflowResourceName(wf.Name)
		lastMod := ""
		if wf.UpdateTime != nil {
			lastMod = wf.UpdateTime.AsTime().Format(tsFormat)
		}
		workflows = append(workflows, models.WorkflowSummary{
			Name:         name,
			Region:       region,
			State:        wf.State.String(),
			RevisionID:   wf.RevisionId,
			Description:  wf.Description,
			LastModified: lastMod,
			Labels:       wf.Labels,
		})
	}
	return workflows, nil
}

func (a *gcpAdapter) ListWorkflowExecutions(ctx context.Context, req models.ListWorkflowExecutionsRequest) (models.ListWorkflowExecutionsResponse, error) {
	if err := a.rateWait(ctx, "workflows.ListWorkflowExecutions"); err != nil {
		return models.ListWorkflowExecutionsResponse{}, err
	}
	if a.workflowExecClient == nil {
		return models.ListWorkflowExecutionsResponse{Executions: []models.WorkflowExecutionSummary{}}, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}

	parent := fmt.Sprintf("projects/%s/locations/%s/workflows/%s", req.ProjectID, req.Region, req.WorkflowName)
	it := a.workflowExecClient.ListExecutions(ctx, &executionspb.ListExecutionsRequest{
		Parent:   parent,
		PageSize: int32(limit),
	})

	var execs []models.WorkflowExecutionSummary
	for {
		exec, err := it.Next()
		if err != nil {
			if isIteratorDone(err) {
				break
			}
			return models.ListWorkflowExecutionsResponse{}, wrapGCPError("workflows.ListExecutions", err)
		}
		if len(execs) >= limit {
			break
		}
		start, end := "", ""
		if exec.StartTime != nil {
			start = exec.StartTime.AsTime().Format(tsFormat)
		}
		if exec.EndTime != nil {
			end = exec.EndTime.AsTime().Format(tsFormat)
		}
		_, eName := parseWorkflowExecutionResourceName(exec.Name)
		execs = append(execs, models.WorkflowExecutionSummary{
			Name:      eName,
			State:     exec.State.String(),
			StartTime: start,
			EndTime:   end,
		})
	}
	if execs == nil {
		execs = []models.WorkflowExecutionSummary{}
	}
	return models.ListWorkflowExecutionsResponse{Executions: execs}, nil
}

// parseWorkflowResourceName extracts region and name from:
// "projects/P/locations/REGION/workflows/NAME"
func parseWorkflowResourceName(fullName string) (region, name string) {
	parts := strings.Split(fullName, "/")
	if len(parts) == 6 && parts[0] == "projects" && parts[2] == "locations" && parts[4] == "workflows" {
		return parts[3], parts[5]
	}
	return "", fullName
}

// parseWorkflowExecutionResourceName extracts the bare execution name from:
// "projects/P/locations/REGION/workflows/WORKFLOW/executions/NAME"
func parseWorkflowExecutionResourceName(fullName string) (workflow, name string) {
	parts := strings.Split(fullName, "/")
	if len(parts) == 8 && parts[4] == "workflows" && parts[6] == "executions" {
		return parts[5], parts[7]
	}
	return "", fullName
}
