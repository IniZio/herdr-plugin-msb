package clientagent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/IniZio/herdr-plugin-msb/internal/core/portfwd"
)

func AgentPidPath(stateDir, target string) string {
	return filepath.Join(stateDir, target+".agent.pid")
}

func AgentLogPath(stateDir, target string) string {
	return filepath.Join(stateDir, target+".agent.log")
}

func readPid(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}
	return pid, true
}

func isAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

type agentSpawnFn func(target string) (int, error)

func spawnIfAbsent(stateDir, target string, spawn agentSpawnFn) (int, error) {
	lockPath := filepath.Join(stateDir, target+".agent.lock")
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return 0, fmt.Errorf("open lock: %w", err)
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return 0, fmt.Errorf("flock: %w", err)
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN) //nolint:errcheck

	pidPath := AgentPidPath(stateDir, target)
	if pid, ok := readPid(pidPath); ok && isAlive(pid) {
		return pid, nil
	}
	pid, err := spawn(target)
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0600); err != nil {
		return 0, fmt.Errorf("write pidfile: %w", err)
	}
	return pid, nil
}

func SpawnIfAbsent(stateDir, selfBin, target string) (int, error) {
	return spawnIfAbsent(stateDir, target, func(t string) (int, error) {
		cmd := exec.Command(selfBin, "local-agent", "--target", t)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		lf, err := os.OpenFile(AgentLogPath(stateDir, t), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return 0, fmt.Errorf("open agent log: %w", err)
		}
		defer lf.Close()
		cmd.Stdout = lf
		cmd.Stderr = lf
		if err := cmd.Start(); err != nil {
			return 0, err
		}
		return cmd.Process.Pid, nil
	})
}

func WaitForSocket(socketPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("socket %s not ready after %s", socketPath, timeout)
}

func TeardownSession(run portfwd.Runner, agentPid int, pidPath, target, ctlPath string) {
	teardownSession(run, agentPid, pidPath, target, ctlPath)
}

func teardownSession(run portfwd.Runner, agentPid int, pidPath, target, ctlPath string) {
	_ = syscall.Kill(agentPid, syscall.SIGTERM)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && isAlive(agentPid) {
		time.Sleep(100 * time.Millisecond)
	}
	_ = os.Remove(pidPath)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	run(ctx, []string{"ssh", "-S", ctlPath, "-O", "exit", target}) //nolint:errcheck
}
