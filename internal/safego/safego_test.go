package safego

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestGroupConvertsPanicToError(t *testing.T) {
	g, ctx := WithContext(context.Background())
	var finished atomic.Bool
	g.Go(func() error {
		panic("boom in projects/private-project-123")
	})
	g.Go(func() error {
		<-ctx.Done() // the panic must cancel siblings like an ordinary error
		finished.Store(true)
		return nil
	})

	err := g.Wait()
	if !errors.Is(err, ErrPanic) {
		t.Fatalf("Wait() = %v, want ErrPanic", err)
	}
	if !finished.Load() {
		t.Fatal("sibling goroutine did not observe the group cancellation")
	}
	if got := err.Error(); got != ErrPanic.Error() {
		t.Fatalf("error leaks panic details: %q", got)
	}
}

func TestGroupPassesThroughErrorsAndLimit(t *testing.T) {
	want := errors.New("collector failed")
	g, _ := WithContext(context.Background())
	g.SetLimit(1)
	g.Go(func() error { return nil })
	g.Go(func() error { return want })
	if err := g.Wait(); !errors.Is(err, want) {
		t.Fatalf("Wait() = %v, want %v", err, want)
	}
}

func TestRecoverReportsPanicToCallback(t *testing.T) {
	results := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer Recover(func(err error) { results <- err })
		panic("goroutine failure")
	}()
	<-done
	select {
	case err := <-results:
		if !errors.Is(err, ErrPanic) {
			t.Fatalf("callback error = %v, want ErrPanic", err)
		}
	default:
		t.Fatal("Recover did not call onPanic")
	}
}

func TestRecoverWithoutPanicDoesNothing(t *testing.T) {
	called := false
	func() {
		defer Recover(func(error) { called = true })
	}()
	if called {
		t.Fatal("onPanic called without a panic")
	}
}
