package gcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	cloudtaskspb "cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"golang.org/x/sync/errgroup"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (a *gcpAdapter) ListTaskQueues(ctx context.Context, req models.ListTaskQueuesRequest) (models.ListTaskQueuesResponse, error) {
	if err := a.rateWait(ctx, "tasks.ListTaskQueues"); err != nil {
		return models.ListTaskQueuesResponse{}, err
	}

	discovery := a.discoverRegions(ctx, req.ProjectID, regionServiceTasks)
	if err := ctx.Err(); err != nil {
		return models.ListTaskQueuesResponse{}, fmt.Errorf("tasks.ListTaskQueues: %w", err)
	}
	regions := discovery.Regions
	if req.Region != "" && req.Region != "-" {
		regions = []string{req.Region}
		discovery = regionDiscovery{Regions: regions, Source: "request", Complete: true}
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(regionalFanoutConcurrency)
	var mu sync.Mutex
	var queues []models.TaskQueueSummary
	truncated := false
	errs := discoveryToolError(discovery, "cloudtasks.projects.locations.list")
	perRegionLimit := regionalInventoryLimit(len(regions))

	for _, r := range regions {
		r := r
		g.Go(func() error {
			items, regionTruncated, rerr := a.listTaskQueuesForRegion(gctx, req.ProjectID, r, perRegionLimit)
			mu.Lock()
			defer mu.Unlock()
			if rerr != nil {
				errs = append(errs, models.ToolError{
					FailingAPI: "cloudtasks.projects.locations.queues.list",
					Message:    fmt.Sprintf("region %s: %v", r, rerr),
				})
				return nil
			}
			queues = append(queues, items...)
			if regionTruncated {
				truncated = true
				errs = append(errs, models.ToolError{FailingAPI: "cloudtasks.projects.locations.queues.list", Message: fmt.Sprintf("region %s truncated at %d items", r, perRegionLimit)})
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return models.ListTaskQueuesResponse{}, fmt.Errorf("tasks.ListTaskQueues: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return models.ListTaskQueuesResponse{}, fmt.Errorf("tasks.ListTaskQueues: %w", err)
	}

	if queues == nil {
		queues = []models.TaskQueueSummary{}
	}
	sort.Slice(queues, func(i, j int) bool {
		if queues[i].Region != queues[j].Region {
			return queues[i].Region < queues[j].Region
		}
		return queues[i].Name < queues[j].Name
	})
	sortToolErrors(errs)
	return models.ListTaskQueuesResponse{Queues: queues, Errors: errs, Truncated: truncated}, nil
}

func (a *gcpAdapter) listTaskQueuesForRegion(ctx context.Context, projectID, region string, limit int) ([]models.TaskQueueSummary, bool, error) {
	if a.tasksClient == nil {
		return nil, false, fmt.Errorf("cloud tasks client is not initialized")
	}
	if err := a.rateWait(ctx, "tasks.listQueuesForRegion"); err != nil {
		return nil, false, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	parent := fmt.Sprintf("projects/%s/locations/%s", projectID, region)
	it := a.tasksClient.ListQueues(ctx, &cloudtaskspb.ListQueuesRequest{Parent: parent, PageSize: int32(limit)})

	var queues []models.TaskQueueSummary
	for {
		q, err := it.Next()
		if err != nil {
			if isIteratorDone(err) {
				return queues, false, nil
			}
			return nil, false, wrapGCPError("tasks.listQueuesForRegion", err)
		}
		if len(queues) >= limit {
			return queues, true, nil
		}
		_, name := parseTaskQueueResourceName(q.Name)
		queues = append(queues, models.TaskQueueSummary{
			Name:   name,
			Region: region,
			State:  q.State.String(),
		})
	}
}

// parseTaskQueueResourceName extracts region and name from:
// "projects/P/locations/REGION/queues/NAME"
func parseTaskQueueResourceName(fullName string) (region, name string) {
	parts := strings.Split(fullName, "/")
	if len(parts) == 6 && parts[0] == "projects" && parts[2] == "locations" && parts[4] == "queues" {
		return parts[3], parts[5]
	}
	return "", fullName
}
