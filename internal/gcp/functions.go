package gcp

import (
	"context"
	"fmt"
	"strings"

	runpb "cloud.google.com/go/run/apiv2/runpb"
	"google.golang.org/api/cloudfunctions/v1"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// ListFunctions lists Cloud Functions in a project. generation may be "1",
// "2", or "both" (default). region may be empty or "-" for all regions.
func (a *gcpAdapter) ListFunctions(ctx context.Context, req models.ListFunctionsRequest) (models.ListFunctionsResponse, error) {
	if err := a.rateWait(ctx, "functions.ListFunctions"); err != nil {
		return models.ListFunctionsResponse{}, err
	}

	gen := strings.ToLower(req.Generation)
	if gen == "" {
		gen = "both"
	}

	var funcs []models.FunctionSummary

	if gen == "1" || gen == "both" {
		v1List, err := a.listFunctionsV1(ctx, req.ProjectID, req.Region)
		if err != nil {
			return models.ListFunctionsResponse{}, err
		}
		funcs = append(funcs, v1List...)
	}

	if gen == "2" || gen == "both" {
		v2List, err := a.listFunctionsV2(ctx, req.ProjectID, req.Region)
		if err != nil {
			return models.ListFunctionsResponse{}, err
		}
		funcs = append(funcs, v2List...)
	}

	if funcs == nil {
		funcs = []models.FunctionSummary{}
	}
	return models.ListFunctionsResponse{Functions: funcs}, nil
}

func (a *gcpAdapter) listFunctionsV1(ctx context.Context, projectID, region string) ([]models.FunctionSummary, error) {
	if a.fnGen1 == nil {
		return nil, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	loc := region
	if loc == "" || loc == "-" {
		loc = "-"
	}
	parent := fmt.Sprintf("projects/%s/locations/%s", projectID, loc)

	var funcs []models.FunctionSummary
	call := a.fnGen1.Projects.Locations.Functions.List(parent)
	if err := call.Pages(ctx, func(page *cloudfunctions.ListFunctionsResponse) error {
		for _, fn := range page.Functions {
			r, name := parseFnV1ResourceName(fn.Name)
			url := ""
			if fn.HttpsTrigger != nil {
				url = fn.HttpsTrigger.Url
			}
			funcs = append(funcs, models.FunctionSummary{
				Name:       name,
				Region:     r,
				Generation: 1,
				Runtime:    fn.Runtime,
				Status:     fn.Status,
				URL:        url,
				Labels:     fn.Labels,
			})
		}
		return nil
	}); err != nil {
		return nil, wrapGCPError("functions.ListFunctionsV1", err)
	}
	return funcs, nil
}

func (a *gcpAdapter) listFunctionsV2(ctx context.Context, projectID, region string) ([]models.FunctionSummary, error) {
	if a.runSvc == nil {
		return nil, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	loc := region
	if loc == "" || loc == "-" {
		loc = "-"
	}
	parent := fmt.Sprintf("projects/%s/locations/%s", projectID, loc)
	it := a.runSvc.ListServices(ctx, &runpb.ListServicesRequest{Parent: parent})

	var funcs []models.FunctionSummary
	for {
		svc, err := it.Next()
		if err != nil {
			if isIteratorDone(err) {
				break
			}
			return nil, wrapGCPError("functions.ListFunctionsV2", err)
		}
		if svc.Labels["goog-managed-by"] != "cloudfunctions" {
			continue
		}
		r, name := parseSvcResourceName(svc.Name)
		lastMod := ""
		if svc.UpdateTime != nil {
			lastMod = svc.UpdateTime.AsTime().Format(tsFormat)
		}
		funcs = append(funcs, models.FunctionSummary{
			Name:         name,
			Region:       r,
			Generation:   2,
			Status:       svc.TerminalCondition.GetMessage(),
			LastModified: lastMod,
			URL:          svc.Uri,
			Labels:       svc.Labels,
		})
	}
	return funcs, nil
}

// GetFunctionDetails retrieves full details for a single Cloud Function.
// If req.Generation is 0 the adapter auto-detects by trying Gen 2 first then Gen 1.
func (a *gcpAdapter) GetFunctionDetails(ctx context.Context, req models.GetFunctionDetailsRequest) (models.FunctionDetails, error) {
	if err := a.rateWait(ctx, "functions.GetFunctionDetails"); err != nil {
		return models.FunctionDetails{}, err
	}

	if req.Generation == 2 || req.Generation == 0 {
		details, err := a.getFunctionDetailsV2(ctx, req)
		if err == nil {
			return details, nil
		}
		if req.Generation == 2 {
			return models.FunctionDetails{}, err
		}
		// fall through to Gen 1
	}

	return a.getFunctionDetailsV1(ctx, req)
}

func (a *gcpAdapter) getFunctionDetailsV2(ctx context.Context, req models.GetFunctionDetailsRequest) (models.FunctionDetails, error) {
	if a.runSvc == nil {
		return models.FunctionDetails{}, fmt.Errorf("functions.GetFunctionDetailsV2: run client not initialized")
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	name := fmt.Sprintf("projects/%s/locations/%s/services/%s", req.ProjectID, req.Region, req.FunctionName)
	svc, err := a.runSvc.GetService(ctx, &runpb.GetServiceRequest{Name: name})
	if err != nil {
		return models.FunctionDetails{}, wrapGCPError("functions.GetFunctionDetailsV2", err)
	}
	if svc.Labels["goog-managed-by"] != "cloudfunctions" {
		return models.FunctionDetails{}, &NotFoundError{Op: "functions.GetFunctionDetailsV2", Err: fmt.Errorf("%q is not a Cloud Function", req.FunctionName)}
	}

	lastMod := ""
	if svc.UpdateTime != nil {
		lastMod = svc.UpdateTime.AsTime().Format(tsFormat)
	}
	var image, sa, vpcConn string
	if svc.Template != nil {
		sa = svc.Template.ServiceAccount
		if ann := svc.Template.GetAnnotations(); ann != nil {
			vpcConn = ann["run.googleapis.com/vpc-access-connector"]
		}
		if len(svc.Template.Containers) > 0 {
			image = svc.Template.Containers[0].Image
		}
	}
	_, bareName := parseSvcResourceName(svc.Name)
	return models.FunctionDetails{
		FunctionSummary: models.FunctionSummary{
			Name:         bareName,
			Region:       req.Region,
			Generation:   2,
			LastModified: lastMod,
			URL:          svc.Uri,
			Labels:       svc.Labels,
		},
		EntryPoint:      image,
		ServiceAccount:  sa,
		VPCConnector:    vpcConn,
		CloudRunService: svc.Name,
	}, nil
}

func (a *gcpAdapter) getFunctionDetailsV1(ctx context.Context, req models.GetFunctionDetailsRequest) (models.FunctionDetails, error) {
	if a.fnGen1 == nil {
		return models.FunctionDetails{}, fmt.Errorf("functions.GetFunctionDetailsV1: gen1 client not initialized")
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	name := fmt.Sprintf("projects/%s/locations/%s/functions/%s", req.ProjectID, req.Region, req.FunctionName)
	fn, err := a.fnGen1.Projects.Locations.Functions.Get(name).Context(ctx).Do()
	if err != nil {
		return models.FunctionDetails{}, wrapGCPError("functions.GetFunctionDetailsV1", err)
	}

	r, bareName := parseFnV1ResourceName(fn.Name)
	httpsURL := ""
	if fn.HttpsTrigger != nil {
		httpsURL = fn.HttpsTrigger.Url
	}
	return models.FunctionDetails{
		FunctionSummary: models.FunctionSummary{
			Name:       bareName,
			Region:     r,
			Generation: 1,
			Runtime:    fn.Runtime,
			Status:     fn.Status,
			URL:        httpsURL,
			Labels:     fn.Labels,
		},
		EntryPoint:     fn.EntryPoint,
		Memory:         fmt.Sprintf("%dMB", fn.AvailableMemoryMb),
		Timeout:        fn.Timeout,
		ServiceAccount: fn.ServiceAccountEmail,
		VPCConnector:   fn.VpcConnector,
	}, nil
}

// parseFnV1ResourceName extracts region and bare name from:
// "projects/P/locations/REGION/functions/NAME"
func parseFnV1ResourceName(fullName string) (region, name string) {
	parts := strings.Split(fullName, "/")
	if len(parts) == 6 && parts[0] == "projects" && parts[2] == "locations" && parts[4] == "functions" {
		return parts[3], parts[5]
	}
	return "", fullName
}
