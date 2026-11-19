package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// Incident resource types.
const (
	resourceCloudRun    = "cloud_run_service"
	resourceFunction    = "cloud_function"
	resourceGKEWorkload = "gke_workload"
)

var gkeWorkloadKinds = map[string]string{
	"deployment": "Deployment", "statefulset": "StatefulSet", "daemonset": "DaemonSet",
}

// normalizeResourceType validates the resource-specific request fields.
func normalizeResourceType(req *models.DiagnoseIncidentRequest) error {
	req.ResourceType = strings.ToLower(strings.TrimSpace(req.ResourceType))
	req.ClusterName = strings.TrimSpace(req.ClusterName)
	req.Namespace = strings.TrimSpace(req.Namespace)
	req.WorkloadKind = strings.TrimSpace(req.WorkloadKind)
	if req.ResourceType == "" {
		req.ResourceType = resourceCloudRun
	}
	switch req.ResourceType {
	case resourceCloudRun:
		if req.ClusterName != "" || req.Namespace != "" || req.WorkloadKind != "" {
			return errors.New("cluster_name, namespace and workload_kind apply only to resource_type=gke_workload")
		}
	case resourceFunction:
		if req.ServiceName == "" {
			return errors.New("service_name (the function name) is required for resource_type=cloud_function")
		}
		if req.ClusterName != "" || req.Namespace != "" || req.WorkloadKind != "" {
			return errors.New("cluster_name, namespace and workload_kind apply only to resource_type=gke_workload")
		}
	case resourceGKEWorkload:
		if req.ServiceName == "" || req.ClusterName == "" || req.Region == "" {
			return errors.New("service_name (the workload name), cluster_name and region (the cluster location) are required for resource_type=gke_workload")
		}
		if req.Namespace == "" {
			req.Namespace = "default"
		}
		if req.WorkloadKind == "" {
			req.WorkloadKind = "Deployment"
		}
		kind, ok := gkeWorkloadKinds[strings.ToLower(req.WorkloadKind)]
		if !ok {
			return errors.New("workload_kind must be Deployment, StatefulSet, or DaemonSet")
		}
		req.WorkloadKind = kind
	default:
		return errors.New("resource_type must be cloud_run_service, cloud_function, or gke_workload")
	}
	return nil
}

// resolveFunctionScope finds the function (in every region when region is
// omitted). A Gen 2 function runs on a Cloud Run service, so it is diagnosed
// with the full Cloud Run evidence set under that service's name.
func (e *Engine) resolveFunctionScope(ctx context.Context, req models.DiagnoseIncidentRequest, scope models.IncidentScope) (models.IncidentScope, string, error) {
	functions, err := e.source.ListFunctions(ctx, models.ListFunctionsRequest{ProjectID: req.ProjectID, Region: req.Region, Generation: "both"})
	if err != nil {
		return scope, "", err
	}
	matches := []models.FunctionSummary{}
	for _, function := range functions.Functions {
		if baseName(function.Name) == req.ServiceName && (req.Region == "" || function.Region == req.Region) {
			matches = append(matches, function)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].Region < matches[j].Region })
	if len(matches) == 0 {
		return scope, "", fmt.Errorf("cloud function %q was not found", req.ServiceName)
	}
	if len(matches) > req.MaxServices {
		matches = matches[:req.MaxServices]
	}
	warning := ""
	for _, function := range matches {
		target := models.IncidentTarget{
			ServiceName: req.ServiceName, Region: function.Region, Labels: function.Labels,
			ResourceType: resourceFunction, FunctionName: req.ServiceName, Generation: function.Generation,
		}
		if function.Generation == 2 {
			details, detailErr := e.source.GetFunctionDetails(ctx, models.GetFunctionDetailsRequest{
				ProjectID: req.ProjectID, Region: function.Region, FunctionName: req.ServiceName, Generation: 2,
			})
			if detailErr != nil {
				return scope, "", fmt.Errorf("cloud function %q details: %w", req.ServiceName, detailErr)
			}
			if service := baseName(details.CloudRunService); service != "" {
				target.ServiceName = service
				warning = "Gen 2 Cloud Functions run on Cloud Run; their requests, revisions and errors were read from the underlying Cloud Run service."
			}
		}
		scope.Targets = append(scope.Targets, target)
	}
	scope.Inferred = req.Region == ""
	if len(scope.Targets) > 1 {
		scope.Confidence = "medium"
		warning = strings.TrimSpace(warning + " The function name exists in multiple regions; all matches within the service limit were analyzed.")
	}
	return scope, warning, nil
}

func gkeScope(req models.DiagnoseIncidentRequest, scope models.IncidentScope) models.IncidentScope {
	scope.Targets = append(scope.Targets, models.IncidentTarget{
		ServiceName: req.ServiceName, Region: req.Region, ResourceType: resourceGKEWorkload,
		ClusterName: req.ClusterName, Namespace: req.Namespace, WorkloadKind: req.WorkloadKind,
	})
	return scope
}

// isGen1Function reports whether the target is a Gen 1 function, which has
// its own metrics and logs instead of a Cloud Run service.
func isGen1Function(target models.IncidentTarget) bool {
	return target.ResourceType == resourceFunction && target.Generation != 2
}

func isGKEWorkload(target models.IncidentTarget) bool {
	return target.ResourceType == resourceGKEWorkload
}

// deploymentNoun names the unit of change for a target in cause titles.
func deploymentTitle(target models.IncidentTarget) string {
	switch {
	case isGKEWorkload(target):
		return "Recent GKE rollout regression"
	case target.ResourceType == resourceFunction:
		return "Recent Cloud Functions deployment regression"
	default:
		return "Recent Cloud Run revision regression"
	}
}

// errorTerm names what the error rate counts for a target.
func errorTerm(target models.IncidentTarget) string {
	if isGen1Function(target) {
		return "failed-execution"
	}
	return "5xx"
}

// logQueryArguments are gcp_logging_query_recent arguments that select the
// target's own application logs.
func logQueryArguments(req models.DiagnoseIncidentRequest, target models.IncidentTarget, maxEntries int) map[string]any {
	resourceType, labels := "cloud_run_revision", map[string]string{"service_name": target.ServiceName}
	switch {
	case isGen1Function(target):
		resourceType, labels = "cloud_function", map[string]string{"function_name": target.FunctionName, "region": target.Region}
	case isGKEWorkload(target):
		resourceType, labels = "k8s_container", map[string]string{"cluster_name": target.ClusterName, "location": target.Region, "namespace_name": target.Namespace}
	}
	return map[string]any{
		"project_id": req.ProjectID, "resource_type": resourceType, "resource_labels": labels,
		"min_severity": "ERROR", "lookback_minutes": req.LookbackMinutes, "max_entries": maxEntries,
	}
}
