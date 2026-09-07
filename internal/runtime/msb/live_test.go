package msb

import (
	"context"
	"os"
	"testing"
	"time"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

const LiveEnvVar = "HERDR_MSB_LIVE"

func RequireLive(t *testing.T) {
	t.Helper()
	if os.Getenv(LiveEnvVar) != "1" {
		t.Skipf("live test: set %s=1 to boot a real microsandbox VM", LiveEnvVar)
	}
}

func LiveContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

func LiveSpec(name string) coreruntime.SandboxSpec {
	return coreruntime.SandboxSpec{
		Project:   "s01",
		Name:      name,
		ImageRef:  "alpine",
		VCPUs:     1,
		MemoryMiB: 512,
		Motive:    "nexus3-microsandbox-pivot",
	}
}

func CleanupSandbox(t *testing.T, r *Runtime, ref coreruntime.SandboxRef) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if h, err := r.handle(ctx, ref); err == nil {
			_ = h.Kill(ctx)
			if err := h.Remove(ctx); err != nil {
				t.Logf("cleanup remove %s: %v", ref.Name, err)
			}
		}
	})
}

func TestLiveTracerCreateAndBootThenRemove(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()
	spec := LiveSpec("tracer")

	ref, err := r.CreateAndBoot(ctx, spec)
	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}
	CleanupSandbox(t, r, ref)

	if ref.Status != coreruntime.SandboxStatusRunning {
		t.Fatalf("status after CreateAndBoot = %q, want running", ref.Status)
	}
	if ref.Project != "s01" || ref.Name != "tracer" {
		t.Fatalf("ref project/name = %q/%q, want s01/tracer", ref.Project, ref.Name)
	}
	if ref.ID == "" {
		t.Fatal("ref ID is empty")
	}

	sb, err := r.connect(ctx, ref)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	out, err := sb.Exec(ctx, "sh", []string{"-c", "echo tracer-ok"})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := out.Stdout(); got != "tracer-ok\n" {
		t.Fatalf("stdout = %q, want %q", got, "tracer-ok\n")
	}
	if err := sb.Detach(ctx); err != nil {
		t.Fatalf("Detach: %v", err)
	}

	h, err := r.handle(ctx, ref)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if err := h.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := r.Remove(ctx, ref); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := r.handle(ctx, ref); err == nil {
		t.Fatal("handle after Remove: want error, got nil")
	}
}
