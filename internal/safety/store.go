package safety

import (
	"fmt"
	"sync"
	"time"
)

// PlanTTL is the maximum time a pending execution plan remains valid.
const PlanTTL = 10 * time.Minute

const MaxPendingPlans = 1000

type planKind uint8

const (
	planKindUnknown planKind = iota
	planKindScaleDeployment
	planKindUpdateTraffic
	planKindExportRecommendations
)

type planEntry struct {
	payload   any
	kind      planKind
	owner     string
	target    string
	expiresAt time.Time
	inFlight  bool
}

// PlanStore is a thread-safe, TTL-bounded store for pending execution plans.
// Plans are created by dry-run calls and consumed (deleted) on confirmation.
type PlanStore struct {
	mu      sync.Mutex
	entries map[string]planEntry
}

func NewPlanStore() *PlanStore {
	return &PlanStore{entries: make(map[string]planEntry)}
}

func (s *PlanStore) putScoped(id, owner, target string, kind planKind, payload any) error {
	if kind == planKindUnknown {
		return fmt.Errorf("pending mutation plan kind is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.purgeExpiredLocked(now)
	if len(s.entries) >= MaxPendingPlans {
		return fmt.Errorf("pending mutation plan capacity reached (%d)", MaxPendingPlans)
	}
	s.entries[id] = planEntry{payload: payload, kind: kind, owner: owner, target: target, expiresAt: now.Add(PlanTTL)}
	return nil
}

// take atomically removes and returns a plan — single-use, prevents replay attacks.
func (s *PlanStore) take(id string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpiredLocked(time.Now())
	entry, ok := s.entries[id]
	if !ok || entry.inFlight {
		return nil, false
	}
	delete(s.entries, id)
	return entry.payload, true
}

// claim atomically leases a plan for execution. A failed execution may release
// it with finish(id, false); successful execution permanently consumes it.
func (s *PlanStore) claim(id, owner string, kind planKind) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.purgeExpiredLocked(now)
	e, ok := s.entries[id]
	if !ok || e.inFlight || e.owner != owner || e.kind != kind {
		return nil, false
	}
	e.inFlight = true
	s.entries[id] = e
	return e.payload, true
}

func (s *PlanStore) finish(id string, success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return
	}
	if success || time.Now().After(e.expiresAt) {
		delete(s.entries, id)
		return
	}
	e.inFlight = false
	s.entries[id] = e
}

func (s *PlanStore) expiresIn(id string) time.Duration {
	s.mu.Lock()
	e, ok := s.entries[id]
	s.mu.Unlock()
	if !ok {
		return 0
	}
	return time.Until(e.expiresAt)
}

func (s *PlanStore) purgeExpiredLocked(now time.Time) {
	for id, entry := range s.entries {
		if now.After(entry.expiresAt) {
			delete(s.entries, id)
		}
	}
}
