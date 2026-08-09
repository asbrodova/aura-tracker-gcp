package gcp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	runpb "cloud.google.com/go/run/apiv2/runpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
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
			Labels:       svc.Labels,
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
			Labels:       svc.Labels,
		},
		Traffic:        traffic,
		LatestRevision: latestRevision,
		Labels:         svc.Labels,
	}, nil
}

func (a *gcpAdapter) ListRevisions(ctx context.Context, req models.ListRevisionsRequest) (models.ListRevisionsResponse, error) {
	if err := a.rateWait(ctx, "cloudrun.ListRevisions"); err != nil {
		return models.ListRevisionsResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	if req.Region == "" || req.ServiceName == "" {
		return models.ListRevisionsResponse{}, fmt.Errorf("cloudrun.ListRevisions: region and service_name are required")
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Limit > 100 {
		return models.ListRevisionsResponse{}, fmt.Errorf("cloudrun.ListRevisions: limit must be at most 100")
	}

	parent := fmt.Sprintf("projects/%s/locations/%s/services/%s", req.ProjectID, req.Region, req.ServiceName)
	it := a.runRevisions.ListRevisions(ctx, &runpb.ListRevisionsRequest{
		Parent:      parent,
		PageSize:    int32(req.Limit + 1),
		ShowDeleted: req.ShowDeleted,
	})

	revisions := make([]models.RevisionSummary, 0, req.Limit)
	truncated := false
	for {
		revision, err := it.Next()
		if isIteratorDone(err) {
			break
		}
		if err != nil {
			return models.ListRevisionsResponse{}, wrapGCPError("cloudrun.ListRevisions", err)
		}
		if len(revisions) >= req.Limit {
			truncated = true
			break
		}
		revisions = append(revisions, revisionSummaryFromProto(revision))
	}
	return models.ListRevisionsResponse{Revisions: revisions, Truncated: truncated}, nil
}

func revisionSummaryFromProto(revision *runpb.Revision) models.RevisionSummary {
	if revision == nil {
		return models.RevisionSummary{Containers: []models.RevisionContainer{}}
	}
	region, service, revisionName := parseRevisionResourceName(revision.Name)
	summary := models.RevisionSummary{
		Name:                          revisionName,
		ServiceName:                   service,
		Region:                        region,
		Creator:                       revision.Creator,
		ServiceAccount:                revision.ServiceAccount,
		MaxInstanceRequestConcurrency: revision.MaxInstanceRequestConcurrency,
		ExecutionEnvironment:          revision.ExecutionEnvironment.String(),
		Reconciling:                   revision.Reconciling,
		Labels:                        revision.Labels,
		Containers:                    make([]models.RevisionContainer, 0, len(revision.Containers)),
	}
	if revision.CreateTime != nil {
		summary.CreateTime = revision.CreateTime.AsTime().UTC().Format(timeFormatRFC3339)
	}
	if revision.UpdateTime != nil {
		summary.UpdateTime = revision.UpdateTime.AsTime().UTC().Format(timeFormatRFC3339)
	}
	if revision.DeleteTime != nil {
		summary.DeleteTime = revision.DeleteTime.AsTime().UTC().Format(timeFormatRFC3339)
	}
	if revision.Timeout != nil {
		summary.TimeoutSeconds = int64(revision.Timeout.AsDuration().Seconds())
	}
	if revision.Scaling != nil {
		summary.MinInstances = revision.Scaling.MinInstanceCount
		summary.MaxInstances = revision.Scaling.MaxInstanceCount
	}
	if revision.VpcAccess != nil {
		summary.VPCConnector = revision.VpcAccess.Connector
		summary.VPCEgress = revision.VpcAccess.Egress.String()
	}

	for _, container := range revision.Containers {
		if container == nil {
			continue
		}
		out := models.RevisionContainer{Name: container.Name, Image: container.Image}
		if container.Resources != nil {
			out.ResourceLimits = container.Resources.Limits
			out.CPUIdle = container.Resources.CpuIdle
			out.StartupCPUBoost = container.Resources.StartupCpuBoost
		}
		for _, env := range container.Env {
			if env == nil {
				continue
			}
			out.EnvironmentNames = append(out.EnvironmentNames, env.Name)
			if source := env.GetValueSource(); source != nil && source.SecretKeyRef != nil {
				ref := source.SecretKeyRef.Secret
				if source.SecretKeyRef.Version != "" {
					ref += ":" + source.SecretKeyRef.Version
				}
				out.SecretReferences = append(out.SecretReferences, ref)
			}
		}
		sort.Strings(out.EnvironmentNames)
		sort.Strings(out.SecretReferences)
		summary.Containers = append(summary.Containers, out)
	}

	for _, condition := range revision.Conditions {
		if condition == nil {
			continue
		}
		reason := ""
		if condition.GetRevisionReason() != runpb.Condition_REVISION_REASON_UNDEFINED {
			reason = condition.GetRevisionReason().String()
		} else if condition.GetReason() != runpb.Condition_COMMON_REASON_UNDEFINED {
			reason = condition.GetReason().String()
		}
		message := condition.Message
		if runes := []rune(message); len(runes) > 512 {
			message = string(runes[:512]) + "…"
		}
		out := models.RevisionCondition{
			Type:     condition.Type,
			State:    condition.State.String(),
			Reason:   reason,
			Severity: condition.Severity.String(),
			Message:  message,
		}
		if condition.LastTransitionTime != nil {
			out.LastTransitionTime = condition.LastTransitionTime.AsTime().UTC().Format(timeFormatRFC3339)
		}
		summary.Conditions = append(summary.Conditions, out)
		if condition.Type == "Ready" && condition.State == runpb.Condition_CONDITION_SUCCEEDED {
			summary.Ready = true
		}
	}

	summary.ConfigFingerprint = revisionConfigFingerprint(revision)
	return summary
}

const timeFormatRFC3339 = "2006-01-02T15:04:05Z"

func parseRevisionResourceName(fullName string) (region, service, revision string) {
	parts := strings.Split(fullName, "/")
	if len(parts) == 8 && parts[0] == "projects" && parts[2] == "locations" && parts[4] == "services" && parts[6] == "revisions" {
		return parts[3], parts[5], parts[7]
	}
	return "", "", fullName
}

func revisionConfigFingerprint(revision *runpb.Revision) string {
	type fingerprintContainer struct {
		Name            string
		Image           string
		Env             []string
		Limits          map[string]string
		CPUIdle         bool
		StartupCPUBoost bool
	}
	type fingerprint struct {
		ServiceAccount string
		Concurrency    int32
		Timeout        string
		MinInstances   int32
		MaxInstances   int32
		VPCConnector   string
		VPCEgress      string
		Containers     []fingerprintContainer
	}

	value := fingerprint{
		ServiceAccount: revision.ServiceAccount,
		Concurrency:    revision.MaxInstanceRequestConcurrency,
	}
	if revision.Timeout != nil {
		value.Timeout = revision.Timeout.AsDuration().String()
	}
	if revision.Scaling != nil {
		value.MinInstances = revision.Scaling.MinInstanceCount
		value.MaxInstances = revision.Scaling.MaxInstanceCount
	}
	if revision.VpcAccess != nil {
		value.VPCConnector = revision.VpcAccess.Connector
		value.VPCEgress = revision.VpcAccess.Egress.String()
	}
	for _, container := range revision.Containers {
		if container == nil {
			continue
		}
		out := fingerprintContainer{Name: container.Name, Image: container.Image}
		if container.Resources != nil {
			out.Limits = container.Resources.Limits
			out.CPUIdle = container.Resources.CpuIdle
			out.StartupCPUBoost = container.Resources.StartupCpuBoost
		}
		for _, env := range container.Env {
			if env == nil {
				continue
			}
			entry := env.Name + "=value:" + env.GetValue()
			if source := env.GetValueSource(); source != nil && source.SecretKeyRef != nil {
				entry = env.Name + "=secret:" + source.SecretKeyRef.Secret + ":" + source.SecretKeyRef.Version
			}
			out.Env = append(out.Env, entry)
		}
		sort.Strings(out.Env)
		value.Containers = append(value.Containers, out)
	}
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("sha256:%x", digest)
}

// UpdateTraffic updates the traffic split for a Cloud Run service.
// When DryRun=true it returns a description of the change without executing it.
func (a *gcpAdapter) UpdateTraffic(ctx context.Context, req models.UpdateTrafficRequest) (models.UpdateTrafficResponse, error) {
	if err := a.rateWait(ctx, "cloudrun.UpdateTraffic"); err != nil {
		return models.UpdateTrafficResponse{}, err
	}
	if err := validateTrafficTargets(req.Traffic); err != nil {
		return models.UpdateTrafficResponse{}, fmt.Errorf("cloudrun.UpdateTraffic: %w", err)
	}

	// Always fetch current state for before/after reporting.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	name := fmt.Sprintf("projects/%s/locations/%s/services/%s", req.ProjectID, req.Region, req.ServiceName)
	current, err := a.runSvc.GetService(ctx, &runpb.GetServiceRequest{Name: name})
	if err != nil {
		return models.UpdateTrafficResponse{}, wrapGCPError("cloudrun.UpdateTraffic.get", err)
	}

	if req.ExpectedEtag != "" && current.Etag != req.ExpectedEtag {
		return models.UpdateTrafficResponse{}, &ports.ConfirmationRequiredError{
			Op:      "cloudrun.UpdateTraffic",
			Message: "the Cloud Run service changed after preview; run dry_run=true again before confirming",
		}
	}
	before := modelTrafficTargets(current.Traffic)
	if err := a.validateTrafficRevisions(ctx, name, req.Traffic); err != nil {
		return models.UpdateTrafficResponse{}, err
	}
	noChange := trafficTargetsEqual(before, req.Traffic)

	if req.DryRun {
		return models.UpdateTrafficResponse{
			DryRun:         true,
			ServiceName:    req.ServiceName,
			Before:         before,
			After:          req.Traffic,
			NoChangeNeeded: noChange,
			Description:    fmt.Sprintf("DRY RUN: would update traffic for service %q", req.ServiceName),
			StateVersion:   current.Etag,
		}, nil
	}
	if noChange {
		return models.UpdateTrafficResponse{
			ServiceName: req.ServiceName, Before: before, After: before, NoChangeNeeded: true,
			Description: fmt.Sprintf("service %q already has the requested traffic split", req.ServiceName),
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

	op, err := a.runSvc.UpdateService(ctx, buildTrafficUpdateRequest(name, current.Etag, pbTraffic))
	if err != nil {
		return models.UpdateTrafficResponse{}, wrapGCPError("cloudrun.UpdateTraffic.update", err)
	}
	updated, err := op.Wait(ctx)
	if err != nil {
		return models.UpdateTrafficResponse{}, wrapGCPError("cloudrun.UpdateTraffic.wait", err)
	}
	after := modelTrafficTargets(updated.Traffic)

	return models.UpdateTrafficResponse{
		DryRun:      false,
		ServiceName: req.ServiceName,
		Before:      before,
		After:       after,
		Description: fmt.Sprintf("traffic update completed for service %q", req.ServiceName),
	}, nil
}

func buildTrafficUpdateRequest(name, etag string, traffic []*runpb.TrafficTarget) *runpb.UpdateServiceRequest {
	return &runpb.UpdateServiceRequest{
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"traffic"}},
		Service:    &runpb.Service{Name: name, Traffic: traffic, Etag: etag},
	}
}

func validateTrafficTargets(targets []models.TrafficTarget) error {
	if len(targets) == 0 {
		return fmt.Errorf("at least one traffic target is required")
	}
	seenRevisions := make(map[string]struct{}, len(targets))
	seenTags := make(map[string]struct{}, len(targets))
	total := int64(0)
	for index, target := range targets {
		if target.Revision == "" || strings.TrimSpace(target.Revision) != target.Revision || strings.Contains(target.Revision, "/") {
			return fmt.Errorf("traffic[%d].revision must be a non-empty revision name", index)
		}
		if target.Percent <= 0 || target.Percent > 100 {
			return fmt.Errorf("traffic[%d].percent must be between 1 and 100", index)
		}
		if _, exists := seenRevisions[target.Revision]; exists {
			return fmt.Errorf("traffic revision %q is duplicated", target.Revision)
		}
		seenRevisions[target.Revision] = struct{}{}
		if target.Tag != "" {
			if strings.TrimSpace(target.Tag) != target.Tag {
				return fmt.Errorf("traffic[%d].tag must not contain surrounding whitespace", index)
			}
			if _, exists := seenTags[target.Tag]; exists {
				return fmt.Errorf("traffic tag %q is duplicated", target.Tag)
			}
			seenTags[target.Tag] = struct{}{}
		}
		total += int64(target.Percent)
	}
	if total != 100 {
		return fmt.Errorf("traffic percentages must sum to 100; got %d", total)
	}
	return nil
}

func (a *gcpAdapter) validateTrafficRevisions(ctx context.Context, serviceName string, targets []models.TrafficTarget) error {
	if a.runRevisions == nil {
		return fmt.Errorf("cloudrun.UpdateTraffic: revisions client is not initialised")
	}
	existing := make(map[string]struct{})
	it := a.runRevisions.ListRevisions(ctx, &runpb.ListRevisionsRequest{Parent: serviceName})
	for {
		revision, err := it.Next()
		if err != nil {
			if isIteratorDone(err) {
				break
			}
			return wrapGCPError("cloudrun.UpdateTraffic.listRevisions", err)
		}
		existing[resourceBaseName(revision.Name)] = struct{}{}
	}
	for _, target := range targets {
		if _, ok := existing[target.Revision]; !ok {
			return &ports.NotFoundError{Op: "cloudrun.UpdateTraffic", Err: fmt.Errorf("revision %q does not exist in the selected service", target.Revision)}
		}
	}
	return nil
}

func resourceBaseName(value string) string {
	if index := strings.LastIndex(value, "/"); index >= 0 {
		return value[index+1:]
	}
	return value
}

func modelTrafficTargets(targets []*runpb.TrafficTarget) []models.TrafficTarget {
	out := make([]models.TrafficTarget, 0, len(targets))
	for _, target := range targets {
		out = append(out, models.TrafficTarget{Revision: target.Revision, Percent: target.Percent, Tag: target.Tag})
	}
	return out
}

func trafficTargetsEqual(left, right []models.TrafficTarget) bool {
	if len(left) != len(right) {
		return false
	}
	canonical := func(source []models.TrafficTarget) []models.TrafficTarget {
		out := append([]models.TrafficTarget(nil), source...)
		sort.Slice(out, func(i, j int) bool {
			if out[i].Revision != out[j].Revision {
				return out[i].Revision < out[j].Revision
			}
			return out[i].Tag < out[j].Tag
		})
		return out
	}
	a, b := canonical(left), canonical(right)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
