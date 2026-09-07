package portfwd

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

type PortBind struct {
	Port     uint16
	BindAddr string
}

type Listener struct {
	Port     uint16
	BindAddr string
	Sandbox  runtime.SandboxRef
}

type execLister interface {
	List(ctx context.Context) ([]runtime.SandboxRef, error)
	Exec(ctx context.Context, ref runtime.SandboxRef, req runtime.ExecRequest) (runtime.ExecResult, error)
}

type Discoverer struct {
	RT execLister
}

func ParseNetstat(text string) []PortBind {
	var out []PortBind
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || fields[5] != "LISTEN" {
			continue
		}
		addr := fields[3]
		i := strings.LastIndex(addr, ":")
		if i < 0 {
			continue
		}
		p, err := strconv.ParseUint(addr[i+1:], 10, 16)
		if err != nil {
			continue
		}
		out = append(out, PortBind{Port: uint16(p), BindAddr: addr[:i]})
	}
	return out
}

func (d *Discoverer) DiscoverOne(ctx context.Context, ref runtime.SandboxRef) ([]Listener, error) {
	var stdout bytes.Buffer
	res, err := d.RT.Exec(ctx, ref, runtime.ExecRequest{
		Argv:   []string{"netstat", "-ltn"},
		Stdout: &stdout,
	})
	if err != nil {
		return nil, fmt.Errorf("sandbox %s (%s): exec: %w", ref.ID, ref.Name, err)
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("sandbox %s (%s): netstat exit %d", ref.ID, ref.Name, res.ExitCode)
	}
	binds := ParseNetstat(stdout.String())
	out := make([]Listener, len(binds))
	for i, b := range binds {
		out[i] = Listener{Port: b.Port, BindAddr: b.BindAddr, Sandbox: ref}
	}
	return out, nil
}

func (d *Discoverer) DiscoverAll(ctx context.Context) ([]Listener, error) {
	refs, err := d.RT.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sandboxes: %w", err)
	}
	var all []Listener
	for _, ref := range refs {
		if ref.Status != runtime.SandboxStatusRunning {
			continue
		}
		ls, err := d.DiscoverOne(ctx, ref)
		if err != nil {
			return nil, err
		}
		all = append(all, ls...)
	}
	return all, nil
}
