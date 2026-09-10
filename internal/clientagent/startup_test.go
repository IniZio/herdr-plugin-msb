package clientagent

import (
	"bytes"
	"context"
	"errors"
	"io"
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

func TestMonitorSelectionTeardownOnDeselect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initial := []portfwd.Machine{{SSHTarget: "engine-03", Selected: true}}

	discover := func(_ context.Context) ([]portfwd.Machine, error) {
		return []portfwd.Machine{{SSHTarget: "engine-03", Selected: false}}, nil
	}

	torn := make(chan string, 1)
	teardown := func(_ string, m portfwd.Machine) { torn <- m.SSHTarget }

	go monitorSelection(ctx, t.TempDir(), initial, discover, teardown, time.Millisecond)

	select {
	case target := <-torn:
		if target != "engine-03" {
			t.Errorf("torn down %q; want engine-03", target)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("teardown not called after machine deselected")
	}
}

func TestMonitorSelectionNoTeardownWhileSelected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	initial := []portfwd.Machine{{SSHTarget: "engine-03", Selected: true}}

	polls := make(chan struct{}, 32)
	discover := func(_ context.Context) ([]portfwd.Machine, error) {
		select {
		case polls <- struct{}{}:
		default:
		}
		return []portfwd.Machine{{SSHTarget: "engine-03", Selected: true}}, nil
	}

	torn := make(chan string, 1)
	teardown := func(_ string, m portfwd.Machine) { torn <- m.SSHTarget }

	go monitorSelection(ctx, t.TempDir(), initial, discover, teardown, time.Millisecond)

	for i := 0; i < 3; i++ {
		select {
		case <-polls:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("discover not called (poll loop stalled)")
		}
	}
	cancel()

	select {
	case target := <-torn:
		t.Errorf("unexpected teardown of %q while machine remains selected", target)
	default:
	}
}

func TestMonitorSelectionNoTeardownOnDiscoverError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	initial := []portfwd.Machine{{SSHTarget: "engine-03", Selected: true}}

	polls := make(chan struct{}, 32)
	discover := func(_ context.Context) ([]portfwd.Machine, error) {
		select {
		case polls <- struct{}{}:
		default:
		}
		return nil, errors.New("herdr server unreachable")
	}

	torn := make(chan string, 1)
	teardown := func(_ string, m portfwd.Machine) { torn <- m.SSHTarget }

	go monitorSelection(ctx, t.TempDir(), initial, discover, teardown, time.Millisecond)

	for i := 0; i < 3; i++ {
		select {
		case <-polls:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("discover not called (poll loop stalled)")
		}
	}
	cancel()

	select {
	case target := <-torn:
		t.Errorf("unexpected teardown of %q on discovery error", target)
	default:
	}
}

func TestLocalAgentStartupTeardownRunsOnExit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	prevD := localAgentDiscoverFn
	localAgentDiscoverFn = func(_ context.Context) ([]portfwd.Machine, error) {
		return []portfwd.Machine{{SSHTarget: "host1", Enabled: true, Selected: true}}, nil
	}
	defer func() { localAgentDiscoverFn = prevD }()

	prevS := localAgentSpawnFn
	localAgentSpawnFn = func(_, _, _ string) (int, error) { return 999, nil }
	defer func() { localAgentSpawnFn = prevS }()

	prevProv := localAgentProvisionFn
	localAgentProvisionFn = func(_ context.Context, _ string, _ portfwd.Machine, _ io.Writer) error { return nil }
	defer func() { localAgentProvisionFn = prevProv }()

	prevP := localAgentParentDiedFn
	localAgentParentDiedFn = func(_ context.Context) <-chan struct{} {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	defer func() { localAgentParentDiedFn = prevP }()

	torn := make(chan []portfwd.Machine, 1)
	prevT := LocalAgentStartupTeardownFn
	LocalAgentStartupTeardownFn = func(_ string, machines []portfwd.Machine) {
		cp := make([]portfwd.Machine, len(machines))
		copy(cp, machines)
		torn <- cp
	}
	defer func() { LocalAgentStartupTeardownFn = prevT }()

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

	select {
	case machines := <-torn:
		if len(machines) != 1 || machines[0].SSHTarget != "host1" {
			t.Errorf("teardown called with %v; want [{SSHTarget:host1}]", machines)
		}
	default:
		t.Fatal("LocalAgentStartupTeardownFn was not called on exit")
	}
}

func TestLocalAgentStartupProvisions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	var provisioned []string
	prevProv := localAgentProvisionFn
	localAgentProvisionFn = func(_ context.Context, _ string, m portfwd.Machine, _ io.Writer) error {
		provisioned = append(provisioned, m.SSHTarget)
		return nil
	}
	defer func() { localAgentProvisionFn = prevProv }()

	prevD := localAgentDiscoverFn
	localAgentDiscoverFn = func(_ context.Context) ([]portfwd.Machine, error) {
		return []portfwd.Machine{{SSHTarget: "engine-host", Enabled: true}}, nil
	}
	defer func() { localAgentDiscoverFn = prevD }()

	prevS := localAgentSpawnFn
	localAgentSpawnFn = func(_, _, _ string) (int, error) { return 999, nil }
	defer func() { localAgentSpawnFn = prevS }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var stderr bytes.Buffer
	code := RunLocalAgentStartup(ctx, nil, nil, &stderr)
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr: %s", code, stderr.String())
	}
	if len(provisioned) != 1 {
		t.Fatalf("want 1 provision call, got %d: %v", len(provisioned), provisioned)
	}
	if provisioned[0] != "engine-host" {
		t.Fatalf("want provision for engine-host, got %q", provisioned[0])
	}
}
