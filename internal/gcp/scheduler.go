package gcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	schedulerpb "cloud.google.com/go/scheduler/apiv1/schedulerpb"
	"golang.org/x/sync/errgroup"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (a *gcpAdapter) ListSchedulerJobs(ctx context.Context, req models.ListSchedulerJobsRequest) (models.ListSchedulerJobsResponse, error) {
	if err := a.rateWait(ctx, "scheduler.ListSchedulerJobs"); err != nil {
		return models.ListSchedulerJobsResponse{}, err
	}

	regions, err := a.discoverRegions(ctx, req.ProjectID)
	if err != nil {
		return models.ListSchedulerJobsResponse{}, err
	}

	if req.Region != "" && req.Region != "-" {
		regions = []string{req.Region}
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(regionalFanoutConcurrency)
	var mu sync.Mutex
	var jobs []models.SchedulerJobSummary
	var errs []models.ToolError

	for _, r := range regions {
		r := r
		g.Go(func() error {
			items, rerr := a.listSchedulerJobsForRegion(gctx, req.ProjectID, r)
			mu.Lock()
			defer mu.Unlock()
			if rerr != nil {
				errs = append(errs, models.ToolError{
					FailingAPI: "cloudscheduler.projects.locations.jobs.list",
					Message:    fmt.Sprintf("region %s: %v", r, rerr),
					Retriable:  false,
				})
				return nil
			}
			jobs = append(jobs, items...)
			return nil
		})
	}
	_ = g.Wait()

	if jobs == nil {
		jobs = []models.SchedulerJobSummary{}
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].Region != jobs[j].Region {
			return jobs[i].Region < jobs[j].Region
		}
		return jobs[i].Name < jobs[j].Name
	})
	sortToolErrors(errs)
	return models.ListSchedulerJobsResponse{Jobs: jobs, Errors: errs}, nil
}

func (a *gcpAdapter) listSchedulerJobsForRegion(ctx context.Context, projectID, region string) ([]models.SchedulerJobSummary, error) {
	if a.schedulerClient == nil {
		return nil, nil
	}
	if err := a.rateWait(ctx, "scheduler.listJobsForRegion"); err != nil {
		return nil, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	parent := fmt.Sprintf("projects/%s/locations/%s", projectID, region)
	it := a.schedulerClient.ListJobs(ctx, &schedulerpb.ListJobsRequest{Parent: parent})

	var jobs []models.SchedulerJobSummary
	for {
		job, err := it.Next()
		if err != nil {
			if isIteratorDone(err) {
				break
			}
			return nil, wrapGCPError("scheduler.listJobsForRegion", err)
		}
		_, name := parseSchedulerResourceName(job.Name)
		targetKind, targetRef := resolveSchedulerTarget(job)
		jobs = append(jobs, models.SchedulerJobSummary{
			Name:        name,
			Region:      region,
			Schedule:    job.Schedule,
			TimeZone:    job.TimeZone,
			State:       job.State.String(),
			TargetKind:  targetKind,
			TargetRef:   targetRef,
			Description: job.Description,
		})
	}
	return jobs, nil
}

func resolveSchedulerTarget(job *schedulerpb.Job) (kind, ref string) {
	switch t := job.Target.(type) {
	case *schedulerpb.Job_HttpTarget:
		return "http", t.HttpTarget.GetUri()
	case *schedulerpb.Job_PubsubTarget:
		return "pubsub", t.PubsubTarget.GetTopicName()
	case *schedulerpb.Job_AppEngineHttpTarget:
		return "app_engine", t.AppEngineHttpTarget.GetRelativeUri()
	default:
		return "unknown", ""
	}
}

// parseSchedulerResourceName extracts region and name from:
// "projects/P/locations/REGION/jobs/NAME"
func parseSchedulerResourceName(fullName string) (region, name string) {
	parts := strings.Split(fullName, "/")
	if len(parts) == 6 && parts[0] == "projects" && parts[2] == "locations" && parts[4] == "jobs" {
		return parts[3], parts[5]
	}
	return "", fullName
}
