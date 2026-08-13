package gcp

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sync/singleflight"
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

func TestTTLCacheConcurrentReadersAndWriters(t *testing.T) {
	cache := newTTLCache[int](time.Minute)
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				key := string(rune('a' + (i % 8)))
				cache.set(key, worker*1000+i)
				_, _ = cache.get(key)
			}
		}(worker)
	}
	wg.Wait()
}

func TestDoSharedCallLeaderCancellationDoesNotPoisonFollower(t *testing.T) {
	var group singleflight.Group
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	work := func(sharedCtx context.Context) (any, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		if sharedCtx.Done() != nil {
			return nil, errors.New("shared context unexpectedly cancellable")
		}
		<-release
		return "complete", nil
	}

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderResult := make(chan error, 1)
	go func() {
		_, err := doSharedCall(leaderCtx, &group, "same-key", work)
		leaderResult <- err
	}()
	<-started

	// Register the follower synchronously before cancelling the leader. The
	// callback must be coalesced with the already-running detached work.
	followerResult := group.DoChan("same-key", func() (any, error) {
		return work(context.Background())
	})

	cancelLeader()
	if err := <-leaderResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader error = %v, want context.Canceled", err)
	}
	close(release)
	follower := <-followerResult
	if follower.Err != nil || follower.Val != "complete" {
		t.Fatalf("follower result = (%v, %v), want (complete, nil)", follower.Val, follower.Err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("shared work calls = %d, want 1", got)
	}
}
