// Package safego contains panics raised in background goroutines.
//
// Recover in an MCP handler only protects the handler's own goroutine. A
// panic in a fan-out goroutine (an errgroup collector, a per-cluster probe)
// would otherwise terminate the whole server process, so every goroutine the
// server starts goes through this package.
package safego

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"

	"golang.org/x/sync/errgroup"
)

// ErrPanic is returned in place of a recovered panic. The panic value and
// stack are written to the server log only: they can contain project IDs or
// resource names that the response privacy filters would otherwise remove.
var ErrPanic = errors.New("internal error: a background task panicked; details are in the server log")

// Recover converts a panic in the current goroutine into ErrPanic and passes
// it to onPanic. It must be deferred directly, as the first defer that should
// still see the panic:
//
//	go func() {
//		defer safego.Recover(func(err error) { results <- result{err: err} })
//		...
//	}()
//
// onPanic runs while the goroutine unwinds, so it must not block: send only
// to buffered channels that have room for the goroutine's result.
func Recover(onPanic func(error)) {
	if r := recover(); r != nil {
		err := logPanic(r)
		if onPanic != nil {
			onPanic(err)
		}
	}
}

func logPanic(r any) error {
	slog.Default().Error("recovered panic in background goroutine",
		"panic", fmt.Sprint(r),
		"stack", string(debug.Stack()),
	)
	return ErrPanic
}

// Group is an errgroup.Group whose goroutines turn a panic into ErrPanic, so
// a panicking collector fails its request instead of the process.
type Group struct {
	g *errgroup.Group
}

// WithContext mirrors errgroup.WithContext.
func WithContext(ctx context.Context) (*Group, context.Context) {
	g, groupCtx := errgroup.WithContext(ctx)
	return &Group{g: g}, groupCtx
}

// SetLimit mirrors errgroup.Group.SetLimit.
func (g *Group) SetLimit(n int) {
	g.g.SetLimit(n)
}

// Go mirrors errgroup.Group.Go. A panic in fn is returned as ErrPanic, which
// cancels the group's context like any other error.
func (g *Group) Go(fn func() error) {
	g.g.Go(func() (err error) {
		defer Recover(func(recovered error) { err = recovered })
		return fn()
	})
}

// Wait mirrors errgroup.Group.Wait.
func (g *Group) Wait() error {
	return g.g.Wait()
}
