package gcp

import (
	"context"
	"errors"
	"time"
)

// withRetry executes op up to maxAttempts times, retrying only on RetriableError
// with exponential back-off starting at 250 ms (250 ms → 500 ms → 1 s → …).
//
// Non-retriable errors and context cancellation return immediately without retry.
// This is intentionally placed at the adapter boundary, below the rate-limiter,
// so retries do not bypass per-call timeouts that callers have already set.
func withRetry(ctx context.Context, maxAttempts int, op func() error) error {
	var lastErr error
	for attempt := range maxAttempts {
		lastErr = op()
		if lastErr == nil {
			return nil
		}
		var r *RetriableError
		if !errors.As(lastErr, &r) {
			return lastErr
		}
		if attempt == maxAttempts-1 {
			break
		}
		delay := time.Duration(1<<uint(attempt)) * 250 * time.Millisecond
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return lastErr
}
