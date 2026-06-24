package gcp

import (
	"context"
	"fmt"
	"strings"
	"sync"

	eventarcpb "cloud.google.com/go/eventarc/apiv1/eventarcpb"
	"golang.org/x/sync/errgroup"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (a *gcpAdapter) ListTriggers(ctx context.Context, req models.ListTriggersRequest) (models.ListTriggersResponse, error) {
	if err := a.rateWait(ctx, "eventarc.ListTriggers"); err != nil {
		return models.ListTriggersResponse{}, err
	}

	regions, err := a.discoverRegions(ctx, req.ProjectID)
	if err != nil {
		return models.ListTriggersResponse{}, err
	}

	if req.Region != "" && req.Region != "-" {
		regions = []string{req.Region}
	}

	g, gctx := errgroup.WithContext(ctx)
	var mu sync.Mutex
	var triggers []models.TriggerSummary
	var errs []models.ToolError

	for _, r := range regions {
		r := r
		g.Go(func() error {
			items, rerr := a.listTriggersForRegion(gctx, req.ProjectID, r)
			mu.Lock()
			defer mu.Unlock()
			if rerr != nil {
				errs = append(errs, models.ToolError{
					FailingAPI: "eventarc.projects.locations.triggers.list",
					Message:    fmt.Sprintf("region %s: %v", r, rerr),
					Retriable:  false,
				})
				return nil
			}
			triggers = append(triggers, items...)
			return nil
		})
	}
	_ = g.Wait()

	if triggers == nil {
		triggers = []models.TriggerSummary{}
	}
	return models.ListTriggersResponse{Triggers: triggers, Errors: errs}, nil
}

func (a *gcpAdapter) listTriggersForRegion(ctx context.Context, projectID, region string) ([]models.TriggerSummary, error) {
	if a.eventarcClient == nil {
		return nil, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	parent := fmt.Sprintf("projects/%s/locations/%s", projectID, region)
	it := a.eventarcClient.ListTriggers(ctx, &eventarcpb.ListTriggersRequest{Parent: parent})

	var triggers []models.TriggerSummary
	for {
		trig, err := it.Next()
		if err != nil {
			if isIteratorDone(err) {
				break
			}
			return nil, wrapGCPError("eventarc.listTriggersForRegion", err)
		}
		_, name := parseEventarcResourceName(trig.Name)
		lastMod := ""
		if trig.UpdateTime != nil {
			lastMod = trig.UpdateTime.AsTime().Format(tsFormat)
		}

		destKind, destURN := resolveEventarcDestination(trig.Destination, projectID)
		transportTopic := ""
		if trig.Transport != nil {
			if ps := trig.Transport.GetPubsub(); ps != nil {
				transportTopic = ps.Topic
			}
		}

		var filters []models.EventFilter
		for _, f := range trig.EventFilters {
			filters = append(filters, models.EventFilter{
				Attribute: f.Attribute,
				Value:     f.Value,
				Operator:  f.Operator,
			})
		}

		triggers = append(triggers, models.TriggerSummary{
			Name:            name,
			Region:          region,
			ServiceAccount:  trig.ServiceAccount,
			DestinationKind: destKind,
			DestinationURN:  destURN,
			TransportTopic:  transportTopic,
			EventFilters:    filters,
			Labels:          trig.Labels,
			LastModified:    lastMod,
		})
	}
	return triggers, nil
}

func (a *gcpAdapter) GetTrigger(ctx context.Context, req models.GetTriggerRequest) (models.TriggerDetails, error) {
	if err := a.rateWait(ctx, "eventarc.GetTrigger"); err != nil {
		return models.TriggerDetails{}, err
	}
	if a.eventarcClient == nil {
		return models.TriggerDetails{}, fmt.Errorf("eventarc.GetTrigger: client not initialized")
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	name := fmt.Sprintf("projects/%s/locations/%s/triggers/%s", req.ProjectID, req.Region, req.TriggerName)
	trig, err := a.eventarcClient.GetTrigger(ctx, &eventarcpb.GetTriggerRequest{Name: name})
	if err != nil {
		return models.TriggerDetails{}, wrapGCPError("eventarc.GetTrigger", err)
	}

	_, bareName := parseEventarcResourceName(trig.Name)
	lastMod := ""
	if trig.UpdateTime != nil {
		lastMod = trig.UpdateTime.AsTime().Format(tsFormat)
	}

	destKind, destURN := resolveEventarcDestination(trig.Destination, req.ProjectID)
	transportTopic := ""
	if trig.Transport != nil {
		if ps := trig.Transport.GetPubsub(); ps != nil {
			transportTopic = ps.Topic
		}
	}

	var filters []models.EventFilter
	for _, f := range trig.EventFilters {
		filters = append(filters, models.EventFilter{
			Attribute: f.Attribute,
			Value:     f.Value,
			Operator:  f.Operator,
		})
	}

	return models.TriggerDetails{
		TriggerSummary: models.TriggerSummary{
			Name:            bareName,
			Region:          req.Region,
			ServiceAccount:  trig.ServiceAccount,
			DestinationKind: destKind,
			DestinationURN:  destURN,
			TransportTopic:  transportTopic,
			EventFilters:    filters,
			Labels:          trig.Labels,
			LastModified:    lastMod,
		},
	}, nil
}

func resolveEventarcDestination(dest *eventarcpb.Destination, _ string) (kind, urn string) {
	if dest == nil {
		return "unknown", ""
	}
	switch {
	case dest.GetCloudRun() != nil:
		cr := dest.GetCloudRun()
		return "cloud_run_service", cr.Service
	case dest.GetCloudFunction() != "":
		return "cloud_function", dest.GetCloudFunction()
	case dest.GetWorkflow() != "":
		return "workflow", dest.GetWorkflow()
	case dest.GetGke() != nil:
		gke := dest.GetGke()
		return "gke", fmt.Sprintf("%s/%s/%s", gke.Cluster, gke.Namespace, gke.Service)
	case dest.GetHttpEndpoint() != nil:
		return "http_endpoint", dest.GetHttpEndpoint().GetUri()
	default:
		return "unknown", ""
	}
}

// parseEventarcResourceName extracts region and trigger name from:
// "projects/P/locations/REGION/triggers/NAME"
func parseEventarcResourceName(fullName string) (region, name string) {
	parts := strings.Split(fullName, "/")
	if len(parts) == 6 && parts[0] == "projects" && parts[2] == "locations" && parts[4] == "triggers" {
		return parts[3], parts[5]
	}
	return "", fullName
}

