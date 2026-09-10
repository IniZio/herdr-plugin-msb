package clientagent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	corepf "github.com/IniZio/herdr-plugin-msb/internal/core/portfwd"
	"github.com/IniZio/herdr-plugin-msb/internal/portfwd"
)

const startupLockFile = "local-agent-startup.lock"

var localAgentDiscoverFn = func(ctx context.Context) ([]portfwd.Machine, error) {
	run := portfwd.Runner(func(ctx context.Context, argv []string) (string, string, int, error) {
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		var out, errb bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errb
		err := cmd.Run()
		code := 0
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		return out.String(), errb.String(), code, err
	})
	return portfwd.DiscoverMachines(ctx, run)
}

var localAgentSpawnFn = func(stateDir, selfBin, target string) (int, error) {
	return SpawnIfAbsent(stateDir, selfBin, target)
}

var localAgentProvisionFn = func(ctx context.Context, stateDir string, m portfwd.Machine, stderr io.Writer) error {
	cs := &ConsentStore{Dir: stateDir}
	pluginRoot := os.Getenv("HERDR_PLUGIN_ROOT")
	binSrc, pluginTOML := "", ""
	if pluginRoot != "" {
		binSrc = filepath.Join(pluginRoot, "herdr-plugin-msb")
		pluginTOML = filepath.Join(pluginRoot, "herdr-plugin.toml")
	}
	p := &Provisioner{
		Target:       m.SSHTarget,
		BinSrc:       binSrc,
		PluginTOML:   pluginTOML,
		LocalVersion: PluginVersion,
		CheckConsent: cs.CheckFn,
		Run:          corepf.OSRunner,
		Copy:         SCPCopy,
		Stderr:       stderr,
	}
	return p.EnsureProvisioned(ctx)
}

const selectionPollInterval = 30 * time.Second

var localAgentTeardownMachineFn = func(stateDir string, m portfwd.Machine) {
	pidPath := AgentPidPath(stateDir, m.SSHTarget)
	ctlPath := ControlPathFor(stateDir, m.SSHTarget)
	pid, ok := readPid(pidPath)
	if !ok || !isAlive(pid) {
		return
	}
	teardownSession(corepf.OSRunner, pid, pidPath, m.SSHTarget, ctlPath)
}

var LocalAgentStartupTeardownFn = func(stateDir string, machines []portfwd.Machine) {
	for _, m := range machines {
		localAgentTeardownMachineFn(stateDir, m)
	}
}

func monitorSelection(
	ctx context.Context,
	stateDir string,
	initial []portfwd.Machine,
	discover func(context.Context) ([]portfwd.Machine, error),
	teardown func(string, portfwd.Machine),
	interval time.Duration,
) {
	selected := make(map[string]bool, len(initial))
	for _, m := range initial {
		selected[m.SSHTarget] = m.Selected
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		machines, err := discover(ctx)
		if err != nil {
			continue
		}
		current := make(map[string]bool, len(machines))
		for _, m := range machines {
			current[m.SSHTarget] = m.Selected
		}
		for _, m := range initial {
			was := selected[m.SSHTarget]
			now := current[m.SSHTarget]
			if was && !now {
				teardown(stateDir, m)
				selected[m.SSHTarget] = false
			} else if !was && now {
				selected[m.SSHTarget] = true
			}
		}
	}
}

var localAgentParentDiedFn = func(ctx context.Context) <-chan struct{} {
	ch := make(chan struct{})
	ppid := os.Getppid()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				if os.Getppid() != ppid {
					close(ch)
					return
				}
			}
		}
	}()
	return ch
}

func RunLocalAgentStartup(ctx context.Context, _ []string, _ io.Writer, stderr io.Writer) int {
	stateDir, err := StateDir(os.Getenv)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	lf, err := os.OpenFile(filepath.Join(stateDir, startupLockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		fmt.Fprintln(stderr, "local-agent-startup: open lock:", err)
		return 1
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return 0
	}
	machines, err := localAgentDiscoverFn(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "local-agent-startup: discover:", err)
		return 1
	}
	var enabled []portfwd.Machine
	for _, m := range machines {
		if m.Enabled {
			enabled = append(enabled, m)
		}
	}
	if len(enabled) == 0 {
		return 0
	}
	selfBin, err := os.Executable()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, m := range enabled {
		if err := localAgentProvisionFn(ctx, stateDir, m, stderr); err != nil && !errors.Is(err, ErrNoConsent) {
			fmt.Fprintf(stderr, "local-agent-startup: provision %s: %v\n", m.SSHTarget, err)
		}
	}
	for _, m := range enabled {
		if _, err := localAgentSpawnFn(stateDir, selfBin, m.SSHTarget); err != nil {
			fmt.Fprintf(stderr, "local-agent-startup: spawn %s: %v\n", m.SSHTarget, err)
		}
	}
	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGHUP, os.Interrupt)
	defer stop()
	parentDied := localAgentParentDiedFn(sigCtx)
	go monitorSelection(sigCtx, stateDir, enabled, localAgentDiscoverFn, localAgentTeardownMachineFn, selectionPollInterval)
	select {
	case <-sigCtx.Done():
	case <-parentDied:
	}
	LocalAgentStartupTeardownFn(stateDir, enabled)
	return 0
}
