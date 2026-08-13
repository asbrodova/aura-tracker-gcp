package safety

import (
	"testing"
	"time"
)

func TestPlanStoreBindsOwnerAndPreventsConcurrentClaims(t *testing.T) {
	store := NewPlanStore()
	if err := store.putScoped("plan", "actor/session", "target", planKindScaleDeployment, "payload"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.claim("plan", "other/session", planKindScaleDeployment); ok {
		t.Fatal("different owner claimed plan")
	}
	if _, ok := store.claim("plan", "actor/session", planKindUpdateTraffic); ok {
		t.Fatal("wrong operation claimed plan")
	}
	if _, ok := store.claim("plan", "actor/session", planKindScaleDeployment); !ok {
		t.Fatal("owner could not claim plan")
	}
	if _, ok := store.claim("plan", "actor/session", planKindScaleDeployment); ok {
		t.Fatal("in-flight plan was claimed twice")
	}
	store.finish("plan", false)
	if _, ok := store.claim("plan", "actor/session", planKindScaleDeployment); !ok {
		t.Fatal("failed execution did not release plan")
	}
	store.finish("plan", true)
	if _, ok := store.claim("plan", "actor/session", planKindScaleDeployment); ok {
		t.Fatal("successful execution did not consume plan")
	}
}

func TestPlanStorePurgesExpiredEntriesBeforeCapacityCheck(t *testing.T) {
	store := NewPlanStore()
	store.entries["expired"] = planEntry{expiresAt: time.Now().Add(-time.Second)}
	if err := store.putScoped("fresh", "", "", planKindScaleDeployment, "payload"); err != nil {
		t.Fatal(err)
	}
	if _, exists := store.entries["expired"]; exists {
		t.Fatal("expired entry was not purged")
	}
}
