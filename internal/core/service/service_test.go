package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/IniZio/herdr-plugin-msb/internal/core/credmount"
	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

type fakeRuntime struct {
	refs               []coreruntime.SandboxRef
	createCount        int
	createAndBootCount int
	lastExecRef        coreruntime.SandboxRef
	lastExecReq        coreruntime.ExecRequest
}

func (f *fakeRuntime) Create(_ context.Context, spec coreruntime.SandboxSpec) (coreruntime.SandboxRef, error) {
	f.createCount++
	return coreruntime.SandboxRef{Name: spec.Name, Project: spec.Project}, nil
}

func (f *fakeRuntime) CreateAndBoot(_ context.Context, spec coreruntime.SandboxSpec) (coreruntime.SandboxRef, error) {
	f.createAndBootCount++
	return coreruntime.SandboxRef{Name: spec.Name, Project: spec.Project}, nil
}

func (f *fakeRuntime) List(_ context.Context) ([]coreruntime.SandboxRef, error) {
	return f.refs, nil
}

func (f *fakeRuntime) Start(_ context.Context, ref coreruntime.SandboxRef) (coreruntime.SandboxRef, error) {
	return ref, nil
}

func (f *fakeRuntime) Stop(_ context.Context, ref coreruntime.SandboxRef) (coreruntime.SandboxRef, error) {
	return ref, nil
}

func (f *fakeRuntime) Pause(_ context.Context, ref coreruntime.SandboxRef) (coreruntime.SandboxRef, error) {
	return ref, nil
}

func (f *fakeRuntime) Resume(_ context.Context, ref coreruntime.SandboxRef) (coreruntime.SandboxRef, error) {
	return ref, nil
}

func (f *fakeRuntime) Remove(_ context.Context, _ coreruntime.SandboxRef) error { return nil }

func (f *fakeRuntime) Exec(_ context.Context, ref coreruntime.SandboxRef, req coreruntime.ExecRequest) (coreruntime.ExecResult, error) {
	f.lastExecRef = ref
	f.lastExecReq = req
	return coreruntime.ExecResult{}, nil
}

func (f *fakeRuntime) RunEphemeral(_ context.Context, _ coreruntime.SandboxSpec, _ coreruntime.ExecRequest) (coreruntime.ExecResult, error) {
	return coreruntime.ExecResult{}, nil
}

func TestSpec_LeavesNetRulesNilForSingleApplicationPoint(t *testing.T) {
	// Single application point: runtime.go:105.
	svc := New(&fakeRuntime{}, "")
	spec, err := svc.Spec(CreateOptions{Name: "x", ImageRef: "img"})
	if err != nil {
		t.Fatal(err)
	}
	if spec.NetRules != nil {
		t.Fatalf("NetRules must be nil, got %v", spec.NetRules)
	}
}

func TestSpec_WorktreeMount(t *testing.T) {
	dir := t.TempDir()
	svc := New(&fakeRuntime{}, "")

	spec, err := svc.Spec(CreateOptions{Name: "x", ImageRef: "img", Worktree: dir})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, m := range spec.Mounts {
		if m.GuestPath == DefaultGuestWorktree {
			found = true
			if m.ReadOnly {
				t.Error("worktree mount must be read-write")
			}
		}
	}
	if !found {
		t.Errorf("no mount at %s with Worktree set", DefaultGuestWorktree)
	}

	spec2, err := svc.Spec(CreateOptions{Name: "x", ImageRef: "img"})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range spec2.Mounts {
		if m.GuestPath == DefaultGuestWorktree {
			t.Errorf("unexpected mount at %s when Worktree empty", DefaultGuestWorktree)
		}
	}
}

