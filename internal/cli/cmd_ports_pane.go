//go:build linux

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	PFStatusIdle     = "idle"
	PFStatusLive     = "live"
	PFStatusPending  = "pending"
	PFStatusDead     = "dead"
	PFStatusError    = "error"
	PFStatusExpired  = "expired"
	PFStatusOutRange = "out_of_range"
)

const ForwardsStateFile = "forwards.state"
const paneStaleThreshold = 5 * time.Minute

const paneSep = "─────────────────────────────────────────────"

type ForwardsState struct {
	WrittenBy string        `json:"written_by"`
	UpdatedAt time.Time     `json:"updated_at"`
	Forwards  []PortForward `json:"forwards"`
}

type PortForward struct {
	Port        uint16    `json:"port"`
	Sandbox     string    `json:"sandbox"`
	Status      string    `json:"status"`
	ConfirmedAt time.Time `json:"confirmed_at,omitempty"`
	Error       string    `json:"error,omitempty"`
}

type PaneKey byte

const (
	KeyJ     PaneKey = 'j'
	KeyK     PaneKey = 'k'
	KeyEnter PaneKey = '\r'
	KeyR     PaneKey = 'r'
	KeyQ     PaneKey = 'q'
)

type PaneDispatchResult struct {
	MoveDelta    int
	Close        bool
	EnqueuePort  uint16
	ShowRecreate bool
}

