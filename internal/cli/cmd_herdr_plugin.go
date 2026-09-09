package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/IniZio/herdr-plugin-msb/internal/core/portfwd"
)

const (
	PluginID      = "herdr-plugin-msb"
	StateDirNS    = "herdr-plugin-msb"
	PluginVersion = "0.1.0"

	QueueFile = "requests.json"
	AckFile   = "requests.acked"

	MaxControlPathLen = 103

	DefaultPoll = 5 * time.Second
	DefaultTTL  = 10 * time.Minute
)

var ErrUnimplemented = errors.New("cli: unimplemented")

type Runner = portfwd.Runner

type Request struct {
	ID            uint64 `json:"id"`
	Host          string `json:"host"`
	RemotePort    uint16 `json:"remote_port"`
	LocalPort     uint16 `json:"local_port"`
	RemoteBind    string `json:"remote_bind"`
	Origin        string `json:"origin"`
	PaneID        string `json:"pane_id,omitempty"`
	OpenURL       string `json:"open_url,omitempty"`
	PluginVersion string `json:"plugin_version"`
	CreatedUnixMS uint64 `json:"created_unix_ms"`
}

type Queue struct {
	Version        uint32    `json:"version"`
	LastConsumedID uint64    `json:"last_consumed_id"`
	Pending        []Request `json:"pending"`
}

// remoteStateScan resolves $p to the first candidate holding a queue file. It runs
// on the sandbox host under /bin/sh, so it must stay POSIX and single-line.
const remoteStateScan = `p=""; for d in "${XDG_STATE_HOME:-$HOME/.local/state}/` + StateDirNS + `" "${HERDR_PLUGIN_STATE_DIR:-}" "${XDG_STATE_HOME:-$HOME/.local/state}/herdr/plugins/` + PluginID + `"; do if [ -n "$d" ] && [ -f "$d/` + QueueFile + `" ]; then p="$d"; break; fi; done; `

const RemoteReadCommand = remoteStateScan + `if [ -n "$p" ]; then cat "$p/` + QueueFile + `"; fi`

const RemoteAckReadCommand = remoteStateScan + `if [ -n "$p" ]; then cat "$p/` + AckFile + `" 2>/dev/null; fi`

func RemoteMarkCommand(id uint64) string {
	n := strconv.FormatUint(id, 10)
	return remoteStateScan + `if [ -n "$p" ]; then o=$(cat "$p/` + AckFile + `" 2>/dev/null); case "$o" in ''|*[!0-9]*) o=0;; esac; if [ "$o" -ge ` + n + ` ] 2>/dev/null; then :; else t="$p/` + AckFile + `.$$"; printf '%s\n' ` + n + ` > "$t" && mv "$t" "$p/` + AckFile + `"; fi; fi`
}

func StateDir(env func(string) string) (string, error) {
	if v := env("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, StateDirNS), nil
	}
	home := env("HOME")
	if home == "" {
		return "", errors.New("cli: neither XDG_STATE_HOME nor HOME is set")
	}
	return filepath.Join(home, ".local", "state", StateDirNS), nil
}

func ControlPathFor(stateDir, host string) string {
	return filepath.Join(stateDir, host+".ctl")
}

func CheckControlPath(p string) error {
	if len(p) > MaxControlPathLen {
		return fmt.Errorf("cli: ControlPath %q is %d bytes, over the %d-byte sun_path limit", p, len(p), MaxControlPathLen)
	}
	return nil
}

func MasterArgv(target, controlPath string) []string {
	return []string{
		"ssh", "-M", "-N", "-f",
		"-o", "ControlPath=" + controlPath,
		"-o", "ControlMaster=auto",
		"-o", "ControlPersist=yes",
		"-o", "BatchMode=yes",
		"-o", "GatewayPorts=no",
		"-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		target,
	}
}

func ExecArgv(target, controlPath, command string) []string {
	return []string{"ssh", "-S", controlPath, "-o", "BatchMode=yes", target, command}
}

func ForwardArgv(target, controlPath, spec string) []string {
	return []string{"ssh", "-S", controlPath, "-O", "forward", "-L", spec, target}
}

