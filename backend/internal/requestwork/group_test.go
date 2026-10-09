package requestwork

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

// TestReadersCancelIndependently ensures one abandoned request does not cancel
// work another reader needs, but the final reader's departure does.
func TestReadersCancelIndependently(t *testing.T) {
	var group Group[int]
	first, cancelFirst := context.WithCancel(t.Context())
	second, cancelSecond := context.WithCancel(t.Context())
	started, cancelled := make(chan struct{}), make(chan struct{})
	work := func(ctx context.Context) (int, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return 0, ctx.Err()
	}
	results := make(chan error, 2)
	go func() { _, err := group.Do(first, "same", work); results <- err }()
	<-started
	go func() { _, err := group.Do(second, "same", work); results <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		group.mu.Lock()
		readers := group.calls["same"].readers
		group.mu.Unlock()
		if readers == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second reader did not join")
		}
		runtime.Gosched()
	}
	cancelFirst()
	if err := <-results; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
		t.Fatal("first reader cancelled shared work")
	default:
	}
	cancelSecond()
	if err := <-results; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("last reader did not cancel shared work")
	}
}
