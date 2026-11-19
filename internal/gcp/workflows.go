package gcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"

	workflowspb "cloud.google.com/go/workflows/apiv1/workflowspb"
	executionspb "cloud.google.com/go/workflows/executions/apiv1/executionspb"

	"github.com/asbrodova/aura-tracker-gcp/internal/safego"
	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (a *gcpAdapter) ListWorkflows(ctx context.Context, req models.ListWorkflowsRequest) (models.ListWorkflowsResponse, error) {
	if err := a.rateWait(ctx, "workflows.ListWorkflows"); err != nil {
		return models.ListWorkflowsResponse{}, err
	}

	discovery := a.discoverRegions(ctx, req.ProjectID, regionServiceWorkflows)
	if err := ctx.Err(); err != nil {
		return models.ListWorkflowsResponse{}, fmt.Errorf("workflows.ListWorkflows: %w", err)
	}
	regions := discovery.Regions
	if req.Region != "" && req.Region != "-" {
		regions = []string{req.Region}
		discovery = regionDiscovery{Regions: regions, Source: "request", Complete: true}
	}

	g, gctx := safego.WithContext(ctx)
	g.SetLimit(regionalFanoutConcurrency)
	var mu sync.Mutex
	var workflows []models.WorkflowSummary
	truncated := false
	errs := discoveryToolError(discovery, "workflows.projects.locations.list")
	perRegionLimit := regionalInventoryLimit(len(regions))

	for _, r := range regions {
		r := r
		g.Go(func() error {
			items, regionTruncated, rerr := a.listWorkflowsForRegion(gctx, req.ProjectID, r, perRegionLimit, req.RequireSourceHash)
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
			if regionTruncated {
				truncated = true
				errs = append(errs, models.ToolError{FailingAPI: "workflows.projects.locations.workflows.list", Message: fmt.Sprintf("region %s truncated at %d items", r, perRegionLimit)})
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return models.ListWorkflowsResponse{}, fmt.Errorf("workflows.ListWorkflows: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return models.ListWorkflowsResponse{}, fmt.Errorf("workflows.ListWorkflows: %w", err)
	}

	if workflows == nil {
		workflows = []models.WorkflowSummary{}
	}
	sort.Slice(workflows, func(i, j int) bool {
		if workflows[i].Region != workflows[j].Region {
			return workflows[i].Region < workflows[j].Region
		}
		return workflows[i].Name < workflows[j].Name
	})
	sortToolErrors(errs)
	return models.ListWorkflowsResponse{Workflows: workflows, Errors: errs, Truncated: truncated}, nil
}

func (a *gcpAdapter) listWorkflowsForRegion(ctx context.Context, projectID, region string, limit int, requireSource bool) ([]models.WorkflowSummary, bool, error) {
	if a.workflowsClient == nil {
		return nil, false, fmt.Errorf("workflows client is not initialized")
	}
	if err := a.rateWait(ctx, "workflows.listForRegion"); err != nil {
		return nil, false, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	parent := fmt.Sprintf("projects/%s/locations/%s", projectID, region)
	it := a.workflowsClient.ListWorkflows(ctx, &workflowspb.ListWorkflowsRequest{Parent: parent, PageSize: int32(limit)})

	var workflows []models.WorkflowSummary
	for {
		wf, err := it.Next()
		if err != nil {
			if isIteratorDone(err) {
				return workflows, false, nil
			}
			return nil, false, wrapGCPError("workflows.listForRegion", err)
		}
		if len(workflows) >= limit {
			return workflows, true, nil
		}
		_, name := parseWorkflowResourceName(wf.Name)
		lastMod := ""
		if wf.UpdateTime != nil {
			lastMod = wf.UpdateTime.AsTime().Format(tsFormat)
		}
		if requireSource && wf.GetSourceContents() == "" {
			full, getErr := a.workflowsClient.GetWorkflow(ctx, &workflowspb.GetWorkflowRequest{Name: wf.GetName()})
			if getErr != nil {
				return nil, false, wrapGCPError("workflows.get", getErr)
			}
			wf = full
		}
		workflows = append(workflows, workflowSummary(wf, name, region, lastMod))
	}
}

// workflowSummary maps a workflow, including the settings environment drift
// compares. The definition is reduced to a SHA-256 and environment
// variables to their names, so neither the source nor values are returned.
func workflowSummary(wf *workflowspb.Workflow, name, region, lastModified string) models.WorkflowSummary {
	summary := models.WorkflowSummary{
		Name: name, Region: region, State: wf.GetState().String(), RevisionID: wf.GetRevisionId(),
		Description: wf.GetDescription(), LastModified: lastModified, Labels: wf.GetLabels(),
		ServiceAccount: wf.GetServiceAccount(), CryptoKeyName: wf.GetCryptoKeyName(),
	}
	if level := wf.GetCallLogLevel(); level != workflowspb.Workflow_CALL_LOG_LEVEL_UNSPECIFIED {
		summary.CallLogLevel = level.String()
	}
	if level := wf.GetExecutionHistoryLevel(); level != workflowspb.ExecutionHistoryLevel_EXECUTION_HISTORY_LEVEL_UNSPECIFIED {
		summary.ExecutionHistoryLevel = level.String()
	}
	for key := range wf.GetUserEnvVars() {
		summary.UserEnvVarNames = append(summary.UserEnvVarNames, key)
	}
	sort.Strings(summary.UserEnvVarNames)
	if source := wf.GetSourceContents(); source != "" {
		sum := sha256.Sum256([]byte(source))
		summary.SourceSHA256 = hex.EncodeToString(sum[:])
	}
	return summary
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