func CancelArgv(target, controlPath, spec string) []string {
	return []string{"ssh", "-S", controlPath, "-O", "cancel", "-L", spec, target}
}

func ForwardSpec(r Request) string {
	return fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", r.LocalPort, r.RemotePort)
}

func LoadQueue(dir string) (*Queue, error) {
	b, err := os.ReadFile(filepath.Join(dir, QueueFile))
	if errors.Is(err, os.ErrNotExist) {
		return &Queue{Version: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	q := &Queue{}
	if err := json.Unmarshal(b, q); err != nil {
		return nil, fmt.Errorf("cli: parse %s: %w", QueueFile, err)
	}
	if q.Version == 0 {
		q.Version = 1
	}
	return q, nil
}

func WriteQueueAtomic(dir string, q *Queue) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(q)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, QueueFile+".tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, QueueFile))
}

func ReadAck(dir string) (uint64, error) {
	b, err := os.ReadFile(filepath.Join(dir, AckFile))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, nil
	}
	return n, nil
}

func (q *Queue) NextID() uint64 {
	max := q.LastConsumedID
	for _, r := range q.Pending {
		if r.ID > max {
			max = r.ID
		}
	}
	return max + 1
}

func (q *Queue) Enqueue(r Request) Request {
	if r.ID == 0 {
		r.ID = q.NextID()
	}
	q.Pending = append(q.Pending, r)
	sort.Slice(q.Pending, func(i, j int) bool { return q.Pending[i].ID < q.Pending[j].ID })
	return r
}

func (q *Queue) Prune(acked uint64, now time.Time, ttl time.Duration) {
	cutoff := uint64(now.Add(-ttl).UnixMilli())
	kept := q.Pending[:0]
	for _, r := range q.Pending {
		if r.ID <= acked || r.CreatedUnixMS < cutoff {
			continue
		}
		kept = append(kept, r)
	}
	q.Pending = kept
	if acked > q.LastConsumedID {
		q.LastConsumedID = acked
	}
}

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
	_, errOut, code, err := n.Run(ctx, PaneOpenArgv(bin, title, body))
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("cli: herdr plugin pane open: exit %d: %s", code, strings.TrimSpace(errOut))
	}
	return nil
}

type Agent struct {
	Target      string
	ControlPath string
	HostName    string
	Poll        time.Duration
	TTL         time.Duration
	Run         Runner

	applied   map[uint64]struct{}
	specs     map[uint64]string
	lastAcked uint64
	seeded    bool
}

func ParseQueueJSON(s string) (*Queue, error) {
	q := &Queue{}
	t := strings.TrimSpace(s)
	if t == "" {
		return &Queue{Version: 1}, nil
	}
	if err := json.Unmarshal([]byte(t), q); err != nil {
		return nil, fmt.Errorf("cli: parse remote queue: %w", err)
	}
	return q, nil
}

func (a *Agent) IsOurs(r Request) bool {
	return r.Host == "" || r.Host == a.HostName
}

func (a *Agent) EnsureMaster(ctx context.Context) error {
	if err := CheckControlPath(a.ControlPath); err != nil {
		return err
	}
	_, _, code, err := a.Run(ctx, []string{"ssh", "-S", a.ControlPath, "-O", "check", a.Target})
	if err != nil {
		return err
	}
	if code == 0 {
		return nil
	}
	if _, statErr := os.Stat(a.ControlPath); statErr == nil {
		_ = os.Remove(a.ControlPath)
	}
	masterArgv := MasterArgv(a.Target, a.ControlPath)
	_, masterStderr, code, err := a.Run(ctx, masterArgv)
	if err != nil {
		return err
	}
	if code != 0 {
		if s := strings.TrimSpace(masterStderr); s != "" {
			return fmt.Errorf("cli: ssh master: exit %d %v: %s", code, masterArgv, s)
		}
		return fmt.Errorf("cli: ssh master: exit %d %v", code, masterArgv)
	}
	return nil
}

