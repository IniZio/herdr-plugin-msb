package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/IniZio/herdr-plugin-msb/internal/core/portfwd"
)

var execHerdrFn = func(bin string, argv, env []string) error {
	return syscall.Exec(bin, argv, env)
}

func ExtractRemoteTarget(args []string) (string, bool) {
	for i, a := range args {
		for _, prefix := range []string{"--remote=", "-remote="} {
			if strings.HasPrefix(a, prefix) {
				return a[len(prefix):], true
			}
		}
		if (a == "--remote" || a == "-remote") && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func AgentPidPath(stateDir, target string) string {
	return filepath.Join(stateDir, target+".agent.pid")
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

func RunWrapHerdr(ctx context.Context, args []string, _ io.Writer, stderr io.Writer) int {
	herdrBin, err := resolveHerdrBin()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	target, hasRemote := ExtractRemoteTarget(args)
	if !hasRemote {
		if err := execHerdrFn(herdrBin, append([]string{herdrBin}, args...), os.Environ()); err != nil {
			fmt.Fprintln(stderr, "wrap-herdr:", err)
			return 1
		}
		return 0
	}
	stateDir, err := StateDir(os.Getenv)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	selfBin, err := os.Executable()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	agentPid, err := SpawnIfAbsent(stateDir, selfBin, target)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctlPath := ControlPathFor(stateDir, target)
	if err := WaitForSocket(ctlPath, 15*time.Second); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	cmd := exec.CommandContext(ctx, herdrBin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	teardownSession(portfwd.OSRunner, agentPid, AgentPidPath(stateDir, target), target, ctlPath)
	return code
}

func resolveHerdrBin() (string, error) {
	if v := os.Getenv("HERDR_BIN"); v != "" {
		return v, nil
	}
	if v := os.Getenv("HERDR_BIN_PATH"); v != "" {
		return v, nil
	}
	return exec.LookPath("herdr")
}
