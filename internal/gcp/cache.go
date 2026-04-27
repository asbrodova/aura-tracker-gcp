package gcp

import (
	"sync"
	"time"
)

type ttlCache[V any] struct {
	mu      sync.RWMutex
	entries map[string]ttlEntry[V]
	ttl     time.Duration
}

type ttlEntry[V any] struct {
	value     V
	expiresAt time.Time
}

func newTTLCache[V any](ttl time.Duration) *ttlCache[V] {
	return &ttlCache[V]{
		entries: make(map[string]ttlEntry[V]),
		ttl:     ttl,
	}
}

func (c *ttlCache[V]) get(key string) (V, bool) {
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok || time.Now().After(e.expiresAt) {
		var zero V
		return zero, false
	}
	return e.value, true
}

func (c *ttlCache[V]) set(key string, v V) {
	c.mu.Lock()
	c.entries[key] = ttlEntry[V]{value: v, expiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}
