package gcp

import (
	"context"
	"fmt"
	"sort"
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

	discovery := a.discoverRegions(ctx, req.ProjectID, regionServiceEventarc)
	if err := ctx.Err(); err != nil {
		return models.ListTriggersResponse{}, fmt.Errorf("eventarc.ListTriggers: %w", err)
	}
	regions := discovery.Regions
	if req.Region != "" && req.Region != "-" {
		regions = []string{req.Region}
		discovery = regionDiscovery{Regions: regions, Source: "request", Complete: true}
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(regionalFanoutConcurrency)
	var mu sync.Mutex
	var triggers []models.TriggerSummary
	truncated := false
	errs := discoveryToolError(discovery, "eventarc.projects.locations.list")
	perRegionLimit := regionalInventoryLimit(len(regions))

	for _, r := range regions {
		r := r
		g.Go(func() error {
			items, regionTruncated, rerr := a.listTriggersForRegion(gctx, req.ProjectID, r, perRegionLimit)
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
			if regionTruncated {
				truncated = true
				errs = append(errs, models.ToolError{FailingAPI: "eventarc.projects.locations.triggers.list", Message: fmt.Sprintf("region %s truncated at %d items", r, perRegionLimit)})
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return models.ListTriggersResponse{}, fmt.Errorf("eventarc.ListTriggers: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return models.ListTriggersResponse{}, fmt.Errorf("eventarc.ListTriggers: %w", err)
	}

	if triggers == nil {
		triggers = []models.TriggerSummary{}
	}
	sort.Slice(triggers, func(i, j int) bool {
		if triggers[i].Region != triggers[j].Region {
			return triggers[i].Region < triggers[j].Region
		}
		return triggers[i].Name < triggers[j].Name
	})
	sortToolErrors(errs)
	return models.ListTriggersResponse{Triggers: triggers, Errors: errs, Truncated: truncated}, nil
}

func (a *gcpAdapter) listTriggersForRegion(ctx context.Context, projectID, region string, limit int) ([]models.TriggerSummary, bool, error) {
	if a.eventarcClient == nil {
		return nil, false, fmt.Errorf("eventarc client is not initialized")
	}
	if err := a.rateWait(ctx, "eventarc.listTriggersForRegion"); err != nil {
		return nil, false, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	parent := fmt.Sprintf("projects/%s/locations/%s", projectID, region)
	it := a.eventarcClient.ListTriggers(ctx, &eventarcpb.ListTriggersRequest{Parent: parent, PageSize: int32(limit)})

	var triggers []models.TriggerSummary
	for {
		trig, err := it.Next()
		if err != nil {
			if isIteratorDone(err) {
				return triggers, false, nil
			}
			return nil, false, wrapGCPError("eventarc.listTriggersForRegion", err)
		}
		if len(triggers) >= limit {
			return triggers, true, nil
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
