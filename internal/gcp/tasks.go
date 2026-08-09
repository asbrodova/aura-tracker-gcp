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

	regions, err := a.discoverRegions(ctx, req.ProjectID)
	if err != nil {
		return models.ListTaskQueuesResponse{}, err
	}
	if req.Region != "" && req.Region != "-" {
		regions = []string{req.Region}
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(regionalFanoutConcurrency)
	var mu sync.Mutex
	var queues []models.TaskQueueSummary
	var errs []models.ToolError

	for _, r := range regions {
		r := r
		g.Go(func() error {
			items, rerr := a.listTaskQueuesForRegion(gctx, req.ProjectID, r)
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
			return nil
		})
	}
	_ = g.Wait()

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
	return models.ListTaskQueuesResponse{Queues: queues, Errors: errs}, nil
}

func (a *gcpAdapter) listTaskQueuesForRegion(ctx context.Context, projectID, region string) ([]models.TaskQueueSummary, error) {
	if a.tasksClient == nil {
		return nil, nil
	}
	if err := a.rateWait(ctx, "tasks.listQueuesForRegion"); err != nil {
		return nil, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	parent := fmt.Sprintf("projects/%s/locations/%s", projectID, region)
	it := a.tasksClient.ListQueues(ctx, &cloudtaskspb.ListQueuesRequest{Parent: parent})

	var queues []models.TaskQueueSummary
	for {
		q, err := it.Next()
		if err != nil {
			if isIteratorDone(err) {
				break
			}
			return nil, wrapGCPError("tasks.listQueuesForRegion", err)
		}
		_, name := parseTaskQueueResourceName(q.Name)
		queues = append(queues, models.TaskQueueSummary{
			Name:   name,
			Region: region,
			State:  q.State.String(),
		})
	}
	return queues, nil
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
