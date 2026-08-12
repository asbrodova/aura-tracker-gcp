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

	discovery := a.discoverRegions(ctx, req.ProjectID, regionServiceScheduler)
	regions := discovery.Regions
	if req.Region != "" && req.Region != "-" {
		regions = []string{req.Region}
		discovery = regionDiscovery{Regions: regions, Source: "request", Complete: true}
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(regionalFanoutConcurrency)
	var mu sync.Mutex
	var jobs []models.SchedulerJobSummary
	truncated := false
	errs := discoveryToolError(discovery, "cloudscheduler.projects.locations.list")
	perRegionLimit := regionalInventoryLimit(len(regions))

	for _, r := range regions {
		r := r
		g.Go(func() error {
			items, regionTruncated, rerr := a.listSchedulerJobsForRegion(gctx, req.ProjectID, r, perRegionLimit)
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
			if regionTruncated {
				truncated = true
				errs = append(errs, models.ToolError{FailingAPI: "cloudscheduler.projects.locations.jobs.list", Message: fmt.Sprintf("region %s truncated at %d items", r, perRegionLimit)})
			}
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
	return models.ListSchedulerJobsResponse{Jobs: jobs, Errors: errs, Truncated: truncated}, nil
}

func (a *gcpAdapter) listSchedulerJobsForRegion(ctx context.Context, projectID, region string, limit int) ([]models.SchedulerJobSummary, bool, error) {
	if a.schedulerClient == nil {
		return nil, false, fmt.Errorf("Cloud Scheduler client is not initialized")
	}
	if err := a.rateWait(ctx, "scheduler.listJobsForRegion"); err != nil {
		return nil, false, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	parent := fmt.Sprintf("projects/%s/locations/%s", projectID, region)
	it := a.schedulerClient.ListJobs(ctx, &schedulerpb.ListJobsRequest{Parent: parent, PageSize: int32(limit)})

	var jobs []models.SchedulerJobSummary
	for {
		job, err := it.Next()
		if err != nil {
			if isIteratorDone(err) {
				return jobs, false, nil
			}
			return nil, false, wrapGCPError("scheduler.listJobsForRegion", err)
		}
		if len(jobs) >= limit {
			return jobs, true, nil
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
