package clientagent

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

const agentUsage = "usage: herdr-plugin-msb-agent <command>\n\ncommands: local-agent-startup  local-agent  provision"

func RunProvision(ctx context.Context, args []string, stdin io.Reader, stdout, errW io.Writer) int {
	fs := flag.NewFlagSet("provision", flag.ContinueOnError)
	fs.SetOutput(errW)
	target := fs.String("target", "", "SSH target to provision (user@host)")
	revoke := fs.Bool("revoke", false, "revoke consent for --target")
	list := fs.Bool("list", false, "list consent directory")
	binFlag := fs.String("bin", "", "local Linux binary path (default: $HERDR_PLUGIN_ROOT/herdr-plugin-msb)")
	tomlFlag := fs.String("toml", "", "local plugin.toml path (default: $HERDR_PLUGIN_ROOT/herdr-plugin.toml)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	stateDir, err := StateDir(os.Getenv)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}
	cs := &ConsentStore{Dir: stateDir}

	if *list {
		fmt.Fprintf(stdout, "consent records: %s/consent/\n", stateDir)
		return 0
	}
	if *target == "" {
		fmt.Fprintln(errW, "provision: --target is required")
		return 2
	}
	if *revoke {
		if err := cs.Revoke(*target); err != nil {
			fmt.Fprintln(errW, "provision revoke:", err)
			return 1
		}
		fmt.Fprintf(stdout, "consent revoked for %s\n", *target)
		return 0
	}

	pluginRoot := os.Getenv("HERDR_PLUGIN_ROOT")
	bin := *binFlag
	if bin == "" && pluginRoot != "" {
		bin = filepath.Join(pluginRoot, "herdr-plugin-msb")
	}
	toml := *tomlFlag
	if toml == "" && pluginRoot != "" {
		toml = filepath.Join(pluginRoot, "herdr-plugin.toml")
	}

	fmt.Fprintf(stdout, "Provision %s:\n", *target)
	fmt.Fprintf(stdout, "  binary: %s → %s:~/.local/bin/herdr-plugin-msb\n", bin, *target)
	fmt.Fprintf(stdout, "  plugin: %s → %s:~/%s/herdr-plugin.toml\n", toml, *target, remotePluginDir)
	fmt.Fprintf(stdout, "  then: herdr plugin link ~/%s on %s\n", remotePluginDir, *target)
	fmt.Fprintf(stdout, "Consent to write to %s? [y/N]: ", *target)

	sc := bufio.NewScanner(stdin)
	if !sc.Scan() {
		fmt.Fprintln(errW, "provision: no input")
		return 1
	}
	if strings.TrimSpace(strings.ToLower(sc.Text())) != "y" {
		fmt.Fprintln(stdout, "Aborted.")
		return 0
	}

	rec := ConsentRecord{Target: *target, ConsentedAt: time.Now().UTC(), Version: PluginVersion}
	if err := cs.Save(rec); err != nil {
		fmt.Fprintln(errW, "provision: save consent:", err)
		return 1
	}
	p := &Provisioner{
		Target:       *target,
		BinSrc:       bin,
		PluginTOML:   toml,
		LocalVersion: PluginVersion,
		CheckConsent: cs.CheckFn,
		Run:          portfwd.OSRunner,
		Copy:         SCPCopy,
		Stderr:       errW,
	}
	if err := p.EnsureProvisioned(ctx); err != nil {
		fmt.Fprintln(errW, "provision:", err)
		return 1
	}
	fmt.Fprintf(stdout, "provisioned %s at version %s\n", *target, PluginVersion)
	return 0
}

func AgentRun(ctx context.Context, argv []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(argv) == 0 {
		fmt.Fprintln(stderr, agentUsage)
		return 2
	}
	switch argv[0] {
	case "local-agent-startup":
		return RunLocalAgentStartup(ctx, argv[1:], stdout, stderr)
	case "local-agent":
		return RunLocalAgent(ctx, argv[1:], stdout, stderr)
	case "provision":
		return RunProvision(ctx, argv[1:], stdin, stdout, stderr)
	case "help", "--help", "-h":
		fmt.Fprintln(stdout, agentUsage)
		return 0
	default:
		fmt.Fprintf(stderr, "herdr-plugin-msb-agent: unknown command %q\n", argv[0])
		return 2
	}
}
