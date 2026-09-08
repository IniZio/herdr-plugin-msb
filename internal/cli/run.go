package cli

import (
	"context"
	"fmt"
	"io"
)

const combinedUsage = `usage: herdr-plugin-msb <command>

sandbox:  create  ps  exec  start  stop  rm
plugin:   declare  status  list  local-agent
other:    version  help`

func Run(ctx context.Context, argv []string, stdout, stderr io.Writer) int {
	if len(argv) == 0 {
		fmt.Fprintln(stderr, combinedUsage)
		return 2
	}
	switch argv[0] {
	case "help", "--help", "-h":
		fmt.Fprintln(stdout, combinedUsage)
		return 0
	case "declare", "status", "list", "local-agent":
		return RunHerdrPlugin(ctx, argv, stdout, stderr)
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
	case "version":
		return runVersion(ctx, argv[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "herdr-plugin-msb: unknown command %q\n\n%s\n", argv[0], combinedUsage)
		return 2
	}
}
