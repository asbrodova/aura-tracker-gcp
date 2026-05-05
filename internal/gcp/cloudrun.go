package gcp

import (
	"context"
	"fmt"
	"strings"

	runpb "cloud.google.com/go/run/apiv2/runpb"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

const tsFormat = "2006-01-02T15:04:05Z"

func (a *gcpAdapter) ListServices(ctx context.Context, req models.ListServicesRequest) (models.ListServicesResponse, error) {
	if err := a.rateWait(ctx, "cloudrun.ListServices"); err != nil {
		return models.ListServicesResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	loc := req.Region
	if loc == "" {
		loc = "-"
	}
	parent := fmt.Sprintf("projects/%s/locations/%s", req.ProjectID, loc)
	it := a.runSvc.ListServices(ctx, &runpb.ListServicesRequest{Parent: parent})

	var services []models.ServiceSummary
	for {
		svc, err := it.Next()
		if err != nil {
			if isIteratorDone(err) {
				break
			}
			return models.ListServicesResponse{}, wrapGCPError("cloudrun.ListServices", err)
		}
		lastMod := ""
		if svc.UpdateTime != nil {
			lastMod = svc.UpdateTime.AsTime().Format("2006-01-02T15:04:05Z")
		}
		// Cloud Functions Gen 2 deploy on the Cloud Run runtime. Exclude them
		// here; gcp_functions_list is the canonical tool for those resources.
		if svc.Labels["goog-managed-by"] == "cloudfunctions" {
			continue
		}
		region, name := parseSvcResourceName(svc.Name)
		services = append(services, models.ServiceSummary{
			Name:         name,
			Region:       region,
			URL:          svc.Uri,
			LastModified: lastMod,
		})
	}
	if services == nil {
		services = []models.ServiceSummary{}
	}
	return models.ListServicesResponse{Services: services}, nil
}

func (a *gcpAdapter) ListJobs(ctx context.Context, req models.ListJobsRequest) (models.ListJobsResponse, error) {
	if err := a.rateWait(ctx, "cloudrun.ListJobs"); err != nil {
		return models.ListJobsResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	loc := req.Region
	if loc == "" {
		loc = "-"
	}
	parent := fmt.Sprintf("projects/%s/locations/%s", req.ProjectID, loc)
	it := a.runJobs.ListJobs(ctx, &runpb.ListJobsRequest{Parent: parent})

	var jobs []models.JobSummary
	for {
		job, err := it.Next()
		if err != nil {
			if isIteratorDone(err) {
				break
			}
			return models.ListJobsResponse{}, wrapGCPError("cloudrun.ListJobs", err)
		}
		region, name := parseJobResourceName(job.Name)
		lastMod := ""
		if job.UpdateTime != nil {
			lastMod = job.UpdateTime.AsTime().Format(tsFormat)
		}
		latestExec := ""
		if job.LatestCreatedExecution != nil {
			latestExec = job.LatestCreatedExecution.Name
		}
		var taskCount, parallelism int32
		if job.Template != nil {
			taskCount = job.Template.TaskCount
			parallelism = job.Template.Parallelism
		}
		jobs = append(jobs, models.JobSummary{
			Name:            name,
			Region:          region,
			LastModified:    lastMod,
			TaskCount:       taskCount,
			Parallelism:     parallelism,
			Labels:          job.Labels,
			LatestExecution: latestExec,
		})
	}
	if jobs == nil {
		jobs = []models.JobSummary{}
	}
	return models.ListJobsResponse{Jobs: jobs}, nil
}

func (a *gcpAdapter) GetJobDetails(ctx context.Context, req models.GetJobDetailsRequest) (models.JobDetails, error) {
	if err := a.rateWait(ctx, "cloudrun.GetJobDetails"); err != nil {
		return models.JobDetails{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	name := fmt.Sprintf("projects/%s/locations/%s/jobs/%s", req.ProjectID, req.Region, req.JobName)
	job, err := a.runJobs.GetJob(ctx, &runpb.GetJobRequest{Name: name})
	if err != nil {
		return models.JobDetails{}, wrapGCPError("cloudrun.GetJobDetails", err)
	}

	region, bareName := parseJobResourceName(job.Name)
	lastMod := ""
	if job.UpdateTime != nil {
		lastMod = job.UpdateTime.AsTime().Format(tsFormat)
	}
	latestExec := ""
	if job.LatestCreatedExecution != nil {
		latestExec = job.LatestCreatedExecution.Name
	}

	var taskCount, parallelism, maxRetries int32
	var timeoutSec int64
	var image, sa string
	if job.Template != nil {
		taskCount = job.Template.TaskCount
		parallelism = job.Template.Parallelism
		if tt := job.Template.Template; tt != nil {
			maxRetries = tt.GetMaxRetries()
			if tt.Timeout != nil {
				timeoutSec = int64(tt.Timeout.AsDuration().Seconds())
			}
			sa = tt.ServiceAccount
			if len(tt.Containers) > 0 {
				image = tt.Containers[0].Image
			}
		}
	}

	return models.JobDetails{
		JobSummary: models.JobSummary{
			Name:            bareName,
			Region:          region,
			LastModified:    lastMod,
			TaskCount:       taskCount,
			Parallelism:     parallelism,
			Labels:          job.Labels,
			LatestExecution: latestExec,
		},
		Image:          image,
		MaxRetries:     maxRetries,
		TimeoutSeconds: timeoutSec,
		ServiceAccount: sa,
	}, nil
}

func (a *gcpAdapter) ListJobExecutions(ctx context.Context, req models.ListJobExecutionsRequest) (models.ListJobExecutionsResponse, error) {
	if err := a.rateWait(ctx, "cloudrun.ListJobExecutions"); err != nil {
		return models.ListJobExecutionsResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	parent := fmt.Sprintf("projects/%s/locations/%s/jobs/%s", req.ProjectID, req.Region, req.JobName)
	it := a.runExecs.ListExecutions(ctx, &runpb.ListExecutionsRequest{
		Parent:   parent,
		PageSize: int32(limit),
	})

	var execs []models.JobExecutionSummary
	for {
		exec, err := it.Next()
		if err != nil {
			if isIteratorDone(err) {
				break
			}
			return models.ListJobExecutionsResponse{}, wrapGCPError("cloudrun.ListJobExecutions", err)
		}
		if len(execs) >= limit {
			break
		}
		start, completion := "", ""
		if exec.StartTime != nil {
			start = exec.StartTime.AsTime().Format(tsFormat)
		}
		if exec.CompletionTime != nil {
			completion = exec.CompletionTime.AsTime().Format(tsFormat)
		}
		_, eName := parseExecutionResourceName(exec.Name)
		execs = append(execs, models.JobExecutionSummary{
			Name:           eName,
			StartTime:      start,
			CompletionTime: completion,
			RunningCount:   exec.RunningCount,
			SucceededCount: exec.SucceededCount,
			FailedCount:    exec.FailedCount,
		})
	}
	if execs == nil {
		execs = []models.JobExecutionSummary{}
	}
	return models.ListJobExecutionsResponse{Executions: execs}, nil
}

// parseSvcResourceName extracts region and name from:
// "projects/P/locations/REGION/services/NAME"
func parseSvcResourceName(fullName string) (region, name string) {
	parts := strings.Split(fullName, "/")
	if len(parts) == 6 && parts[0] == "projects" && parts[2] == "locations" && parts[4] == "services" {
		return parts[3], parts[5]
	}
	return "", fullName
}

// parseJobResourceName extracts region and name from:
// "projects/P/locations/REGION/jobs/NAME"
func parseJobResourceName(fullName string) (region, name string) {
	parts := strings.Split(fullName, "/")
	if len(parts) == 6 && parts[0] == "projects" && parts[2] == "locations" && parts[4] == "jobs" {
		return parts[3], parts[5]
	}
	return "", fullName
}

// parseExecutionResourceName extracts the bare execution name from:
// "projects/P/locations/REGION/jobs/JOB/executions/NAME"
func parseExecutionResourceName(fullName string) (job, name string) {
	parts := strings.Split(fullName, "/")
	if len(parts) == 8 && parts[0] == "projects" && parts[2] == "locations" && parts[4] == "jobs" && parts[6] == "executions" {
		return parts[5], parts[7]
	}
	return "", fullName
}

func (a *gcpAdapter) GetServiceDetails(ctx context.Context, req models.GetServiceDetailsRequest) (models.ServiceDetails, error) {
	if err := a.rateWait(ctx, "cloudrun.GetServiceDetails"); err != nil {
		return models.ServiceDetails{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	name := fmt.Sprintf("projects/%s/locations/%s/services/%s", req.ProjectID, req.Region, req.ServiceName)
	svc, err := a.runSvc.GetService(ctx, &runpb.GetServiceRequest{Name: name})
	if err != nil {
		return models.ServiceDetails{}, wrapGCPError("cloudrun.GetServiceDetails", err)
	}

	traffic := make([]models.TrafficTarget, 0, len(svc.Traffic))
	for _, t := range svc.Traffic {
		revName := t.Revision
		if revName == "" {
			if svc.LatestReadyRevision != "" {
				revName = svc.LatestReadyRevision
			} else {
				revName = "LATEST"
			}
		}
		traffic = append(traffic, models.TrafficTarget{
			Revision: revName,
			Percent:  int32(t.Percent),
			Tag:      t.Tag,
		})
	}

	lastMod := ""
	if svc.UpdateTime != nil {
		lastMod = svc.UpdateTime.AsTime().Format("2006-01-02T15:04:05Z")
	}

	latestRevision := svc.LatestReadyRevision

	return models.ServiceDetails{
		ServiceSummary: models.ServiceSummary{
			Name:         svc.Name,
			Region:       req.Region,
			URL:          svc.Uri,
			LastModified: lastMod,
		},
		Traffic:        traffic,
		LatestRevision: latestRevision,
		Labels:         svc.Labels,
	}, nil
}

// UpdateTraffic updates the traffic split for a Cloud Run service.
// When DryRun=true it returns a description of the change without executing it.
func (a *gcpAdapter) UpdateTraffic(ctx context.Context, req models.UpdateTrafficRequest) (models.UpdateTrafficResponse, error) {
	if err := a.rateWait(ctx, "cloudrun.UpdateTraffic"); err != nil {
		return models.UpdateTrafficResponse{}, err
	}

	// Always fetch current state for before/after reporting.
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	name := fmt.Sprintf("projects/%s/locations/%s/services/%s", req.ProjectID, req.Region, req.ServiceName)
	current, err := a.runSvc.GetService(ctx, &runpb.GetServiceRequest{Name: name})
	if err != nil {
		return models.UpdateTrafficResponse{}, wrapGCPError("cloudrun.UpdateTraffic.get", err)
	}

	before := make([]models.TrafficTarget, 0, len(current.Traffic))
	for _, t := range current.Traffic {
		before = append(before, models.TrafficTarget{
			Revision: t.Revision,
			Percent:  int32(t.Percent),
			Tag:      t.Tag,
		})
	}

	if req.DryRun {
		return models.UpdateTrafficResponse{
			DryRun:      true,
			ServiceName: req.ServiceName,
			Before:      before,
			After:       req.Traffic,
			Description: fmt.Sprintf("DRY RUN: would update traffic for service %q", req.ServiceName),
		}, nil
	}

	pbTraffic := make([]*runpb.TrafficTarget, 0, len(req.Traffic))
	for _, t := range req.Traffic {
		pbTraffic = append(pbTraffic, &runpb.TrafficTarget{
			Revision: t.Revision,
			Percent:  t.Percent,
			Tag:      t.Tag,
		})
	}

	op, err := a.runSvc.UpdateService(ctx, &runpb.UpdateServiceRequest{
		Service: &runpb.Service{
			Name:    name,
			Traffic: pbTraffic,
		},
	})
	if err != nil {
		return models.UpdateTrafficResponse{}, wrapGCPError("cloudrun.UpdateTraffic.update", err)
	}
	// We don't wait for the LRO to complete — the operation was submitted successfully.
	_ = op

	return models.UpdateTrafficResponse{
		DryRun:      false,
		ServiceName: req.ServiceName,
		Before:      before,
		After:       req.Traffic,
		Description: fmt.Sprintf("traffic update submitted for service %q — operation in progress", req.ServiceName),
	}, nil
}