func LoadForwardsState(dir string) (*ForwardsState, bool, error) {
	b, err := os.ReadFile(filepath.Join(dir, ForwardsStateFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var s ForwardsState
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, false, err
	}
	return &s, true, nil
}

func RenderPortsPane(state *ForwardsState, cursor int, now time.Time) string {
	if state == nil {
		return "Port forwards — (no sandbox)\n" +
			paneSep + "\n" +
			"  laptop agent not connected\n" +
			"  forwards.state not found\n\n" +
			"  Run: herdr-plugin-msb attach <target>\n" +
			"  Setup: doc/portfwd-operator-guide.md §1\n\n" +
			"  q  close pane\n"
	}

	var sb strings.Builder
	sandboxName := "none"
	if len(state.Forwards) > 0 {
		sandboxName = state.Forwards[0].Sandbox
	}
	sb.WriteString("Port forwards — " + sandboxName + "\n")
	sb.WriteString(paneSep + "\n")

	if len(state.Forwards) == 0 {
		sb.WriteString("  (no ports declared)\n\n")
		sb.WriteString("  agent connected " + state.UpdatedAt.Format("15:04:05") + "\n\n")
		sb.WriteString("  j/k  select   r  add port (recreates)   q  close\n")
		return sb.String()
	}

	for i, fwd := range state.Forwards {
		prefix := "  "
		if cursor == i {
			prefix = "> "
		}
		sb.WriteString(prefix + renderRow(fwd) + "\n")
	}

	if now.Sub(state.UpdatedAt) > paneStaleThreshold {
		mins := int(now.Sub(state.UpdatedAt).Minutes())
		sb.WriteString(fmt.Sprintf("  WARNING: state is %dm old — laptop agent may be down\n", mins))
	}

	sb.WriteString("\n")
	curStatus := ""
	if cursor >= 0 && cursor < len(state.Forwards) {
		curStatus = state.Forwards[cursor].Status
	}
	sb.WriteString("  " + paneFooter(curStatus) + "\n")
	return sb.String()
}

func renderRow(fwd PortForward) string {
	p := fmt.Sprintf("%d", fwd.Port)
	switch fwd.Status {
	case PFStatusLive:
		ts := fwd.ConfirmedAt.Format("15:04:05")
		return fmt.Sprintf("%s   LIVE    since %s   ctrl+click → http://127.0.0.1:%d", p, ts, fwd.Port)
	case PFStatusPending:
		return fmt.Sprintf("%s   PENDING request enqueued, waiting for laptop agent...", p)
	case PFStatusDead:
		ts := fwd.ConfirmedAt.Format("15:04:05")
		return fmt.Sprintf("%s   DEAD    lost %s   error: %s", p, ts, fwd.Error)
	case PFStatusError:
		return fmt.Sprintf("%s   ERROR   %s", p, fwd.Error)
	case PFStatusExpired:
		return fmt.Sprintf("%s   EXPIRED %s", p, fwd.Error)
	case PFStatusOutRange:
		return fmt.Sprintf("%s   OUT-OF-RANGE  recreate required (outside 1024-11023)", p)
	default:
		return fmt.Sprintf("%s   IDLE", p)
	}
}

func paneFooter(status string) string {
	switch status {
	case PFStatusLive:
		return "j/k  select   Enter  enqueue   r  add port (recreates)   q  close"
	case PFStatusDead:
		return "j/k  select   Enter  retry   r  add port (recreates)   q  close"
	case PFStatusPending:
		return "j/k  select   q  close"
	case PFStatusError:
		return "j/k  select   q  close"
	case PFStatusExpired:
		return "q  close"
	case PFStatusOutRange:
		return "j/k  select   r  add port (recreates)   q  close"
	case "":
		return "r  add port (recreates)   q  close"
	default:
		return "j/k  select   Enter  forward selected   r  add port (recreates)   q  close"
	}
}

func DispatchPaneKey(key PaneKey, rows []PortForward, cursor int) PaneDispatchResult {
	switch key {
	case KeyJ:
		return PaneDispatchResult{MoveDelta: +1}
	case KeyK:
		return PaneDispatchResult{MoveDelta: -1}
	case KeyQ:
		return PaneDispatchResult{Close: true}
	case KeyR:
		return PaneDispatchResult{ShowRecreate: true}
	case KeyEnter:
		if cursor < 0 || cursor >= len(rows) {
			return PaneDispatchResult{}
		}
		row := rows[cursor]
		switch row.Status {
		case PFStatusPending, PFStatusError, PFStatusExpired:
			return PaneDispatchResult{}
		default:
			return PaneDispatchResult{EnqueuePort: row.Port}
		}
	}
	return PaneDispatchResult{}
}

func EnqueueForwardRequest(stateDir string, port uint16, now time.Time) error {
	q, err := LoadQueue(stateDir)
	if err != nil {
		return err
	}
	acked, err := ReadAck(stateDir)
	if err != nil {
		return err
	}
	q.Prune(acked, now, DefaultTTL)
	r := Request{
		LocalPort:     port,
		RemotePort:    port,
		RemoteBind:    "127.0.0.1",
		Origin:        "ports-pane",
		PluginVersion: PluginVersion,
		CreatedUnixMS: uint64(now.UnixMilli()),
	}
	q.Enqueue(r)
	return WriteQueueAtomic(stateDir, q)
}

const recreateWarning = `! Adding a port requires recreating this sandbox.

  ALL IN-GUEST STATE WILL BE DESTROYED.
  Running processes, open files, and in-memory state will be lost.
  Your current session ends. (~350ms downtime.)

  Press Enter to confirm recreation, or q to cancel.
`

func runPortsPane(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	dir, err := StateDir(os.Getenv)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	state, _, _ := LoadForwardsState(dir)
	cursor := 0

	if !isTerminal(0) {
		fmt.Fprint(stdout, "\x1b[2J\x1b[H")
		fmt.Fprint(stdout, RenderPortsPane(state, cursor, time.Now()))
		return 0
	}

	_, cleanup, _ := enterRawMode(0)
	defer cleanup()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	stdinCh := make(chan byte, 32)
	go func() {
		buf := [1]byte{}
		for {
			n, readErr := os.Stdin.Read(buf[:])
			if n > 0 {
				select {
				case stdinCh <- buf[0]:
				default:
				}
			}
			if readErr != nil {
				return
			}
		}
	}()

	render := func() {
		state, _, _ = LoadForwardsState(dir)
		fmt.Fprint(stdout, "\x1b[2J\x1b[H")
		fmt.Fprint(stdout, RenderPortsPane(state, cursor, time.Now()))
	}
	render()

	for {
		select {
		case <-ctx.Done():
			return 0
		case <-ticker.C:
			render()
		case b := <-stdinCh:
			var rows []PortForward
			if state != nil {
				rows = state.Forwards
			}
			res := DispatchPaneKey(PaneKey(b), rows, cursor)
			if res.Close {
				return 0
			}
			if res.MoveDelta != 0 {
				cursor += res.MoveDelta
				if cursor < 0 {
					cursor = 0
				}
				if len(rows) > 0 && cursor >= len(rows) {
					cursor = len(rows) - 1
				}
				render()
				continue
			}
			if res.EnqueuePort > 0 {
				_ = EnqueueForwardRequest(dir, res.EnqueuePort, time.Now())
				render()
				continue
			}
			if res.ShowRecreate {
				fmt.Fprint(stdout, "\x1b[2J\x1b[H")
				fmt.Fprint(stdout, recreateWarning)
				select {
				case <-ctx.Done():
					return 0
				case next := <-stdinCh:
					if next == '\r' || next == '\n' {
						fmt.Fprintln(stdout, "recreation not yet implemented (Slice C)")
						return 0
					}
				}
				render()
			}
		}
	}
}
