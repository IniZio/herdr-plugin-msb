package herdrspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
)

func TestPutAndGet(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	b := Binding{SpaceLabel: "msb:w1", HerdrWorkspaceID: "wA", SandboxHandle: "demo/w1"}

	if err := Put(ctx, dir, b); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := GetByLabel(ctx, dir, "msb:w1")
	if err != nil || got != b {
		t.Fatalf("GetByLabel: got %+v err %v, want %+v", got, err, b)
	}
	got, err = GetByWorkspaceID(ctx, dir, "wA")
	if err != nil || got != b {
		t.Fatalf("GetByWorkspaceID: got %+v err %v", got, err)
	}
	got, err = GetByHandle(ctx, dir, "demo/w1")
	if err != nil || got != b {
		t.Fatalf("GetByHandle: got %+v err %v", got, err)
	}
}

func TestGetNotFound(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	_, err := GetByLabel(ctx, dir, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestPut_InvariantOnSpaceLabel(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	b1 := Binding{SpaceLabel: "msb:w1", HerdrWorkspaceID: "wA", SandboxHandle: "demo/h1"}
	b2 := Binding{SpaceLabel: "msb:w1", HerdrWorkspaceID: "wB", SandboxHandle: "demo/h2"}

	if err := Put(ctx, dir, b1); err != nil {
		t.Fatalf("Put b1: %v", err)
	}
	if err := Put(ctx, dir, b2); err != nil {
		t.Fatalf("Put b2: %v", err)
	}

	all, err := List(ctx, dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("1:1 invariant violated on SpaceLabel: got %d entries, want 1; entries: %+v", len(all), all)
	}
	if all[0] != b2 {
		t.Fatalf("expected b2 to survive; got %+v", all[0])
	}
}

func TestPut_InvariantOnSandboxHandle(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	b1 := Binding{SpaceLabel: "msb:w1", HerdrWorkspaceID: "wA", SandboxHandle: "demo/h1"}
	b2 := Binding{SpaceLabel: "msb:w2", HerdrWorkspaceID: "wB", SandboxHandle: "demo/h1"}

	if err := Put(ctx, dir, b1); err != nil {
		t.Fatalf("Put b1: %v", err)
	}
	if err := Put(ctx, dir, b2); err != nil {
		t.Fatalf("Put b2: %v", err)
	}

	all, err := List(ctx, dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("1:1 invariant violated on SandboxHandle: got %d entries, want 1; entries: %+v", len(all), all)
	}
	if all[0] != b2 {
		t.Fatalf("expected b2 to survive; got %+v", all[0])
	}
}

func TestWriteMode0600(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	if err := Put(ctx, dir, Binding{SpaceLabel: "msb:w1", HerdrWorkspaceID: "wA", SandboxHandle: "demo/h1"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	info, err := os.Stat(bindingsPath(dir))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Fatalf("bindings file mode %04o, want 0600", mode)
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	b := Binding{SpaceLabel: "msb:w1", HerdrWorkspaceID: "wA", SandboxHandle: "demo/h1"}
	if err := Put(ctx, dir, b); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := Delete(ctx, dir, "msb:w1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	all, err := List(ctx, dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("expected empty after delete, got %+v", all)
	}
}

func TestConcurrentPut(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			b := Binding{
				SpaceLabel:       fmt.Sprintf("msb:w%d", i),
				HerdrWorkspaceID: fmt.Sprintf("w%d", i),
				SandboxHandle:    fmt.Sprintf("demo/h%d", i),
			}
			if err := Put(ctx, dir, b); err != nil {
				t.Errorf("Put %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	all, err := List(ctx, dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != n {
		t.Fatalf("after %d concurrent puts, got %d bindings, want %d", n, len(all), n)
	}

	seen := map[string]bool{}
	for _, b := range all {
		if seen[b.SpaceLabel] {
			t.Fatalf("duplicate SpaceLabel %q in %+v", b.SpaceLabel, all)
		}
		seen[b.SpaceLabel] = true
	}
}
