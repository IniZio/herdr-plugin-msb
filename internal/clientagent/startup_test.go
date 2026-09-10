package clientagent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/IniZio/herdr-plugin-msb/internal/portfwd"
)

func TestLocalAgentStartupZeroEnabledMachines(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	var spawned []string
	prevS := localAgentSpawnFn
	localAgentSpawnFn = func(_, _, target string) (int, error) {
		spawned = append(spawned, target)
		return 999, nil
	}
	defer func() { localAgentSpawnFn = prevS }()

	prevD := localAgentDiscoverFn
	localAgentDiscoverFn = func(_ context.Context) ([]portfwd.Machine, error) {
		return []portfwd.Machine{{SSHTarget: "disabled-host", Enabled: false}}, nil
	}
	defer func() { localAgentDiscoverFn = prevD }()

	done := make(chan int, 1)
	go func() {
		var stderr bytes.Buffer
		done <- RunLocalAgentStartup(context.Background(), nil, nil, &stderr)
	}()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("want exit 0, got %d", code)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("RunLocalAgentStartup did not return promptly with zero enabled machines")
	}
	if len(spawned) != 0 {
		t.Fatalf("want 0 spawns, got %d: %v", len(spawned), spawned)
	}
}

func TestLocalAgentStartupSingleInstance(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	stateDir := filepath.Join(dir, StateDirNS)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	lf, err := os.OpenFile(filepath.Join(stateDir, startupLockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}

	var spawned []string
	prevS := localAgentSpawnFn
	localAgentSpawnFn = func(_, _, target string) (int, error) {
		spawned = append(spawned, target)
		return 999, nil
	}
	defer func() { localAgentSpawnFn = prevS }()

	prevD := localAgentDiscoverFn
	localAgentDiscoverFn = func(_ context.Context) ([]portfwd.Machine, error) {
		return []portfwd.Machine{{SSHTarget: "live-host", Enabled: true}}, nil
	}
	defer func() { localAgentDiscoverFn = prevD }()

	done := make(chan int, 1)
	go func() {
		var stderr bytes.Buffer
		done <- RunLocalAgentStartup(context.Background(), nil, nil, &stderr)
	}()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("want exit 0, got %d", code)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("second invocation blocked — single-instance guard not working")
	}
	if len(spawned) != 0 {
		t.Fatalf("want 0 spawns (suppressed by single-instance guard), got %d: %v", len(spawned), spawned)
	}
}

func TestLocalAgentStartupExitsOnParentDeath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	prevD := localAgentDiscoverFn
	localAgentDiscoverFn = func(_ context.Context) ([]portfwd.Machine, error) {
		return []portfwd.Machine{{SSHTarget: "host1", Enabled: true}}, nil
	}
	defer func() { localAgentDiscoverFn = prevD }()

	prevS := localAgentSpawnFn
	localAgentSpawnFn = func(_, _, _ string) (int, error) { return 999, nil }
	defer func() { localAgentSpawnFn = prevS }()

	prevP := localAgentParentDiedFn
	localAgentParentDiedFn = func(_ context.Context) <-chan struct{} {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	defer func() { localAgentParentDiedFn = prevP }()

	done := make(chan int, 1)
	go func() {
		var stderr bytes.Buffer
		done <- RunLocalAgentStartup(context.Background(), nil, nil, &stderr)
	}()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("want exit 0, got %d", code)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("RunLocalAgentStartup did not exit on simulated parent death")
	}
}

func TestLocalAgentStartupOnlyEnabledServed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	var spawned []string
	prevS := localAgentSpawnFn
	localAgentSpawnFn = func(_, _, target string) (int, error) {
		spawned = append(spawned, target)
		return 999, nil
	}
	defer func() { localAgentSpawnFn = prevS }()

	prevD := localAgentDiscoverFn
	localAgentDiscoverFn = func(_ context.Context) ([]portfwd.Machine, error) {
		return []portfwd.Machine{
			{SSHTarget: "enabled-host", Enabled: true},
			{SSHTarget: "disabled-host", Enabled: false},
		}, nil
	}
	defer func() { localAgentDiscoverFn = prevD }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var stderr bytes.Buffer
	code := RunLocalAgentStartup(ctx, nil, nil, &stderr)
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr: %s", code, stderr.String())
	}
	if len(spawned) != 1 {
		t.Fatalf("want 1 spawn, got %d: %v", len(spawned), spawned)
	}
	if spawned[0] != "enabled-host" {
		t.Fatalf("want spawn for enabled-host, got %q", spawned[0])
	}
}
