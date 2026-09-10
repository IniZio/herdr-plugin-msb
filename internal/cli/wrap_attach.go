//go:build linux

package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/IniZio/herdr-plugin-msb/internal/core/portfwd"
)

var execHerdrFn = func(bin string, argv, env []string) error {
	return syscall.Exec(bin, argv, env)
}

var runHerdrChildFn = func(ctx context.Context, bin string, args []string) int {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	return 0
}

var attachRunnerFn portfwd.Runner = portfwd.OSRunner

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

func RunAttach(ctx context.Context, args []string, _ io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: herdr-plugin-msb attach <target> [herdr args...]")
		return 2
	}
	target := args[0]
	herdrArgs := append([]string{"--remote", target}, args[1:]...)
	herdrBin, err := resolveHerdrBin()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
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
	code := runHerdrChildFn(ctx, herdrBin, herdrArgs)
	TeardownSession(attachRunnerFn, agentPid, AgentPidPath(stateDir, target), target, ctlPath)
	return code
}

func RunWrapHerdr(ctx context.Context, args []string, _ io.Writer, stderr io.Writer) int {
	fmt.Fprintln(stderr, "wrap-herdr: deprecated; use: herdr-plugin-msb attach <target>")
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
	TeardownSession(portfwd.OSRunner, agentPid, AgentPidPath(stateDir, target), target, ctlPath)
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
