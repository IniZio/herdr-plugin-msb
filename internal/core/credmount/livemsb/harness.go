package livemsb

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

const msbBin = "msb"

type BindMount struct {
	HostPath  string
	GuestPath string
	ReadOnly  bool
}

type SandboxOpts struct {
	Name       string
	Image      string
	MemoryMiB  int
	VCPUs      int
	FileMounts []BindMount
	DirMounts  []BindMount
	Env        map[string]string
}

type Sandbox struct {
	name string
}

func Available() error {
	_, err := exec.Command(msbBin, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("msb unavailable: %w", err)
	}
	return nil
}

func Create(ctx context.Context, o SandboxOpts) (*Sandbox, error) {
	if os.Getenv("HERDR_MSB_LIVE") != "1" {
		return nil, fmt.Errorf("HERDR_MSB_LIVE not set")
	}
	args := []string{"create", "--replace", "-q", "-n", o.Name}
	if o.MemoryMiB > 0 {
		args = append(args, "-m", strconv.Itoa(o.MemoryMiB)+"M")
	}
	if o.VCPUs > 0 {
		args = append(args, "-c", strconv.Itoa(o.VCPUs))
	}
	for _, m := range o.FileMounts {
		args = append(args, "--mount-file", m.HostPath+":"+m.GuestPath)
	}
	for _, m := range o.DirMounts {
		args = append(args, "--mount-dir", m.HostPath+":"+m.GuestPath)
	}
	for k, v := range o.Env {
		args = append(args, "-e", k+"="+v)
	}
	if o.Image != "" {
		args = append(args, o.Image)
	}
	cmd := exec.CommandContext(ctx, msbBin, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("msb create: %w\n%s", err, out)
	}
	s := &Sandbox{name: o.Name}
	if err := s.waitReady(ctx); err != nil {
		_ = s.Remove(ctx)
		return nil, err
	}
	return s, nil
}

func (s *Sandbox) waitReady(ctx context.Context) error {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.CommandContext(ctx, msbBin, "ping", "-q", s.name).CombinedOutput()
		if err == nil {
			return nil
		}
		_ = out
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("sandbox %q not ready after 90s", s.name)
}

func (s *Sandbox) Exec(ctx context.Context, argv ...string) (stdout, stderr string, exitCode int, err error) {
	args := append([]string{"exec", "--no-tty", s.name, "--"}, argv...)
	cmd := exec.CommandContext(ctx, msbBin, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	runErr := cmd.Run()
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			return outBuf.String(), errBuf.String(), ee.ExitCode(), nil
		}
		return "", "", -1, runErr
	}
	return outBuf.String(), errBuf.String(), 0, nil
}

func (s *Sandbox) Sh(ctx context.Context, script string) (stdout, stderr string, exitCode int, err error) {
	return s.Exec(ctx, "/bin/sh", "-c", script)
}

func (s *Sandbox) Remove(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, msbBin, "remove", "-f", "-q", s.name).CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.ToLower(string(out))
	if strings.Contains(msg, "not found") || strings.Contains(msg, "no sandbox") {
		return nil
	}
	return fmt.Errorf("msb remove: %w\n%s", err, out)
}

func ListNames(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, msbBin, "list").Output()
	if err != nil {
		return nil, fmt.Errorf("msb list: %w", err)
	}
	var names []string
	for i, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if i == 0 || strings.HasPrefix(line, "No sandboxes") {
			continue
		}
		if fields := strings.Fields(line); len(fields) > 0 {
			names = append(names, fields[0])
		}
	}
	return names, nil
}

func RequireSandbox(t *testing.T, o SandboxOpts) *Sandbox {
	t.Helper()
	if err := Available(); err != nil {
		t.Skip("msb not available:", err)
	}
	if os.Getenv("HERDR_MSB_LIVE") != "1" {
		t.Skip("HERDR_MSB_LIVE not set")
	}
	if !strings.HasPrefix(o.Name, "s16-") {
		t.Fatalf("sandbox name must start with s16-, got %q", o.Name)
	}
	if o.MemoryMiB > 2048 {
		t.Fatalf("MemoryMiB %d exceeds 2048 cap", o.MemoryMiB)
	}
	sb, err := Create(t.Context(), o)
	if err != nil {
		t.Fatalf("create sandbox: %v", err)
	}
	t.Cleanup(func() {
		if err := sb.Remove(context.Background()); err != nil {
			t.Logf("cleanup remove %q: %v", sb.name, err)
		}
	})
	return sb
}
