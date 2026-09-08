package msb

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type wedgedDetacher struct {
	release  chan struct{}
	honorCtx bool
	mu       sync.Mutex
	calls    int
}

func newWedgedDetacher(honorCtx bool) *wedgedDetacher {
	return &wedgedDetacher{release: make(chan struct{}), honorCtx: honorCtx}
}

func (w *wedgedDetacher) Detach(ctx context.Context) error {
	w.mu.Lock()
	w.calls++
	w.mu.Unlock()
	if w.honorCtx {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.release:
			return nil
		}
	}
	<-w.release
	return nil
}

func (w *wedgedDetacher) stop() { close(w.release) }

func withShortDetachTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := detachTimeout
	detachTimeout = d
	t.Cleanup(func() { detachTimeout = prev })
}

func TestDetachWithTimeoutCtxHonouringDaemon(t *testing.T) {
	withShortDetachTimeout(t, 120*time.Millisecond)
	w := newWedgedDetacher(true)
	t.Cleanup(w.stop)

	start := time.Now()
	err := detachWithTimeout(context.Background(), w)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
	if elapsed < 100*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("elapsed %v outside expected band for 120ms timeout", elapsed)
	}
	t.Logf("ctx-honouring wedge: err=%v elapsed=%v", err, elapsed)
}

func TestDetachWithTimeoutCtxIgnoringDaemonDoesNotBlockTeardown(t *testing.T) {
	withShortDetachTimeout(t, 120*time.Millisecond)
	w := newWedgedDetacher(false)
	t.Cleanup(w.stop)

	type outcome struct {
		err     error
		elapsed time.Duration
	}
	done := make(chan outcome, 1)
	start := time.Now()
	go func() {
		err := detachWithTimeout(context.Background(), w)
		done <- outcome{err, time.Since(start)}
	}()

	select {
	case o := <-done:
		if !errors.Is(o.err, context.DeadlineExceeded) {
			t.Fatalf("want DeadlineExceeded, got %v", o.err)
		}
		if o.elapsed > 2*time.Second {
			t.Fatalf("returned but took %v", o.elapsed)
		}
		t.Logf("ctx-ignoring wedge: err=%v elapsed=%v", o.err, o.elapsed)
	case <-time.After(3 * time.Second):
		t.Fatalf("detachWithTimeout never returned: a daemon that ignores cancellation blocks teardown forever (timeout was %v, waited 3s)", detachTimeout)
	}
}

func TestDetachWithTimeoutLeavesOneGoroutinePerWedge(t *testing.T) {
	withShortDetachTimeout(t, 80*time.Millisecond)
	w := newWedgedDetacher(false)

	before := runtime.NumGoroutine()
	res := make(chan error, 1)
	go func() { res <- detachWithTimeout(context.Background(), w) }()
	select {
	case err := <-res:
		if !errors.Is(err, context.DeadlineExceeded) {
			w.stop()
			t.Fatalf("want DeadlineExceeded, got %v", err)
		}
	case <-time.After(3 * time.Second):
		w.stop()
		t.Fatalf("detachWithTimeout never returned with an %v budget", detachTimeout)
	}
	after := runtime.NumGoroutine()
	t.Logf("goroutines before=%d after=%d delta=%d", before, after, after-before)

	w.stop()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before {
			t.Logf("abandoned goroutine exited after wedge released")
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("goroutine count stayed above %d after releasing wedge (now %d)", before, runtime.NumGoroutine())
}

func TestDetachWithTimeoutSurfacesDaemonError(t *testing.T) {
	withShortDetachTimeout(t, time.Second)
	sentinel := errors.New("daemon refused detach")
	err := detachWithTimeout(context.Background(), detachFunc(func(context.Context) error { return sentinel }))
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel, got %v", err)
	}
	if strings.Contains(err.Error(), "context") {
		t.Fatalf("unexpected context wrapping: %v", err)
	}
}

type detachFunc func(context.Context) error

func (f detachFunc) Detach(ctx context.Context) error { return f(ctx) }
