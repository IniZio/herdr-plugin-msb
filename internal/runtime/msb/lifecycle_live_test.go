package msb

import (
	"errors"
	"testing"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

func findRef(refs []coreruntime.SandboxRef, project, name string) *coreruntime.SandboxRef {
	for i := range refs {
		if refs[i].Project == project && refs[i].Name == name {
			return &refs[i]
		}
	}
	return nil
}

func TestLiveLifecycleAll(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()

	ref, err := r.CreateAndBoot(ctx, LiveSpec("lifecycle"))
	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}
	CleanupSandbox(t, r, ref)

	if ref.Status != coreruntime.SandboxStatusRunning {
		t.Fatalf("status after CreateAndBoot = %q, want running", ref.Status)
	}

	refs, err := r.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := findRef(refs, "s01", "lifecycle")
	if found == nil {
		t.Fatalf("List: s01/lifecycle not found among %d refs", len(refs))
	}
	if found.Status != coreruntime.SandboxStatusRunning {
		t.Fatalf("List status = %q, want running", found.Status)
	}

	stopped, err := r.Stop(ctx, ref)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if stopped.Status != coreruntime.SandboxStatusStopped {
		t.Fatalf("status after Stop = %q, want stopped", stopped.Status)
	}

	refs, err = r.List(ctx)
	if err != nil {
		t.Fatalf("List after Stop: %v", err)
	}
	found = findRef(refs, "s01", "lifecycle")
	if found == nil {
		t.Fatalf("List after Stop: s01/lifecycle not found")
	}
	if found.Status != coreruntime.SandboxStatusStopped {
		t.Fatalf("List status after Stop = %q, want stopped", found.Status)
	}

	started, err := r.Start(ctx, stopped)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if started.Status != coreruntime.SandboxStatusRunning {
		t.Fatalf("status after Start = %q, want running", started.Status)
	}

	sb, err := r.connect(ctx, started)
	if err != nil {
		t.Fatalf("connect after Start: %v", err)
	}
	out, err := sb.Exec(ctx, "sh", []string{"-c", "echo lifecycle-ok"})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := out.Stdout(); got != "lifecycle-ok\n" {
		t.Fatalf("stdout = %q, want %q", got, "lifecycle-ok\n")
	}
	if err := sb.Detach(ctx); err != nil {
		t.Fatalf("Detach: %v", err)
	}

	if _, err := r.Stop(ctx, started); err != nil {
		t.Fatalf("final Stop: %v", err)
	}
}

func TestLiveLifecyclePauseResume(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()

	ref, err := r.CreateAndBoot(ctx, LiveSpec("lcpause"))
	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}
	CleanupSandbox(t, r, ref)

	_, err = r.Pause(ctx, ref)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Pause: want ErrUnsupported, got %v", err)
	}

	_, err = r.Resume(ctx, ref)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Resume: want ErrUnsupported, got %v", err)
	}
}
