package msb

import (
	"bytes"
	"strings"
	"testing"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
	msbsdk "github.com/superradcompany/microsandbox/sdk/go"
)

func TestLiveExecStdout(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()
	spec := LiveSpec("exec")
	ref, err := r.CreateAndBoot(ctx, spec)
	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}
	CleanupSandbox(t, r, ref)

	var buf bytes.Buffer
	res, err := r.Exec(ctx, ref, coreruntime.ExecRequest{
		Argv:   []string{"sh", "-c", "echo hello-stdout"},
		Stdout: &buf,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
	if got := buf.String(); got != "hello-stdout\n" {
		t.Fatalf("stdout = %q, want %q", got, "hello-stdout\n")
	}
}

func TestLiveExecStderr(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()
	spec := LiveSpec("exec")
	ref, err := r.CreateAndBoot(ctx, spec)
	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}
	CleanupSandbox(t, r, ref)

	var out, errBuf bytes.Buffer
	res, err := r.Exec(ctx, ref, coreruntime.ExecRequest{
		Argv:   []string{"sh", "-c", "echo to-stderr >&2"},
		Stdout: &out,
		Stderr: &errBuf,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
	if got := out.String(); got != "" {
		t.Fatalf("stdout = %q, want empty", got)
	}
	if got := errBuf.String(); got != "to-stderr\n" {
		t.Fatalf("stderr = %q, want %q", got, "to-stderr\n")
	}
}

func TestLiveExecStdin(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()
	spec := LiveSpec("exec")
	ref, err := r.CreateAndBoot(ctx, spec)
	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}
	CleanupSandbox(t, r, ref)

	var buf bytes.Buffer
	res, err := r.Exec(ctx, ref, coreruntime.ExecRequest{
		Argv:   []string{"sh", "-c", "cat"},
		Stdin:  "ping-stdin",
		Stdout: &buf,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
	if got := buf.String(); got != "ping-stdin" {
		t.Fatalf("stdout = %q, want %q", got, "ping-stdin")
	}
}

func TestLiveExecCwd(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()
	spec := LiveSpec("exec")
	ref, err := r.CreateAndBoot(ctx, spec)
	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}
	CleanupSandbox(t, r, ref)

	_, err = r.Exec(ctx, ref, coreruntime.ExecRequest{
		Argv: []string{"mkdir", "-p", "/tmp/cwdtest"},
	})
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	var buf bytes.Buffer
	res, err := r.Exec(ctx, ref, coreruntime.ExecRequest{
		Argv:   []string{"pwd"},
		Cwd:    "/tmp/cwdtest",
		Stdout: &buf,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
	if got := strings.TrimSpace(buf.String()); got != "/tmp/cwdtest" {
		t.Fatalf("pwd = %q, want /tmp/cwdtest", got)
	}
}

func TestLiveExecEnv(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()
	spec := LiveSpec("exec")
	ref, err := r.CreateAndBoot(ctx, spec)
	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}
	CleanupSandbox(t, r, ref)

	var buf bytes.Buffer
	res, err := r.Exec(ctx, ref, coreruntime.ExecRequest{
		Argv:   []string{"sh", "-c", "echo $MY_VAR"},
		Env:    map[string]string{"MY_VAR": "envval"},
		Stdout: &buf,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
	if got := strings.TrimSpace(buf.String()); got != "envval" {
		t.Fatalf("env output = %q, want envval", got)
	}
}

func TestLiveExecNonZeroExit(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()
	spec := LiveSpec("exec")
	ref, err := r.CreateAndBoot(ctx, spec)
	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}
	CleanupSandbox(t, r, ref)

	res, err := r.Exec(ctx, ref, coreruntime.ExecRequest{
		Argv: []string{"sh", "-c", "exit 7"},
	})
	if err != nil {
		t.Fatalf("Exec returned error for non-zero exit: %v", err)
	}
	if res.ExitCode != 7 {
		t.Fatalf("exit code = %d, want 7", res.ExitCode)
	}
}

func TestLiveExecNilWriters(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()
	spec := LiveSpec("exec")
	ref, err := r.CreateAndBoot(ctx, spec)
	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}
	CleanupSandbox(t, r, ref)

	res, err := r.Exec(ctx, ref, coreruntime.ExecRequest{
		Argv: []string{"sh", "-c", "echo out; echo err >&2"},
	})
	if err != nil {
		t.Fatalf("Exec with nil writers: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
}

func TestLiveRunEphemeral(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()
	spec := LiveSpec("exec-eph")

	var buf bytes.Buffer
	res, err := r.RunEphemeral(ctx, spec, coreruntime.ExecRequest{
		Argv:   []string{"sh", "-c", "echo ephemeral-ok"},
		Stdout: &buf,
	})
	if err != nil {
		t.Fatalf("RunEphemeral: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
	if got := buf.String(); got != "ephemeral-ok\n" {
		t.Fatalf("stdout = %q, want ephemeral-ok\\n", got)
	}

	name := SDKName(spec.Project, spec.Name)
	_, err = msbsdk.GetSandbox(ctx, name)
	if err == nil {
		t.Fatalf("sandbox %q still exists after RunEphemeral", name)
	}
}

func TestLiveExecStoppedSandboxErrors(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()
	spec := LiveSpec("exec")
	ref, err := r.CreateAndBoot(ctx, spec)
	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}
	CleanupSandbox(t, r, ref)

	h, err := r.handle(ctx, ref)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if err := h.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	_, err = r.Exec(ctx, ref, coreruntime.ExecRequest{
		Argv: []string{"echo", "nope"},
	})
	if err == nil {
		t.Fatal("Exec on stopped sandbox: want error, got nil")
	}
}
