package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/IniZio/herdr-plugin-msb/internal/core/portfwd"
)

func TestExtractRemoteTarget(t *testing.T) {
	cases := []struct {
		args    []string
		want    string
		wantOK  bool
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

func TestSpawnIfAbsentIdempotent(t *testing.T) {
	dir := t.TempDir()
	target := "engine-03"

	pidPath := AgentPidPath(dir, target)
	if err := os.WriteFile(pidPath, []byte(pidToStr(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}

	var calls int
	spy := func(_ string) (int, error) {
		calls++
		return 0, nil
	}
	pid, err := spawnIfAbsent(dir, target, spy)
	if err != nil {
		t.Fatalf("spawnIfAbsent: %v", err)
	}
	if calls != 0 {
		t.Errorf("spy called %d times; want 0", calls)
	}
	if pid != os.Getpid() {
		t.Errorf("pid = %d; want %d", pid, os.Getpid())
	}

	dir2 := t.TempDir()
	var calls2 int
	spy2 := func(_ string) (int, error) {
		calls2++
		return os.Getpid(), nil
	}
	_, err = spawnIfAbsent(dir2, target, spy2)
	if err != nil {
		t.Fatalf("spawnIfAbsent (no pidfile): %v", err)
	}
	if calls2 != 1 {
		t.Errorf("spy2 called %d times; want 1", calls2)
	}
}

func TestSpawnIfAbsentRace(t *testing.T) {
	dir := t.TempDir()
	target := "engine-03"

	var spawnCount int64
	spawn := func(_ string) (int, error) {
		atomic.AddInt64(&spawnCount, 1)
		time.Sleep(30 * time.Millisecond)
		return os.Getpid(), nil
	}

	start := make(chan struct{})
	pids := make([]int, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			pids[i], errs[i] = spawnIfAbsent(dir, target, spawn)
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
	if n := atomic.LoadInt64(&spawnCount); n != 1 {
		t.Errorf("spawnCount = %d; want 1", n)
	}
	if pids[0] != pids[1] {
		t.Errorf("pids differ: %d vs %d", pids[0], pids[1])
	}
}

func TestTeardownSessionKillsProcess(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid

	if !isAlive(pid) {
		t.Fatal("process not alive before teardown (opposite-outcome probe failed)")
	}

	dir := t.TempDir()
	pidPath := filepath.Join(dir, "agent.pid")
	if err := os.WriteFile(pidPath, []byte(pidToStr(pid)), 0o600); err != nil {
		t.Fatal(err)
	}

	var capturedArgv []string
	mockRun := portfwd.Runner(func(_ context.Context, argv []string) (string, string, int, error) {
		capturedArgv = argv
		return "", "", 0, nil
	})

	teardownSession(mockRun, pid, pidPath, "target", "ctlPath")

	cmd.Wait()

	if isAlive(pid) {
		t.Error("process still alive after teardown")
	}
	if _, err := os.Stat(pidPath); err == nil {
		t.Error("pidfile still exists after teardown")
	}
	want := []string{"ssh", "-S", "ctlPath", "-O", "exit", "target"}
	if len(capturedArgv) != len(want) {
		t.Errorf("runner argv = %v; want %v", capturedArgv, want)
	} else {
		for i, v := range want {
			if capturedArgv[i] != v {
				t.Errorf("runner argv[%d] = %q; want %q", i, capturedArgv[i], v)
			}
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

func TestAgentPidPath(t *testing.T) {
	if got := AgentPidPath("/state", "engine-03"); got != "/state/engine-03.agent.pid" {
		t.Errorf("got %q", got)
	}
	if got := AgentPidPath("/state", "newman@engine-03"); got != "/state/newman@engine-03.agent.pid" {
		t.Errorf("got %q", got)
	}
}

func pidToStr(n int) string {
	buf := make([]byte, 0, 12)
	if n == 0 {
		return "0"
	}
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
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

var _ = syscall.Flock
