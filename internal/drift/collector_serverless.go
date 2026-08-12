package drift

import (
	"context"
	"fmt"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (c *GCPCollector) collectCloudRun(ctx context.Context, req CollectionRequest) (CollectionResult, error) {
	services, err := c.source.ListServices(ctx, models.ListServicesRequest{ProjectID: req.ProjectID})
	if err != nil {
		return CollectionResult{}, err
	}
	result := CollectionResult{Resources: []Resource{}}
	markInventoryTruncated(&result, services.Truncated, "Cloud Run service")
	for _, service := range services.Services {
		if !includeResource(req, service.Name, service.Region) {
			continue
		}
		details, detailErr := c.source.GetServiceDetails(ctx, models.GetServiceDetailsRequest{ProjectID: req.ProjectID, Region: service.Region, ServiceName: bareName(service.Name)})
		if detailErr != nil {
			result.Partial = true
			result.Warnings = append(result.Warnings, fmt.Sprintf("Cloud Run service %s details: %v", bareName(service.Name), detailErr))
			result.Resources = append(result.Resources, resource("cloudrun", "cloudrun.service", service.Name, service.Region, "", service))
			continue
		}
		for i := range details.Traffic {
			if details.Traffic[i].Revision == details.LatestRevision {
				details.Traffic[i].Revision = "LATEST"
			}
		}
		configuration := map[string]any{"service": configMap(details)}
		revisions, revisionErr := c.source.ListRevisions(ctx, models.ListRevisionsRequest{
			ProjectID: req.ProjectID, Region: service.Region, ServiceName: bareName(service.Name), Limit: 20,
		})
		if revisionErr != nil {
			result.Partial = true
			result.Warnings = append(result.Warnings, fmt.Sprintf("Cloud Run service %s revision configuration: %v", bareName(service.Name), revisionErr))
		} else if revision, ok := currentRevision(details, revisions.Revisions); ok {
			// Revision names are generated deployment history. The selected
			// revision's effective template is configuration; its identity is not.
			revision.Name = ""
			revision.Conditions = nil
			configuration["revision_template"] = configMap(revision)
		}
		result.Resources = append(result.Resources, resource("cloudrun", "cloudrun.service", service.Name, service.Region, "", configuration))
	}

	jobs, err := c.source.ListJobs(ctx, models.ListJobsRequest{ProjectID: req.ProjectID})
	if err != nil {
		result.Partial = true
		result.Warnings = append(result.Warnings, fmt.Sprintf("Cloud Run jobs: %v", err))
		return result, nil
	}
	for _, job := range jobs.Jobs {
		if !includeResource(req, job.Name, job.Region) {
			continue
		}
		details, detailErr := c.source.GetJobDetails(ctx, models.GetJobDetailsRequest{ProjectID: req.ProjectID, Region: job.Region, JobName: bareName(job.Name)})
		if detailErr != nil {
			result.Partial = true
			result.Warnings = append(result.Warnings, fmt.Sprintf("Cloud Run job %s details: %v", bareName(job.Name), detailErr))
			result.Resources = append(result.Resources, resource("cloudrun", "cloudrun.job", job.Name, job.Region, "", job))
			continue
		}
		result.Resources = append(result.Resources, resource("cloudrun", "cloudrun.job", job.Name, job.Region, "", details))
	}
	markInventoryTruncated(&result, jobs.Truncated, "Cloud Run job")
	return result, nil
}

func currentRevision(details models.ServiceDetails, revisions []models.RevisionSummary) (models.RevisionSummary, bool) {
	wanted := bareName(details.LatestRevision)
	for _, revision := range revisions {
		if bareName(revision.Name) == wanted {
			return revision, true
		}
	}
	for _, revision := range revisions {
		if revision.Ready {
			return revision, true
		}
	}
	if len(revisions) > 0 {
		return revisions[0], true
	}
	return models.RevisionSummary{}, false
}

func (c *GCPCollector) collectFunctions(ctx context.Context, req CollectionRequest) (CollectionResult, error) {
	response, err := c.source.ListFunctions(ctx, models.ListFunctionsRequest{ProjectID: req.ProjectID, Generation: "both"})
	if err != nil {
		return CollectionResult{}, err
	}
	result := CollectionResult{Resources: []Resource{}}
	markInventoryTruncated(&result, response.Truncated, "Cloud Functions")
	for _, function := range response.Functions {
		if !includeResource(req, function.Name, function.Region) {
			continue
		}
		details, detailErr := c.source.GetFunctionDetails(ctx, models.GetFunctionDetailsRequest{
			ProjectID: req.ProjectID, Region: function.Region, FunctionName: bareName(function.Name), Generation: function.Generation,
		})
		if detailErr != nil {
			result.Partial = true
			result.Warnings = append(result.Warnings, fmt.Sprintf("function %s details: %v", bareName(function.Name), detailErr))
			result.Resources = append(result.Resources, resource("functions", "cloudfunctions.function", function.Name, function.Region, fmt.Sprint(function.Generation), function))
			continue
		}
		result.Resources = append(result.Resources, resource("functions", "cloudfunctions.function", function.Name, function.Region, fmt.Sprint(function.Generation), details))
	}
	return result, nil
}

func (c *GCPCollector) collectEventarc(ctx context.Context, req CollectionRequest) (CollectionResult, error) {
	response, err := c.source.ListTriggers(ctx, models.ListTriggersRequest{ProjectID: req.ProjectID})
	if err != nil {
		return CollectionResult{}, err
	}
	result := CollectionResult{Resources: []Resource{}}
	for _, trigger := range response.Triggers {
		if includeResource(req, trigger.Name, trigger.Region) {
			result.Resources = append(result.Resources, resource("eventarc", "eventarc.trigger", trigger.Name, trigger.Region, "", trigger))
		}
	}
	partial, warnings := toolErrors(response.Errors)
	mergePartial(&result, partial, warnings)
	markInventoryTruncated(&result, response.Truncated, "Eventarc trigger")
	return result, nil
}

func (c *GCPCollector) collectScheduler(ctx context.Context, req CollectionRequest) (CollectionResult, error) {
	response, err := c.source.ListSchedulerJobs(ctx, models.ListSchedulerJobsRequest{ProjectID: req.ProjectID})
	if err != nil {
		return CollectionResult{}, err
	}
	result := CollectionResult{Resources: []Resource{}}
	for _, job := range response.Jobs {
		if includeResource(req, job.Name, job.Region) {
			result.Resources = append(result.Resources, resource("scheduler", "scheduler.job", job.Name, job.Region, "", job))
		}
	}
	partial, warnings := toolErrors(response.Errors)
	mergePartial(&result, partial, warnings)
	markInventoryTruncated(&result, response.Truncated, "Cloud Scheduler job")
	return result, nil
}

func (c *GCPCollector) collectWorkflows(ctx context.Context, req CollectionRequest) (CollectionResult, error) {
	response, err := c.source.ListWorkflows(ctx, models.ListWorkflowsRequest{ProjectID: req.ProjectID})
	if err != nil {
		return CollectionResult{}, err
	}
	result := CollectionResult{Resources: []Resource{}}
	for _, workflow := range response.Workflows {
		if includeResource(req, workflow.Name, workflow.Region) {
			result.Resources = append(result.Resources, resource("workflows", "workflows.workflow", workflow.Name, workflow.Region, "", workflow))
		}
	}
	partial, warnings := toolErrors(response.Errors)
	mergePartial(&result, partial, warnings)
	markInventoryTruncated(&result, response.Truncated, "Cloud Workflow")
	return result, nil
}

func (c *GCPCollector) collectTasks(ctx context.Context, req CollectionRequest) (CollectionResult, error) {
	response, err := c.source.ListTaskQueues(ctx, models.ListTaskQueuesRequest{ProjectID: req.ProjectID})
	if err != nil {
		return CollectionResult{}, err
	}
	result := CollectionResult{Resources: []Resource{}, Partial: true, Warnings: []string{"queue presence is compared, but rate and retry settings are not exposed by the current Cloud Tasks collector"}}
	for _, queue := range response.Queues {
		if includeResource(req, queue.Name, queue.Region) {
			result.Resources = append(result.Resources, resource("tasks", "cloudtasks.queue", queue.Name, queue.Region, "", queue))
		}
	}
	partial, warnings := toolErrors(response.Errors)
	mergePartial(&result, partial, warnings)
	markInventoryTruncated(&result, response.Truncated, "Cloud Tasks queue")
	return result, nil
}

func (c *GCPCollector) collectSecrets(ctx context.Context, req CollectionRequest) (CollectionResult, error) {
	response, err := c.source.ListSecrets(ctx, models.ListSecretsRequest{ProjectID: req.ProjectID})
	if err != nil {
		return CollectionResult{}, err
	}
	result := CollectionResult{Resources: []Resource{}}
	markInventoryTruncated(&result, response.Truncated, "Secret Manager")
	for _, secret := range response.Secrets {
		if includeResource(req, secret.Name, "") {
			result.Resources = append(result.Resources, resource("secretmanager", "secretmanager.secret", secret.Name, "", "", secret))
		}
	}
	return result, nil
}

func (c *GCPCollector) collectVPCAccess(ctx context.Context, req CollectionRequest) (CollectionResult, error) {
	response, err := c.source.ListVPCConnectors(ctx, models.ListVPCConnectorsRequest{ProjectID: req.ProjectID})
	if err != nil {
		return CollectionResult{}, err
	}
	result := CollectionResult{Resources: []Resource{}}
	for _, connector := range response.Connectors {
		if includeResource(req, connector.Name, connector.Region) {
			result.Resources = append(result.Resources, resource("vpcaccess", "vpcaccess.connector", connector.Name, connector.Region, "", connector))
		}
	}
	partial, warnings := toolErrors(response.Errors)
	mergePartial(&result, partial, warnings)
	return result, nil
}

func (c *GCPCollector) collectCloudSQL(ctx context.Context, req CollectionRequest) (CollectionResult, error) {
	response, err := c.source.ListSQLInstances(ctx, models.ListSQLInstancesRequest{ProjectID: req.ProjectID})
	if err != nil {
		return CollectionResult{}, err
	}
	result := CollectionResult{Resources: []Resource{}, Partial: true, Warnings: []string{"Cloud SQL tier, version, region, and labels are compared; advanced instance settings require a future detailed collector"}}
	for _, instance := range response.Instances {
		if includeResource(req, instance.Name, instance.Region) {
			result.Resources = append(result.Resources, resource("cloudsql", "cloudsql.instance", instance.Name, instance.Region, "", instance))
		}
	}
	return result, nil
}
