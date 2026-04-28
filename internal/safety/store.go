package safety

import (
	"sync"
	"time"
)

// PlanTTL is the maximum time a pending execution plan remains valid.
const PlanTTL = 10 * time.Minute

type planEntry struct {
	payload   any
	expiresAt time.Time
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

func (s *PlanStore) put(id string, payload any) {
	s.mu.Lock()
	s.entries[id] = planEntry{payload: payload, expiresAt: time.Now().Add(PlanTTL)}
	s.mu.Unlock()
}

// take atomically removes and returns a plan — single-use, prevents replay attacks.
func (s *PlanStore) take(id string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok || time.Now().After(e.expiresAt) {
		delete(s.entries, id)
		return nil, false
	}
	delete(s.entries, id)
	return e.payload, true
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
