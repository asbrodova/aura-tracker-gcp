package gcp

import (
	"reflect"
	"testing"
)

func TestUniqueRegionsFiltersNonRegionalLocations(t *testing.T) {
	got := uniqueRegions([]string{
		"us-central1", "US-CENTRAL1", "us-central1-a", "global", "nam5", "", " europe-west1 ",
	})
	want := []string{"europe-west1", "us-central1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uniqueRegions() = %#v, want %#v", got, want)
	}
}

func TestCloneRegionDiscoveryDoesNotShareRegionSlice(t *testing.T) {
	original := regionDiscovery{Regions: []string{"us-central1"}, Source: "scheduler.locations.list", Complete: true}
	clone := cloneRegionDiscovery(original)
	clone.Regions[0] = "europe-west1"
	if original.Regions[0] != "us-central1" {
		t.Fatalf("cached discovery mutated: %+v", original)
	}
}

func TestDiscoveryToolErrorMarksFallbackAsPartial(t *testing.T) {
	complete := discoveryToolError(regionDiscovery{Complete: true}, "scheduler.list")
	if len(complete) != 0 {
		t.Fatalf("complete discovery emitted errors: %+v", complete)
	}
	partial := discoveryToolError(regionDiscovery{Warning: "fallback", Complete: false}, "scheduler.list")
	if len(partial) != 1 || !partial[0].Retriable || partial[0].Message != "fallback" {
		t.Fatalf("partial discovery error = %+v", partial)
	}
}

func TestInventoryPageSizeDefaultsAndBounds(t *testing.T) {
	if got, err := inventoryPageSize(0); err != nil || got != defaultInventoryPageSize {
		t.Fatalf("default page size = %d, %v", got, err)
	}
	if got, err := inventoryPageSize(maxInventoryPageSize); err != nil || got != maxInventoryPageSize {
		t.Fatalf("maximum page size = %d, %v", got, err)
	}
	for _, invalid := range []int{-1, maxInventoryPageSize + 1} {
		if _, err := inventoryPageSize(invalid); err == nil {
			t.Fatalf("invalid page size %d was accepted", invalid)
		}
	}
}

func TestAppendInventoryBoundedStopsAtLimit(t *testing.T) {
	values := make([]int, 0, maxUnpagedInventoryItems)
	for index := 0; index < maxUnpagedInventoryItems; index++ {
		if !appendInventoryBounded(&values, index) {
			t.Fatalf("stopped early at %d", index)
		}
	}
	if len(values) != maxUnpagedInventoryItems || appendInventoryBounded(&values, maxUnpagedInventoryItems) {
		t.Fatalf("bound was not enforced: length=%d", len(values))
	}
	truncated, err := inventoryLimitResult(errInventoryLimitReached)
	if !truncated || err != nil {
		t.Fatalf("limit result = (%v, %v)", truncated, err)
	}
}

func TestRegionalInventoryLimitBoundsTotalFanout(t *testing.T) {
	for _, regionCount := range []int{0, 1, 2, 40, maxUnpagedInventoryItems, maxUnpagedInventoryItems + 1} {
		limit := regionalInventoryLimit(regionCount)
		if limit < 1 || (regionCount > 1 && regionCount <= maxUnpagedInventoryItems && limit*regionCount > maxUnpagedInventoryItems) {
			t.Fatalf("regionalInventoryLimit(%d) = %d", regionCount, limit)
		}
	}
}
