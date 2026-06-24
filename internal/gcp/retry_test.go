package gcp

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	calls := 0
	err := withRetry(context.Background(), 3, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if calls != 1 {
		t.Errorf("want 1 call, got %d", calls)
	}
}

func TestWithRetry_RetriesOnRetriableError(t *testing.T) {
	calls := 0
	retriable := &RetriableError{Op: "test", Err: fmt.Errorf("quota")}
	err := withRetry(context.Background(), 3, func() error {
		calls++
		if calls < 3 {
			return retriable
		}
		return nil
	})
	if err != nil {
		t.Fatalf("want nil after retries, got %v", err)
	}
	if calls != 3 {
		t.Errorf("want 3 calls, got %d", calls)
	}
}

func TestWithRetry_NoRetryOnNonRetriable(t *testing.T) {
	calls := 0
	sentinel := errors.New("hard error")
	err := withRetry(context.Background(), 3, func() error {
		calls++
		return fmt.Errorf("wrapped: %w", sentinel)
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("want exactly 1 call for non-retriable error, got %d", calls)
	}
}

func TestWithRetry_ExhaustsMaxAttempts(t *testing.T) {
	calls := 0
	retriable := &RetriableError{Op: "test", Err: fmt.Errorf("always quota")}
	err := withRetry(context.Background(), 3, func() error {
		calls++
		return retriable
	})
	if !errors.As(err, &retriable) {
		t.Fatalf("want RetriableError, got %v", err)
	}
	if calls != 3 {
		t.Errorf("want 3 attempts (maxAttempts=3), got %d", calls)
	}
}

func TestWithRetry_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	calls := 0
	err := withRetry(ctx, 3, func() error {
		calls++
		return &RetriableError{Op: "test", Err: fmt.Errorf("quota")}
	})
	// First attempt always runs; context cancellation fires during back-off.
	if calls != 1 {
		t.Errorf("want 1 call before cancel detected, got %d", calls)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}
