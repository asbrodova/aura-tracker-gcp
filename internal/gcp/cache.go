package gcp

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const defaultCacheMaxEntries = 1024

type ttlCache[V any] struct {
	mu      sync.RWMutex
	entries map[string]ttlEntry[V]
	ttl     time.Duration
	max     int
}

type ttlEntry[V any] struct {
	value     V
	expiresAt time.Time
}

func newTTLCache[V any](ttl time.Duration) *ttlCache[V] {
	return &ttlCache[V]{
		entries: make(map[string]ttlEntry[V]),
		ttl:     ttl,
		max:     defaultCacheMaxEntries,
	}
}

func (c *ttlCache[V]) get(key string) (V, bool) {
	now := time.Now()
	c.mu.RLock()
	e, ok := c.entries[key]
	if !ok {
		c.mu.RUnlock()
		var zero V
		return zero, false
	}
	if now.Before(e.expiresAt) {
		c.mu.RUnlock()
		return e.value, true
	}
	c.mu.RUnlock()

	// Expiry cleanup needs the write lock. Re-read after acquiring it so a
	// concurrent refresh cannot be deleted based on the stale entry above.
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok = c.entries[key]
	if ok && time.Now().Before(e.expiresAt) {
		return e.value, true
	}
	if ok {
		delete(c.entries, key)
	}
	var zero V
	return zero, false
}

func (c *ttlCache[V]) set(key string, v V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for existingKey, entry := range c.entries {
		if now.After(entry.expiresAt) {
			delete(c.entries, existingKey)
		}
	}
	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.max {
		var oldestKey string
		var oldestExpiry time.Time
		for existingKey, entry := range c.entries {
			if oldestKey == "" || entry.expiresAt.Before(oldestExpiry) {
				oldestKey = existingKey
				oldestExpiry = entry.expiresAt
			}
		}
		delete(c.entries, oldestKey)
	}
	c.entries[key] = ttlEntry[V]{value: v, expiresAt: now.Add(c.ttl)}
}

// doSharedCall coalesces identical work while allowing each waiter to honor its
// own cancellation. The first caller's values are retained for tracing and
// logging, but its cancellation and deadline are detached; callers must bound
// fn itself (architecture collection does so with graphTimeout).
func doSharedCall(ctx context.Context, group *singleflight.Group, key string, fn func(context.Context) (any, error)) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resultCh := group.DoChan(key, func() (any, error) {
		return fn(context.WithoutCancel(ctx))
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultCh:
		return result.Val, result.Err
	}
}