func TestSpec_CredentialMount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	credDir := filepath.Join(home, ".config", "nexus3", "claude-dedicated")
	if err := os.MkdirAll(credDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(credDir, ".credentials.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	svc := New(&fakeRuntime{}, "")
	expected := credmount.DefaultStoreDir()

	spec, err := svc.Spec(CreateOptions{Name: "x", ImageRef: "img", Credential: true})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, m := range spec.Mounts {
		if m.GuestPath == DefaultGuestCredDir {
			found = true
			if m.ReadOnly {
				t.Error("credential mount must be read-write")
			}
			if m.HostPath != expected {
				t.Errorf("HostPath = %q, want %q", m.HostPath, expected)
			}
		}
	}
	if !found {
		t.Errorf("no credential mount at %s", DefaultGuestCredDir)
	}

	spec2, err := svc.Spec(CreateOptions{Name: "x", ImageRef: "img", Credential: false})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range spec2.Mounts {
		if m.GuestPath == DefaultGuestCredDir {
			t.Errorf("unexpected credential mount when Credential=false")
		}
	}
}

func TestSpec_Memory(t *testing.T) {
	svc := New(&fakeRuntime{}, "")

	spec, err := svc.Spec(CreateOptions{Name: "x", ImageRef: "img", MemoryMiB: 0})
	if err != nil {
		t.Fatal(err)
	}
	if spec.MemoryMiB != DefaultMemoryMiB {
		t.Errorf("MemoryMiB=0 => %d, want %d", spec.MemoryMiB, DefaultMemoryMiB)
	}

	spec2, err := svc.Spec(CreateOptions{Name: "x", ImageRef: "img", MemoryMiB: 2048})
	if err != nil {
		t.Fatal(err)
	}
	if spec2.MemoryMiB != 2048 {
		t.Errorf("MemoryMiB=2048 => %d", spec2.MemoryMiB)
	}

	_, err = svc.Spec(CreateOptions{Name: "x", ImageRef: "img", MemoryMiB: 2049})
	if !errors.Is(err, ErrMemoryTooLarge) {
		t.Errorf("MemoryMiB=2049: want ErrMemoryTooLarge, got %v", err)
	}
}

func TestSpec_ValidationErrors(t *testing.T) {
	svc := New(&fakeRuntime{}, "")

	_, err := svc.Spec(CreateOptions{ImageRef: "img"})
	if !errors.Is(err, ErrNameRequired) {
		t.Errorf("empty Name: want ErrNameRequired, got %v", err)
	}

	_, err = svc.Spec(CreateOptions{Name: "x"})
	if !errors.Is(err, ErrImageRequired) {
		t.Errorf("empty ImageRef: want ErrImageRequired, got %v", err)
	}
}

func TestCreate_BootFlag(t *testing.T) {
	ctx := context.Background()

	rt1 := &fakeRuntime{}
	svc1 := New(rt1, "")
	if _, err := svc1.Create(ctx, CreateOptions{Name: "x", ImageRef: "img", Boot: true}); err != nil {
		t.Fatal(err)
	}
	if rt1.createAndBootCount != 1 || rt1.createCount != 0 {
		t.Errorf("Boot=true: createAndBoot=%d create=%d, want 1/0", rt1.createAndBootCount, rt1.createCount)
	}

	rt2 := &fakeRuntime{}
	svc2 := New(rt2, "")
	if _, err := svc2.Create(ctx, CreateOptions{Name: "x", ImageRef: "img", Boot: false}); err != nil {
		t.Fatal(err)
	}
	if rt2.createCount != 1 || rt2.createAndBootCount != 0 {
		t.Errorf("Boot=false: create=%d createAndBoot=%d, want 1/0", rt2.createCount, rt2.createAndBootCount)
	}
}

func TestResolve(t *testing.T) {
	ctx := context.Background()
	rt := &fakeRuntime{
		refs: []coreruntime.SandboxRef{
			{ID: "id1", Project: "herdr", Name: "alpha"},
		},
	}
	svc := New(rt, "")

	ref, err := svc.Resolve(ctx, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Name != "alpha" {
		t.Errorf("Resolve returned %q, want alpha", ref.Name)
	}

	_, err = svc.Resolve(ctx, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("missing name: want ErrNotFound, got %v", err)
	}
}

func TestExec_DelegatesWithResolvedRef(t *testing.T) {
	ctx := context.Background()
	ref := coreruntime.SandboxRef{ID: "id1", Project: "herdr", Name: "alpha"}
	rt := &fakeRuntime{refs: []coreruntime.SandboxRef{ref}}
	svc := New(rt, "")

	req := coreruntime.ExecRequest{Argv: []string{"echo", "hi"}, Cwd: "/"}
	if _, err := svc.Exec(ctx, "alpha", req); err != nil {
		t.Fatal(err)
	}
	if rt.lastExecRef != ref {
		t.Errorf("Exec ref = %v, want %v", rt.lastExecRef, ref)
	}
	if len(rt.lastExecReq.Argv) != 2 || rt.lastExecReq.Argv[0] != "echo" {
		t.Errorf("Exec req argv = %v, want [echo hi]", rt.lastExecReq.Argv)
	}
}
