package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/IniZio/herdr-plugin-msb/internal/core/portfwd"
)

func TestExtractRemoteTarget(t *testing.T) {
	cases := []struct {
		args   []string
		want   string
		wantOK bool
	}{
		{[]string{"--remote", "engine-03"}, "engine-03", true},
		{[]string{"--remote", "newman@engine-03", "--session", "default"}, "newman@engine-03", true},
		{[]string{"--remote=engine-03"}, "engine-03", true},
		{[]string{"-remote", "engine-03"}, "engine-03", true},
		{[]string{"--session", "local"}, "", false},
		{[]string{}, "", false},
	}
	for _, c := range cases {
		got, ok := ExtractRemoteTarget(c.args)
		if ok != c.wantOK || got != c.want {
			t.Errorf("ExtractRemoteTarget(%v) = %q,%v; want %q,%v", c.args, got, ok, c.want, c.wantOK)
		}
	}
}

func TestWrapHerdrNoRemoteExecsHerdr(t *testing.T) {
	orig := execHerdrFn
	defer func() { execHerdrFn = orig }()

	var calledBin string
	var calledArgv []string
	execHerdrFn = func(bin string, argv, env []string) error {
		calledBin = bin
		calledArgv = argv
		return nil
	}

	t.Setenv("HERDR_BIN", "/bin/true")
	dir := t.TempDir()
	t.Setenv("HERDR_STATE_DIR", dir)

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	RunWrapHerdr(context.Background(), []string{"--session", "local"}, out, errOut)

	if calledBin != "/bin/true" {
		t.Errorf("bin = %q; want /bin/true", calledBin)
	}
	if len(calledArgv) == 0 || calledArgv[0] != "/bin/true" {
		t.Errorf("argv[0] = %q; want /bin/true", firstOrEmpty(calledArgv))
	}
	if len(calledArgv) < 2 || calledArgv[1] != "--session" {
		t.Errorf("argv[1] = %q; want --session", nthOrEmpty(calledArgv, 1))
	}

	pidPath := AgentPidPath(dir, "")
	if _, err := os.Stat(pidPath); err == nil {
		t.Error("unexpected pidfile created for no-remote run")
	}
}

func TestRunAttachNoTarget(t *testing.T) {
	var stderr bytes.Buffer
	code := RunAttach(context.Background(), nil, nil, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d; want 2", code)
	}
	if !strings.Contains(stderr.String(), "usage") {
		t.Errorf("stderr %q missing usage", stderr.String())
	}
}

func TestRunAttachMissingHerdrBin(t *testing.T) {
	t.Setenv("HERDR_BIN", "")
	t.Setenv("HERDR_BIN_PATH", "")
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "")
	defer t.Setenv("PATH", origPath)

	var stderr bytes.Buffer
	code := RunAttach(context.Background(), []string{"engine-03"}, nil, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d; want 1 (stderr: %q)", code, stderr.String())
	}
	if stderr.Len() == 0 {
		t.Error("stderr empty; expected error message")
	}
}

func attachTestEnv(t *testing.T) string {
	t.Helper()
	xdg := t.TempDir()
	stateDir := filepath.Join(xdg, StateDirNS)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("HOME", "")
	t.Setenv("HERDR_BIN", "/bin/true")
	return stateDir
}

