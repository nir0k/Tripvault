// Package requestwork coalesces concurrent provider requests without tying
// shared work to the lifetime of the first reader.
package requestwork

import (
	"context"
	"sync"
)

// Group shares one operation per key until it completes or all readers leave.
type Group[T any] struct {
	mu    sync.Mutex
	calls map[string]*call[T]
}

// call keeps a result and the readers still waiting for it.
type call[T any] struct {
	done    chan struct{}
	cancel  context.CancelFunc
	readers int
	value   T
	err     error
}

// Do - joins or starts work for a key, cancelling it when no readers remain.
//
// Arguments:
//   - ctx: the individual reader's lifetime.
//   - key: the complete identity of the operation.
//   - work: the operation, bounded by a context shared by its readers.
//
// Returns:
//   - the shared result.
//   - the operation's error or the reader's cancellation.
func (g *Group[T]) Do(ctx context.Context, key string, work func(context.Context) (T, error)) (T, error) {
	if err := ctx.Err(); err != nil {
		var zero T
		return zero, err
	}
	g.mu.Lock()
	if g.calls == nil {
		g.calls = make(map[string]*call[T])
	}
	c := g.calls[key]
	if c == nil {
		shared, cancel := context.WithCancel(context.WithoutCancel(ctx))
		c = &call[T]{done: make(chan struct{}), cancel: cancel}
		g.calls[key] = c
		go func() {
			defer cancel()
			c.value, c.err = work(shared)
			g.mu.Lock()
			if g.calls[key] == c {
				delete(g.calls, key)
			}
			close(c.done)
			g.mu.Unlock()
		}()
	}
	c.readers++
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		c.readers--
		if c.readers == 0 {
			if g.calls[key] == c {
				delete(g.calls, key)
			}
			c.cancel()
		}
		g.mu.Unlock()
	}()
	select {
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	case <-c.done:
		return c.value, c.err
	}
}
