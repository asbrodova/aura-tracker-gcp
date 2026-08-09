package gcp

import (
	"testing"
	"time"
)

func TestTTLCachePurgesExpiredEntries(t *testing.T) {
	cache := newTTLCache[string](time.Minute)
	cache.set("expired", "value")
	cache.mu.Lock()
	entry := cache.entries["expired"]
	entry.expiresAt = time.Now().Add(-time.Second)
	cache.entries["expired"] = entry
	cache.mu.Unlock()

	if _, ok := cache.get("expired"); ok {
		t.Fatal("expired entry was returned")
	}
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	if len(cache.entries) != 0 {
		t.Fatalf("expired entry was not removed: %#v", cache.entries)
	}
}

func TestTTLCacheEvictsOldestAtCapacity(t *testing.T) {
	cache := newTTLCache[string](time.Hour)
	cache.max = 2
	cache.set("oldest", "a")
	cache.set("middle", "b")
	cache.set("newest", "c")

	if _, ok := cache.get("oldest"); ok {
		t.Fatal("oldest entry was not evicted")
	}
	if value, ok := cache.get("newest"); !ok || value != "c" {
		t.Fatalf("new entry missing: value=%q ok=%v", value, ok)
	}
}
