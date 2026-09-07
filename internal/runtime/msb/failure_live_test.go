package msb

import (
	"errors"
	"strings"
	"testing"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
	msbsdk "github.com/superradcompany/microsandbox/sdk/go"
)

func TestLiveFailureCreateEmptyName(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()

	_, err := r.Create(ctx, coreruntime.SandboxSpec{})
	if err == nil {
		t.Fatal("Create empty name: want error, got nil")
	}
	if !strings.Contains(err.Error(), "no name") {
		t.Fatalf("Create empty name: error %q does not contain \"no name\"", err)
	}
}

func TestLiveFailureCreateAndBootEmptyName(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()

	_, err := r.CreateAndBoot(ctx, coreruntime.SandboxSpec{})
	if err == nil {
		t.Fatal("CreateAndBoot empty name: want error, got nil")
	}
	if !strings.Contains(err.Error(), "no name") {
		t.Fatalf("CreateAndBoot empty name: error %q does not contain \"no name\"", err)
	}
}

func TestLiveFailureCreateAndBootBadImage(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()

	spec := coreruntime.SandboxSpec{
		Project:   "s01",
		Name:      "failure-badimg",
		ImageRef:  "definitely-not-a-real-image:nope",
		VCPUs:     1,
		MemoryMiB: 512,
	}
	_, err := r.CreateAndBoot(ctx, spec)
	if err == nil {
		t.Fatal("CreateAndBoot bad image: want error, got nil")
	}
	t.Logf("CreateAndBoot bad image error: %v", err)

	sdkName := SDKName(spec.Project, spec.Name)
	if _, getErr := msbsdk.GetSandbox(ctx, sdkName); getErr == nil {
		t.Fatalf("CreateAndBoot bad image: sandbox %q still exists after error", sdkName)
	}
}

func TestLiveFailureStartNonexistent(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()

	ghost := coreruntime.SandboxRef{Project: "s01", Name: "does-not-exist"}
	_, err := r.Start(ctx, ghost)
	if err == nil {
		t.Fatal("Start nonexistent: want error, got nil")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("Start nonexistent: error %q does not mention sandbox name", err)
	}
	t.Logf("Start nonexistent: %v", err)
}

func TestLiveFailureStopNonexistent(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()

	ghost := coreruntime.SandboxRef{Project: "s01", Name: "does-not-exist"}
	_, err := r.Stop(ctx, ghost)
	if err == nil {
		t.Fatal("Stop nonexistent: want error, got nil")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("Stop nonexistent: error %q does not mention sandbox name", err)
	}
	t.Logf("Stop nonexistent: %v", err)
}

func TestLiveFailureRemoveNonexistent(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()

	ghost := coreruntime.SandboxRef{Project: "s01", Name: "does-not-exist"}
	err := r.Remove(ctx, ghost)
	if err == nil {
		t.Fatal("Remove nonexistent: want error, got nil")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("Remove nonexistent: error %q does not mention sandbox name", err)
	}
	t.Logf("Remove nonexistent: %v", err)
}

func TestLiveFailureExecNonexistent(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()

	ghost := coreruntime.SandboxRef{Project: "s01", Name: "does-not-exist"}
	_, err := r.Exec(ctx, ghost, coreruntime.ExecRequest{Argv: []string{"echo", "hi"}})
	if err == nil {
		t.Fatal("Exec nonexistent: want error, got nil")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("Exec nonexistent: error %q does not mention sandbox name", err)
	}
	t.Logf("Exec nonexistent: %v", err)
}

func TestLiveFailureExecEmptyArgv(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()

	_, err := r.Exec(ctx, coreruntime.SandboxRef{}, coreruntime.ExecRequest{})
	if err == nil {
		t.Fatal("Exec empty argv: want error, got nil")
	}
	if !strings.Contains(err.Error(), "argv must not be empty") {
		t.Fatalf("Exec empty argv: error %q does not contain \"argv must not be empty\"", err)
	}
}

func TestLiveFailurePause(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()

	_, err := r.Pause(ctx, coreruntime.SandboxRef{})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Pause: want errors.Is(err, ErrUnsupported), got %v", err)
	}
}

func TestLiveFailureResume(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()

	_, err := r.Resume(ctx, coreruntime.SandboxRef{})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Resume: want errors.Is(err, ErrUnsupported), got %v", err)
	}
}

func TestLiveFailureRunEphemeralBadImage(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()

	spec := coreruntime.SandboxSpec{
		Project:   "s01",
		Name:      "failure-eph",
		ImageRef:  "definitely-not-a-real-image:nope",
		VCPUs:     1,
		MemoryMiB: 512,
	}
	_, err := r.RunEphemeral(ctx, spec, coreruntime.ExecRequest{Argv: []string{"echo", "hi"}})
	if err == nil {
		t.Fatal("RunEphemeral bad image: want error, got nil")
	}
	t.Logf("RunEphemeral bad image error: %v", err)

	sdkName := SDKName(spec.Project, spec.Name)
	if _, getErr := msbsdk.GetSandbox(ctx, sdkName); getErr == nil {
		t.Fatalf("RunEphemeral bad image: sandbox %q still listed after error", sdkName)
	}
}

func TestLiveFailureListUnreachable(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()
	t.Log("List failure: no reachable failure path against a real host; the daemon always returns a valid page (possibly empty); forcing a transport error requires mock injection, which is excluded by the VM-budget and no-mock constraints")
	refs, err := r.List(ctx)
	if err != nil {
		t.Fatalf("List: unexpected error: %v", err)
	}
	t.Logf("List returned %d sandboxes", len(refs))
}

func TestLiveFailureWithVM(t *testing.T) {
	RequireLive(t)
	r := New()
	ctx := LiveContext(t)

	ref, err := r.CreateAndBoot(ctx, LiveSpec("failure"))
	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}
	CleanupSandbox(t, r, ref)

	rmErr := r.Remove(ctx, ref)
	if rmErr == nil {
		t.Log("Remove of running sandbox: succeeded — sandbox is gone")
		if _, getErr := msbsdk.GetSandbox(ctx, SDKName(ref.Project, ref.Name)); getErr == nil {
			t.Fatal("Remove running: succeeded but sandbox still listed")
		}
	} else {
		t.Logf("Remove of running sandbox: error (expected) — %v", rmErr)
	}

	if h, herr := r.handle(ctx, ref); herr == nil {
		_ = h.Kill(ctx)
		_ = h.Remove(ctx)
	}

	ref2, err := r.CreateAndBoot(ctx, LiveSpec("failure"))
	if err != nil {
		t.Fatalf("CreateAndBoot for exec-on-stopped: %v", err)
	}
	CleanupSandbox(t, r, ref2)

	if _, stopErr := r.Stop(ctx, ref2); stopErr != nil {
		t.Fatalf("Stop before exec-on-stopped: %v", stopErr)
	}

	_, execErr := r.Exec(ctx, ref2, coreruntime.ExecRequest{Argv: []string{"echo", "x"}})
	if execErr == nil {
		t.Fatal("Exec on stopped sandbox: want error, got nil")
	}
	t.Logf("Exec on stopped sandbox: %v", execErr)
}
