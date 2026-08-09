package gcp

import (
	"testing"

	runpb "cloud.google.com/go/run/apiv2/runpb"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func TestValidateTrafficTargets(t *testing.T) {
	valid := []models.TrafficTarget{{Revision: "service-00001", Percent: 80}, {Revision: "service-00002", Percent: 20, Tag: "canary"}}
	if err := validateTrafficTargets(valid); err != nil {
		t.Fatalf("valid targets rejected: %v", err)
	}
	for name, targets := range map[string][]models.TrafficTarget{
		"empty":              nil,
		"bad total":          {{Revision: "v1", Percent: 90}},
		"zero percentage":    {{Revision: "v1", Percent: 100}, {Revision: "v2", Percent: 0}},
		"duplicate revision": {{Revision: "v1", Percent: 50}, {Revision: "v1", Percent: 50}},
		"duplicate tag":      {{Revision: "v1", Percent: 50, Tag: "stable"}, {Revision: "v2", Percent: 50, Tag: "stable"}},
		"qualified revision": {{Revision: "services/s/revisions/v1", Percent: 100}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateTrafficTargets(targets); err == nil {
				t.Fatalf("invalid targets accepted: %+v", targets)
			}
		})
	}
}

func TestBuildTrafficUpdateRequestRestrictsUpdateMaskAndCarriesEtag(t *testing.T) {
	req := buildTrafficUpdateRequest("projects/p/locations/r/services/s", "etag-1", []*runpb.TrafficTarget{{Revision: "v1", Percent: 100}})
	if req.UpdateMask == nil || len(req.UpdateMask.Paths) != 1 || req.UpdateMask.Paths[0] != "traffic" {
		t.Fatalf("update mask = %+v", req.UpdateMask)
	}
	if req.Service == nil || req.Service.Etag != "etag-1" || req.Service.Name == "" || len(req.Service.Traffic) != 1 {
		t.Fatalf("service = %+v", req.Service)
	}
}

func TestTrafficTargetsEqualIgnoresOrdering(t *testing.T) {
	left := []models.TrafficTarget{{Revision: "v1", Percent: 80}, {Revision: "v2", Percent: 20, Tag: "canary"}}
	right := []models.TrafficTarget{{Revision: "v2", Percent: 20, Tag: "canary"}, {Revision: "v1", Percent: 80}}
	if !trafficTargetsEqual(left, right) {
		t.Fatal("same targets in different order should compare equal")
	}
}
