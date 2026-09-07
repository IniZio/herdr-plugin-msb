//go:build live

package portfwd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

const (
	liveSandbox = "s04-live"
	livePort    = 45459
)

type msbCLI struct{ t *testing.T }

func (m msbCLI) List(ctx context.Context) ([]runtime.SandboxRef, error) {
	out, err := exec.CommandContext(ctx, "msb", "list", "--format", "json").Output()
	if err != nil {
		return nil, fmt.Errorf("msb list: %w", err)
	}
	var rows []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("msb list json: %w", err)
	}
	refs := make([]runtime.SandboxRef, 0, len(rows))
	for _, r := range rows {
		refs = append(refs, runtime.SandboxRef{
			ID:     r.Name,
			Name:   r.Name,
			Status: runtime.SandboxStatus(strings.ToLower(r.Status)),
		})
	}
	return refs, nil
}

func (m msbCLI) Exec(ctx context.Context, ref runtime.SandboxRef, req runtime.ExecRequest) (runtime.ExecResult, error) {
	argv := append([]string{"exec", ref.Name, "--no-tty", "--timeout", "8s", "--"}, req.Argv...)
	cmd := exec.CommandContext(ctx, "msb", argv...)
	cmd.Stdout = req.Stdout
	cmd.Stderr = req.Stderr
	err := cmd.Run()
	if ex, ok := err.(*exec.ExitError); ok {
		return runtime.ExecResult{ExitCode: int32(ex.ExitCode())}, nil
	}
	if err != nil {
		return runtime.ExecResult{}, err
	}
	return runtime.ExecResult{}, nil
}

func mustRun(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v: %s", name, args, err, out)
	}
	return string(out)
}

func sshCheck(t *testing.T, controlPath string) string {
	t.Helper()
	out, _ := exec.Command("ssh", "-O", "check", "-o", "ControlPath="+controlPath, "127.0.0.1").CombinedOutput()
	return strings.TrimSpace(string(out))
}

func ssLines(t *testing.T, port int) []string {
	t.Helper()
	out := mustRun(t, "ss", "-ltn")
	var hits []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, fmt.Sprintf(":%d", port)) {
			hits = append(hits, strings.TrimSpace(line))
		}
	}
	return hits
}

func TestLivePortfwdEndToEnd(t *testing.T) {
	ctx := context.Background()
	rt := msbCLI{t: t}
	controlPath := fmt.Sprintf("/run/user/%d/cm-s04.sock", os.Getuid())

	refs, err := rt.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var target runtime.SandboxRef
	for _, r := range refs {
		if r.Name == liveSandbox {
			target = r
		}
	}
	if target.Name == "" || target.Status != runtime.SandboxStatusRunning {
		t.Fatalf("sandbox %s must be running before this test; got refs=%+v", liveSandbox, refs)
	}
	t.Logf("STORE IDENTITY: id=%q name=%q status=%q", target.ID, target.Name, target.Status)

	start := fmt.Sprintf("nohup nc -l -p %d >/dev/null 2>&1 & echo listener-started", livePort)
	var startOut strings.Builder
	if _, err := rt.Exec(ctx, target, runtime.ExecRequest{
		Argv:   []string{"sh", "-c", start},
		Stdout: &startOut,
	}); err != nil {
		t.Fatalf("start listener: %v", err)
	}
	t.Logf("IN-SANDBOX START: %s", strings.TrimSpace(startOut.String()))

	t.Logf("HOST ss BEFORE FORWARD, lines matching :%d = %v", livePort, ssLines(t, livePort))
	t.Logf("MSB OWN STATE: %s", strings.TrimSpace(mustRun(t, "msb", "status", liveSandbox, "--format", "json")))

	d := &Discoverer{RT: rt}
	var found *Listener
	for i := 0; i < 10; i++ {
		ls, err := d.DiscoverAll(ctx)
		if err != nil {
			t.Fatalf("discover: %v", err)
		}
		t.Logf("DISCOVERALL attempt %d: %+v", i+1, ls)
		for j := range ls {
			if ls[j].Port == livePort {
				found = &ls[j]
			}
		}
		if found != nil {
			break
		}
		time.Sleep(time.Second)
	}
	if found == nil {
		t.Fatalf("unpublished listener on %d not discovered", livePort)
	}
	t.Logf("DISCOVERED: port=%d bind=%q sandbox.id=%q sandbox.name=%q sandbox.status=%q",
		found.Port, found.BindAddr, found.Sandbox.ID, found.Sandbox.Name, found.Sandbox.Status)

	fw := &Forwarder{ControlPath: controlPath, SSHHost: "127.0.0.1", Run: OSRunner}
	if err := fw.EnsureMaster(ctx); err != nil {
		t.Fatalf("ensure master: %v", err)
	}
	defer exec.Command("ssh", "-O", "exit", "-o", "ControlPath="+controlPath, "127.0.0.1").Run()
	t.Logf("SSH -O CHECK after master open: %s", sshCheck(t, controlPath))

	m := NewManager(fw)
	if err := m.Reconcile(ctx, []Listener{*found}); err != nil {
		t.Fatalf("reconcile apply: %v", err)
	}
	present, err := fw.Present(ctx, livePort)
	if err != nil {
		t.Fatalf("present: %v", err)
	}
	t.Logf("FORWARD PRESENT before stop = %v; ss lines matching :%d = %v", present, livePort, ssLines(t, livePort))
	t.Logf("SSH -O CHECK before stop: %s", sshCheck(t, controlPath))
	if !present {
		t.Fatalf("same-port forward on %d not present after apply", livePort)
	}

	probe, probeErr := exec.Command("curl", "-sS", "-m", "5", fmt.Sprintf("http://127.0.0.1:%d/", livePort)).CombinedOutput()
	t.Logf("TRAFFIC PROBE through same-port forward: out=%q err=%v", strings.TrimSpace(string(probe)), probeErr)

	t.Logf("MSB STOP: %s", strings.TrimSpace(mustRun(t, "msb", "stop", liveSandbox)))

	var afterRefs []runtime.SandboxRef
	for i := 0; i < 15; i++ {
		afterRefs, err = rt.List(ctx)
		if err != nil {
			t.Fatalf("list after stop: %v", err)
		}
		stillRunning := false
		for _, r := range afterRefs {
			if r.Name == liveSandbox && r.Status == runtime.SandboxStatusRunning {
				stillRunning = true
			}
		}
		t.Logf("POST-STOP LIST attempt %d: %+v", i+1, afterRefs)
		if !stillRunning {
			break
		}
		time.Sleep(2 * time.Second)
	}

	desired, err := d.DiscoverAll(ctx)
	if err != nil {
		t.Fatalf("discover after stop: %v", err)
	}
	t.Logf("DESIRED after stop: %+v", desired)
	if err := m.Reconcile(ctx, desired); err != nil {
		t.Fatalf("reconcile teardown: %v", err)
	}

	present, err = fw.Present(ctx, livePort)
	if err != nil {
		t.Fatalf("present after stop: %v", err)
	}
	t.Logf("FORWARD PRESENT after stop = %v; ss lines matching :%d = %v", present, livePort, ssLines(t, livePort))
	t.Logf("SSH -O CHECK after stop: %s", sshCheck(t, controlPath))
	if present {
		t.Fatalf("forward on %d still present after sandbox stop", livePort)
	}
}
