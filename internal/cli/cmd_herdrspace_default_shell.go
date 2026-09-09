package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/IniZio/herdr-plugin-msb/internal/core/herdrspace"
	"github.com/IniZio/herdr-plugin-msb/internal/core/service"
)

var execProcess func(string, []string, []string) error = func(argv0 string, argv []string, envv []string) error {
	if testing.Testing() {
		return fmt.Errorf("refused to exec under go test: %s", argv0)
	}
	return syscall.Exec(argv0, argv, envv)
}

const defaultShellSentinel = "HERDR_MSB_DEFAULT_SHELL_ACTIVE"

const defaultShellProbeTimeout = 5 * time.Second

type dsDecision struct {
	useGuest bool
	project  string
	sandbox  string
}

func defaultShellDecide(
	ctx context.Context,
	getenv func(string) string,
	storeRoot string,
	lookup func(context.Context, string, string) (herdrspace.Binding, error),
) dsDecision {
	if getenv(defaultShellSentinel) != "" {
		return dsDecision{}
	}
	wsID := getenv("HERDR_WORKSPACE_ID")
	if wsID == "" || storeRoot == "" {
		return dsDecision{}
	}
	b, err := lookup(ctx, storeRoot, wsID)
	if err != nil {
		if !errors.Is(err, herdrspace.ErrNotFound) {
			fmt.Fprintf(os.Stderr, "default-shell: lookup: %v\n", err)
		}
		return dsDecision{}
	}
	project, name := splitHandle(b.SandboxHandle)
	if name == "" {
		return dsDecision{}
	}
	return dsDecision{useGuest: true, project: project, sandbox: name}
}

func guestShellArgv(plugin, project, sandbox, guestShell string) []string {
	cdScript := "cd " + service.DefaultGuestWorktree + " 2>/dev/null || cd /; exec \"$0\" \"$@\""
	argv := []string{plugin, "exec", "-pty", "-project", project, sandbox, "--", "/bin/sh", "-c", cdScript, guestShell}
	if strings.HasSuffix(guestShell, "bash") {
		argv = append(argv, "-l")
	}
	return argv
}

func runDefaultShell(ctx context.Context, _ []string, _ io.Writer, errW io.Writer) int {
	dir, dirErr := StateDir(os.Getenv)
	if dirErr != nil {
		fmt.Fprintln(errW, "default-shell:", dirErr)
	}

	dec := defaultShellDecide(ctx, os.Getenv, dir, herdrspace.GetByWorkspaceID)

	hostShell := func() int {
		sh := os.Getenv("SHELL")
		if sh == "" {
			sh = "/bin/sh"
		}
		if err := execProcess(sh, []string{sh}, os.Environ()); err != nil {
			fmt.Fprintln(errW, "default-shell: host exec:", err)
			return 1
		}
		return 0
	}

	if !dec.useGuest {
		return hostShell()
	}

	plugin, err := os.Executable()
	if err != nil {
		fmt.Fprintln(errW, "default-shell: resolve binary:", err)
		return hostShell()
	}

	probeCtx, cancelProbe := context.WithTimeout(ctx, defaultShellProbeTimeout)
	probeOut, _ := exec.CommandContext(probeCtx, plugin,
		"exec", "-project", dec.project, dec.sandbox,
		"--", "/bin/sh", "-c", "command -v bash 2>/dev/null || echo /bin/sh",
	).Output()
	cancelProbe()

	guestShell := "/bin/sh"
	if trimmed := strings.TrimSpace(string(probeOut)); trimmed != "" {
		lines := strings.Split(trimmed, "\n")
		if last := strings.TrimRight(lines[len(lines)-1], "\r"); last != "" {
			guestShell = last
		}
	}

	argv := guestShellArgv(plugin, dec.project, dec.sandbox, guestShell)

	env := append(os.Environ(), defaultShellSentinel+"=1")
	if execErr := execProcess(plugin, argv, env); execErr != nil {
		fmt.Fprintln(errW, "default-shell: exec guest:", execErr)
		return hostShell()
	}
	return 0
}
