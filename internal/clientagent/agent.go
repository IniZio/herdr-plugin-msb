package clientagent

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/IniZio/herdr-plugin-msb/internal/core/portfwd"
)

const DefaultPoll = 5 * time.Second

type Agent struct {
	Target      string
	ControlPath string
	HostName    string
	Poll        time.Duration
	TTL         time.Duration
	Run         portfwd.Runner

	applied   map[uint64]struct{}
	specs     map[uint64]string
	lastAcked uint64
	seeded    bool
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
			return fmt.Errorf("clientagent: ssh master: exit %d %v: %s", code, masterArgv, s)
		}
		return fmt.Errorf("clientagent: ssh master: exit %d %v", code, masterArgv)
	}
	return nil
}

func (a *Agent) Tick(ctx context.Context, now time.Time) (applied []uint64, settled uint64, err error) {
	if a.applied == nil {
		a.applied = make(map[uint64]struct{})
		a.specs = make(map[uint64]string)
	}
	qOut, qStderr, code, runErr := a.Run(ctx, ExecArgv(a.Target, a.ControlPath, RemoteReadCommand))
	if runErr != nil {
		return nil, 0, fmt.Errorf("clientagent: remote read: %w", runErr)
	}
	if code != 0 {
		if s := strings.TrimSpace(qStderr); s != "" {
			return nil, 0, fmt.Errorf("clientagent: remote read: exit %d: %s", code, s)
		}
		return nil, 0, fmt.Errorf("clientagent: remote read: exit %d", code)
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
				fwdRun = fmt.Errorf("clientagent: ssh -O forward: exit %d: %s", fwdCode, strings.TrimSpace(fwdErr))
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

func (a *Agent) CancelApplied(ctx context.Context) error {
	var firstErr error
	for id := range a.applied {
		spec, ok := a.specs[id]
		if !ok {
			continue
		}
		if _, _, code, err := a.Run(ctx, CancelArgv(a.Target, a.ControlPath, spec)); err != nil || code != 0 {
			if firstErr == nil {
				firstErr = fmt.Errorf("clientagent: cancel %s: exit %d: %v", spec, code, err)
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