func seedAttachState(t *testing.T, stateDir, target string) {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() }) //nolint:errcheck
	pid := cmd.Process.Pid
	if err := os.WriteFile(AgentPidPath(stateDir, target), []byte(strconv.Itoa(pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ControlPathFor(stateDir, target), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRunAttachHappyPath(t *testing.T) {
	stateDir := attachTestEnv(t)
	target := "engine-03"
	seedAttachState(t, stateDir, target)

	origChild := runHerdrChildFn
	origRunner := attachRunnerFn
	defer func() { runHerdrChildFn = origChild; attachRunnerFn = origRunner }()

	var childBin string
	var childArgs []string
	runHerdrChildFn = func(_ context.Context, bin string, args []string) int {
		childBin = bin
		childArgs = append([]string(nil), args...)
		return 0
	}

	var runnerArgv []string
	attachRunnerFn = portfwd.Runner(func(_ context.Context, argv []string) (string, string, int, error) {
		runnerArgv = append([]string(nil), argv...)
		return "", "", 0, nil
	})

	var stderr bytes.Buffer
	code := RunAttach(context.Background(), []string{target}, nil, &stderr)

	if code != 0 {
		t.Errorf("code = %d; want 0 (stderr: %q)", code, stderr.String())
	}
	if childBin != "/bin/true" {
		t.Errorf("childBin = %q; want /bin/true", childBin)
	}
	if len(childArgs) < 2 || childArgs[0] != "--remote" || childArgs[1] != target {
		t.Errorf("childArgs = %v; want [--remote %s ...]", childArgs, target)
	}
	if len(runnerArgv) == 0 {
		t.Error("teardown runner not invoked")
	}
	if _, err := os.Stat(AgentPidPath(stateDir, target)); err == nil {
		t.Error("pidfile still exists after teardown")
	}
}

func TestRunAttachExitCodePropagation(t *testing.T) {
	stateDir := attachTestEnv(t)
	target := "engine-03"
	seedAttachState(t, stateDir, target)

	origChild := runHerdrChildFn
	origRunner := attachRunnerFn
	defer func() { runHerdrChildFn = origChild; attachRunnerFn = origRunner }()

	runHerdrChildFn = func(_ context.Context, _ string, _ []string) int { return 3 }
	attachRunnerFn = portfwd.Runner(func(_ context.Context, _ []string) (string, string, int, error) {
		return "", "", 0, nil
	})

	var stderr bytes.Buffer
	code := RunAttach(context.Background(), []string{target}, nil, &stderr)
	if code != 3 {
		t.Errorf("code = %d; want 3", code)
	}
}

func TestRunAttachTeardownOnFailure(t *testing.T) {
	stateDir := attachTestEnv(t)
	target := "engine-03"
	seedAttachState(t, stateDir, target)

	origChild := runHerdrChildFn
	origRunner := attachRunnerFn
	defer func() { runHerdrChildFn = origChild; attachRunnerFn = origRunner }()

	runHerdrChildFn = func(_ context.Context, _ string, _ []string) int { return 7 }

	var teardownCalled bool
	attachRunnerFn = portfwd.Runner(func(_ context.Context, _ []string) (string, string, int, error) {
		teardownCalled = true
		return "", "", 0, nil
	})

	var stderr bytes.Buffer
	code := RunAttach(context.Background(), []string{target}, nil, &stderr)

	if code != 7 {
		t.Errorf("code = %d; want 7", code)
	}
	if !teardownCalled {
		t.Error("teardown not called after child failure")
	}
}

func TestRunAttachArgPassthrough(t *testing.T) {
	stateDir := attachTestEnv(t)
	target := "engine-03"
	seedAttachState(t, stateDir, target)

	origChild := runHerdrChildFn
	origRunner := attachRunnerFn
	defer func() { runHerdrChildFn = origChild; attachRunnerFn = origRunner }()

	var childArgs []string
	runHerdrChildFn = func(_ context.Context, _ string, args []string) int {
		childArgs = append([]string(nil), args...)
		return 0
	}
	attachRunnerFn = portfwd.Runner(func(_ context.Context, _ []string) (string, string, int, error) {
		return "", "", 0, nil
	})

	var stderr bytes.Buffer
	RunAttach(context.Background(), []string{target, "--foo", "bar"}, nil, &stderr)

	want := []string{"--remote", target, "--foo", "bar"}
	if len(childArgs) != len(want) {
		t.Fatalf("childArgs = %v; want %v", childArgs, want)
	}
	for i, v := range want {
		if childArgs[i] != v {
			t.Errorf("childArgs[%d] = %q; want %q", i, childArgs[i], v)
		}
	}
}

func firstOrEmpty(s []string) string {
	if len(s) > 0 {
		return s[0]
	}
	return ""
}

func nthOrEmpty(s []string, n int) string {
	if len(s) > n {
		return s[n]
	}
	return ""
}

var _ = syscall.Flock
