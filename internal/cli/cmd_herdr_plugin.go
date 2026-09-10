//go:build linux

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/IniZio/herdr-plugin-msb/internal/core/portfwd"
	"github.com/IniZio/herdr-plugin-msb/internal/core/service"
	"github.com/IniZio/herdr-plugin-msb/internal/runtime/msb"
)

var ErrUnimplemented = errors.New("cli: unimplemented")

// Declarer is the sandbox-host half: it turns discovered guest listeners into
// queue entries that a local agent will pick up over its own ssh master.
type Declarer struct {
	Dir           string
	Host          string
	PluginVersion string
	TTL           time.Duration
}

func (d *Declarer) Declare(listeners []portfwd.Listener, now time.Time) ([]Request, error) {
	ttl := d.TTL
	if ttl == 0 {
		ttl = DefaultTTL
	}
	q, err := LoadQueue(d.Dir)
	if err != nil {
		return nil, err
	}
	acked, err := ReadAck(d.Dir)
	if err != nil {
		return nil, err
	}
	q.Prune(acked, now, ttl)
	pending := make(map[uint16]struct{}, len(q.Pending))
	for _, r := range q.Pending {
		pending[r.LocalPort] = struct{}{}
	}
	var added []Request
	for _, l := range listeners {
		if _, ok := pending[l.Port]; ok {
			continue
		}
		r := q.Enqueue(Request{
			Host:          d.Host,
			RemotePort:    l.Port,
			LocalPort:     l.Port,
			RemoteBind:    "127.0.0.1",
			Origin:        "discovery",
			PluginVersion: d.PluginVersion,
			CreatedUnixMS: uint64(now.UnixMilli()),
		})
		added = append(added, r)
	}
	if err := WriteQueueAtomic(d.Dir, q); err != nil {
		return nil, err
	}
	return added, nil
}

// Notifier reaches the operator. Action stdout/stderr land only in
// `herdr plugin log list`, so a pane is the sole operator-visible channel.
type Notifier struct {
	Run    Runner
	HerdrB string
}

func PaneOpenArgv(herdrBin, title string, body []string) []string {
	return []string{
		herdrBin, "plugin", "pane", "open",
		"--plugin", PluginID,
		"--entrypoint", "notify",
		"--env", "HERDR_NOTIFY_TITLE=" + title,
		"--env", "HERDR_NOTIFY_BODY=" + strings.Join(body, "\n"),
	}
}

func (n *Notifier) Notify(ctx context.Context, title string, body []string) error {
	bin := n.HerdrB
	if bin == "" {
		bin = "herdr"
	}
	out, errOut, code, err := n.Run(ctx, PaneOpenArgv(bin, title, body))
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("cli: herdr plugin pane open: exit %d: %s", code, strings.TrimSpace(errOut))
	}
	var resp struct {
		Type string `json:"type"`
	}
	if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(out)), &resp); jsonErr != nil {
		return fmt.Errorf("cli: herdr plugin pane open: parse response: %w", jsonErr)
	}
	if resp.Type != "plugin_pane_opened" {
		return fmt.Errorf("cli: herdr plugin pane open: unexpected type %q", resp.Type)
	}
	return nil
}

func writeAppliedState(ctx context.Context, stateDir string, fw *portfwd.Forwarder, mgr *portfwd.Manager) {
	entries := mgr.Applied()
	fwds := make([]PortForward, 0, len(entries))
	for _, e := range entries {
		pf := PortForward{Port: e.Port, Sandbox: e.SandboxID, Status: PFStatusPending}
		if ok, _ := fw.Present(ctx, e.Port); ok {
			pf.Status = PFStatusLive
			pf.ConfirmedAt = time.Now()
		}
		fwds = append(fwds, pf)
	}
	sort.Slice(fwds, func(i, j int) bool { return fwds[i].Port < fwds[j].Port })
	s := &ForwardsState{WrittenBy: "fwd-sync", UpdatedAt: time.Now(), Forwards: fwds}
	_ = WriteForwardsStateAtomic(stateDir, s)
}

func runFwdSync(ctx context.Context, args []string, out, errW io.Writer) int {
	fs := flag.NewFlagSet("fwd-sync", flag.ContinueOnError)
	fs.SetOutput(errW)
	ctl := fs.String("control-path", "", "ssh ControlPath socket")
	sshHost := fs.String("ssh-host", "", "ssh target (user@host)")
	teardownName := fs.String("teardown", "", "sandbox name whose forwards to remove")
	project := fs.String("project", service.DefaultProject, "project name")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *ctl == "" || *sshHost == "" {
		fmt.Fprintln(errW, "fwd-sync: --control-path and --ssh-host are required")
		return 2
	}
	stateDir, stateDirErr := StateDir(os.Getenv)
	fw := &portfwd.Forwarder{
		ControlPath: *ctl,
		SSHHost:     *sshHost,
		Run:         portfwd.OSRunner,
	}
	mgr := portfwd.NewManager(fw)
	rt := msb.New()
	if *teardownName != "" {
		svc := service.New(rt, *project)
		ref, err := svc.Resolve(ctx, *teardownName)
		if err != nil {
			fmt.Fprintln(errW, err)
			return 1
		}
		if err := mgr.TeardownSandbox(ctx, ref); err != nil {
			fmt.Fprintln(errW, err)
			return 1
		}
		if stateDirErr == nil {
			writeAppliedState(ctx, stateDir, fw, mgr)
		}
		fmt.Fprintf(out, "removed forwards for %s\n", *teardownName)
		return 0
	}
	d := &portfwd.Discoverer{RT: rt}
	desired, err := d.DiscoverAll(ctx)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}
	if err := mgr.Reconcile(ctx, desired); err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}
	if stateDirErr == nil {
		writeAppliedState(ctx, stateDir, fw, mgr)
	}
	fmt.Fprintf(out, "synced %d listener(s)\n", len(desired))
	return 0
}

