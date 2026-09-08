package msb

import (
	"context"
	"testing"
	"time"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

type fakeResizer struct {
	got chan coreruntime.WinSize
}

func (f *fakeResizer) Resize(_ context.Context, rows, cols uint16) error {
	f.got <- coreruntime.WinSize{Rows: rows, Cols: cols}
	return nil
}

func TestPumpResizeForwardsEveryChange(t *testing.T) {
	f := &fakeResizer{got: make(chan coreruntime.WinSize, 8)}
	in := make(chan coreruntime.WinSize, 8)
	done := make(chan struct{})
	go pumpResize(context.Background(), f, in, done)
	defer close(done)

	for _, want := range []coreruntime.WinSize{{Rows: 62, Cols: 242}, {Rows: 30, Cols: 100}} {
		in <- want
		select {
		case got := <-f.got:
			if got != want {
				t.Fatalf("forwarded %dx%d, want %dx%d", got.Rows, got.Cols, want.Rows, want.Cols)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("resize %dx%d never reached the guest PTY", want.Rows, want.Cols)
		}
	}
}

func TestPumpResizeStopsWhenExecFinishes(t *testing.T) {
	f := &fakeResizer{got: make(chan coreruntime.WinSize, 8)}
	in := make(chan coreruntime.WinSize)
	done := make(chan struct{})
	exited := make(chan struct{})
	go func() { pumpResize(context.Background(), f, in, done); close(exited) }()

	close(done)
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("pumpResize outlived the exec")
	}
}

func TestPumpResizeIgnoresZeroSize(t *testing.T) {
	f := &fakeResizer{got: make(chan coreruntime.WinSize, 8)}
	in := make(chan coreruntime.WinSize, 8)
	done := make(chan struct{})
	go pumpResize(context.Background(), f, in, done)
	defer close(done)

	in <- coreruntime.WinSize{}
	in <- coreruntime.WinSize{Rows: 62, Cols: 242}
	select {
	case got := <-f.got:
		if got.Rows == 0 {
			t.Fatal("pumpResize forwarded a 0x0 size to the guest PTY")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no resize forwarded")
	}
}
