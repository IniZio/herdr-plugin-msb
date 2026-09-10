//go:build linux

package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
)

const combinedUsage = `usage: herdr-plugin-msb <command>

sandbox:  create  ps  exec  start  stop  rm
plugin:   declare  status  list  local-agent  local-agent-startup  fwd-sync  attach  wrap-herdr  ports-pane  ports-toggle
space:    space-create  space-convert  space-open-pane  new-tab  space-prune
other:    default-shell  version  help`

const GuestShellArgv0 = "herdr-plugin-msb-guest-shell"

func NormalizeArgv(argv0 string, args []string) []string {
	if filepath.Base(argv0) == GuestShellArgv0 {
		return append([]string{"default-shell"}, args...)
	}
	return args
}

func Run(ctx context.Context, argv []string, stdout, stderr io.Writer) int {
	if len(argv) == 0 {
		fmt.Fprintln(stderr, combinedUsage)
		return 2
	}
	switch argv[0] {
	case "help", "--help", "-h":
		fmt.Fprintln(stdout, combinedUsage)
		return 0
	case "declare", "status", "list", "local-agent", "fwd-sync":
		return RunHerdrPlugin(ctx, argv, stdout, stderr)
	case "local-agent-startup":
		return RunLocalAgentStartup(ctx, argv[1:], stdout, stderr)
	case "attach":
		return RunAttach(ctx, argv[1:], stdout, stderr)
	case "wrap-herdr":
		return RunWrapHerdr(ctx, argv[1:], stdout, stderr)
	case "create":
		return runCreate(ctx, argv[1:], stdout, stderr)
	case "ps":
		return runPS(ctx, argv[1:], stdout, stderr)
	case "exec":
		return runExec(ctx, argv[1:], stdout, stderr)
	case "start":
		return runStart(ctx, argv[1:], stdout, stderr)
	case "stop":
		return runStop(ctx, argv[1:], stdout, stderr)
	case "rm":
		return runRM(ctx, argv[1:], stdout, stderr)
	case "space-create":
		return runSpaceCreate(ctx, argv[1:], stdout, stderr)
	case "space-convert":
		return runSpaceConvert(ctx, argv[1:], stdout, stderr)
	case "space-open-pane":
		return runSpaceOpenPane(ctx, argv[1:], stdout, stderr)
	case "new-tab":
		return runNewTab(ctx, argv[1:], stdout, stderr)
	case "default-shell":
		return runDefaultShell(ctx, argv[1:], stdout, stderr)
	case "space-prune":
		return runSpacePrune(ctx, argv[1:], stdout, stderr)
	case "ports-pane":
		return runPortsPane(ctx, argv[1:], stdout, stderr)
	case "ports-toggle":
		return runPortsToggle(ctx, argv[1:], stdout, stderr)
	case "version":
		return runVersion(ctx, argv[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "herdr-plugin-msb: unknown command %q\n\n%s\n", argv[0], combinedUsage)
		return 2
	}
}
