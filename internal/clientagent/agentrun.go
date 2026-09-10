package clientagent

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/IniZio/herdr-plugin-msb/internal/core/portfwd"
)

func RunLocalAgent(ctx context.Context, args []string, _ io.Writer, errW io.Writer) int {
	fs := flag.NewFlagSet("local-agent", flag.ContinueOnError)
	fs.SetOutput(errW)
	target := fs.String("target", "", "ssh target (user@host)")
	ctl := fs.String("control-path", "", "ssh ControlPath socket")
	host := fs.String("host", "", "hostname filter")
	pollS := fs.String("poll", "", "poll interval (e.g. 5s)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *target == "" {
		fmt.Fprintln(errW, "clientagent: --target is required")
		return 2
	}
	if *ctl == "" {
		dir, err := StateDir(os.Getenv)
		if err != nil {
			fmt.Fprintln(errW, err)
			return 1
		}
		*ctl = ControlPathFor(dir, *target)
	}
	if err := CheckControlPath(*ctl); err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}
	poll := DefaultPoll
	if *pollS != "" {
		if d, e := time.ParseDuration(*pollS); e == nil {
			poll = d
		}
	}
	a := &Agent{
		Target:      *target,
		ControlPath: *ctl,
		HostName:    *host,
		Poll:        poll,
		Run:         portfwd.OSRunner,
	}
	if err := a.Serve(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(errW, err)
		return 1
	}
	return 0
}

const agentUsage = "usage: herdr-plugin-msb-agent <command>\n\ncommands: local-agent-startup  local-agent"

func AgentRun(ctx context.Context, argv []string, stdout, stderr io.Writer) int {
	if len(argv) == 0 {
		fmt.Fprintln(stderr, agentUsage)
		return 2
	}
	switch argv[0] {
	case "local-agent-startup":
		return RunLocalAgentStartup(ctx, argv[1:], stdout, stderr)
	case "local-agent":
		return RunLocalAgent(ctx, argv[1:], stdout, stderr)
	case "help", "--help", "-h":
		fmt.Fprintln(stdout, agentUsage)
		return 0
	default:
		fmt.Fprintf(stderr, "herdr-plugin-msb-agent: unknown command %q\n", argv[0])
		return 2
	}
}