const pluginUsage = "usage: herdr-plugin-msb <command>\n\ncommands: declare  status  list  local-agent  fwd-sync  attach  wrap-herdr  help"

func RunHerdrPlugin(ctx context.Context, argv []string, stdout, stderr io.Writer) int {
	if len(argv) == 0 {
		fmt.Fprintln(stderr, pluginUsage)
		return 2
	}
	switch argv[0] {
	case "declare":
		return runDeclare(ctx, argv[1:], stdout, stderr)
	case "status":
		return runStatus(ctx, argv[1:], stdout, stderr)
	case "list":
		return runList(ctx, argv[1:], stdout, stderr)
	case "local-agent":
		return runLocalAgent(ctx, argv[1:], stdout, stderr)
	case "fwd-sync":
		return runFwdSync(ctx, argv[1:], stdout, stderr)
	case "help", "--help", "-h":
		fmt.Fprintln(stdout, pluginUsage)
		return 0
	default:
		fmt.Fprintf(stderr, "herdr-plugin-msb: unknown command %q\n\n%s\n", argv[0], pluginUsage)
		return 2
	}
}

func runDeclare(_ context.Context, args []string, out io.Writer, errW io.Writer) int {
	fs := flag.NewFlagSet("declare", flag.ContinueOnError)
	fs.SetOutput(errW)
	ports := fs.String("port", "", "comma-separated guest ports to declare")
	host := fs.String("host", "", "host the forward belongs to")
	notify := fs.Bool("notify", false, "report the result to the operator via herdr plugin pane open")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dir, err := StateDir(os.Getenv)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}
	if *ports == "" {
		q, err := LoadQueue(dir)
		if err != nil {
			fmt.Fprintln(errW, err)
			return 1
		}
		b, _ := json.Marshal(q)
		fmt.Fprintf(out, "%s\n", b)
		return 0
	}
	var listeners []portfwd.Listener
	for _, f := range strings.Split(*ports, ",") {
		n, e := strconv.ParseUint(strings.TrimSpace(f), 10, 16)
		if e != nil {
			fmt.Fprintf(errW, "cli: bad --port %q\n", f)
			return 2
		}
		listeners = append(listeners, portfwd.Listener{Port: uint16(n), BindAddr: "127.0.0.1"})
	}
	d := &Declarer{Dir: dir, Host: *host, PluginVersion: PluginVersion}
	added, err := d.Declare(listeners, time.Now())
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}
	b, _ := json.Marshal(added)
	fmt.Fprintf(out, "%s\n", b)
	if !*notify {
		return 0
	}
	body := make([]string, 0, len(added))
	for _, r := range added {
		body = append(body, fmt.Sprintf("http://127.0.0.1:%d  (%s:%d)", r.LocalPort, r.Host, r.RemotePort))
	}
	if len(body) == 0 {
		body = append(body, "no new ports to forward")
	}
	n := &Notifier{Run: portfwd.OSRunner, HerdrB: undeletedHerdrBin(os.Getenv("HERDR_BIN"))}
	if err := n.Notify(context.Background(), "microsandbox ports", body); err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}
	return 0
}

func runStatus(_ context.Context, _ []string, out io.Writer, errW io.Writer) int {
	dir, err := StateDir(os.Getenv)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}
	q, err := LoadQueue(dir)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}
	acked, _ := ReadAck(dir)
	fmt.Fprintf(out, "pending=%d acked=%d\n", len(q.Pending), acked)
	return 0
}

func runList(_ context.Context, _ []string, out io.Writer, errW io.Writer) int {
	dir, err := StateDir(os.Getenv)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}
	q, err := LoadQueue(dir)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}
	b, _ := json.Marshal(q.Pending)
	fmt.Fprintf(out, "%s\n", b)
	return 0
}

func runLocalAgent(ctx context.Context, args []string, _ io.Writer, errW io.Writer) int {
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
		fmt.Fprintln(errW, "cli: --target is required")
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
		Report:      func(s string) { fmt.Fprintln(errW, s) },
	}
	if err := a.Serve(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(errW, err)
		return 1
	}
	return 0
}