// Tick performs one poll-apply-settle pass and returns the ids applied this pass
// and the highest id settled.
func (a *Agent) Tick(ctx context.Context, now time.Time) (applied []uint64, settled uint64, err error) {
	if a.applied == nil {
		a.applied = make(map[uint64]struct{})
		a.specs = make(map[uint64]string)
	}
	qOut, qStderr, code, runErr := a.Run(ctx, ExecArgv(a.Target, a.ControlPath, RemoteReadCommand))
	if runErr != nil {
		return nil, 0, fmt.Errorf("cli: remote read: %w", runErr)
	}
	if code != 0 {
		if s := strings.TrimSpace(qStderr); s != "" {
			return nil, 0, fmt.Errorf("cli: remote read: exit %d: %s", code, s)
		}
		return nil, 0, fmt.Errorf("cli: remote read: exit %d", code)
	}
	q, parseErr := ParseQueueJSON(qOut)
	if parseErr != nil {
		return nil, 0, parseErr
	}
	if !a.seeded {
		ackOut, _, _, _ := a.Run(ctx, ExecArgv(a.Target, a.ControlPath, RemoteAckReadCommand))
		if n, e := strconv.ParseUint(strings.TrimSpace(ackOut), 10, 64); e == nil {
			a.lastAcked = n
		}
		a.seeded = true
	}
	ttl := a.TTL
	if ttl == 0 {
		ttl = DefaultTTL
	}
	cutoffMS := uint64(now.Add(-ttl).UnixMilli())
	reqs := make([]Request, len(q.Pending))
	copy(reqs, q.Pending)
	sort.Slice(reqs, func(i, j int) bool { return reqs[i].ID < reqs[j].ID })
	cursor := a.lastAcked
	for _, r := range reqs {
		if r.ID <= a.lastAcked {
			continue
		}
		if !a.IsOurs(r) || r.CreatedUnixMS < cutoffMS {
			cursor = r.ID
			continue
		}
		if _, ok := a.applied[r.ID]; ok {
			cursor = r.ID
			continue
		}
		spec := ForwardSpec(r)
		_, fwdErr, fwdCode, fwdRun := a.Run(ctx, ForwardArgv(a.Target, a.ControlPath, spec))
		if fwdRun != nil || fwdCode != 0 {
			if fwdRun == nil {
				fwdRun = fmt.Errorf("cli: ssh -O forward: exit %d: %s", fwdCode, strings.TrimSpace(fwdErr))
			}
			err = fwdRun
			break
		}
		a.applied[r.ID] = struct{}{}
		a.specs[r.ID] = spec
		applied = append(applied, r.ID)
		cursor = r.ID
	}
	if cursor > a.lastAcked {
		_, _, _, _ = a.Run(ctx, ExecArgv(a.Target, a.ControlPath, RemoteMarkCommand(cursor)))
		a.lastAcked = cursor
	}
	return applied, cursor, err
}

// CancelApplied removes every forward this agent applied. The ssh master runs with
// ControlPersist=yes and outlives the agent, so a forward is NOT reclaimed by the
// agent exiting - only by this call, and a SIGKILLed agent leaks its forwards.
func (a *Agent) CancelApplied(ctx context.Context) error {
	var firstErr error
	for id := range a.applied {
		spec, ok := a.specs[id]
		if !ok {
			continue
		}
		if _, _, code, err := a.Run(ctx, CancelArgv(a.Target, a.ControlPath, spec)); err != nil || code != 0 {
			if firstErr == nil {
				firstErr = fmt.Errorf("cli: cancel %s: exit %d: %v", spec, code, err)
			}
			continue
		}
		delete(a.applied, id)
	}
	return firstErr
}

func (a *Agent) Serve(ctx context.Context) error {
	if err := a.EnsureMaster(ctx); err != nil {
		return err
	}
	defer func() { _ = a.CancelApplied(context.WithoutCancel(ctx)) }()
	poll := a.Poll
	if poll == 0 {
		poll = DefaultPoll
	}
	backoff := poll
	maxBackoff := 60 * time.Second
	consec := 0
	for {
		_, _, tickErr := a.Tick(ctx, time.Now())
		if tickErr != nil {
			consec++
			if consec > 1 {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
		} else {
			consec = 0
			backoff = poll
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
}

const pluginUsage = "usage: herdr-plugin-msb <command>\n\ncommands: declare  status  list  local-agent  help"

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
		*ctl = ControlPathFor(dir, *host)
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
