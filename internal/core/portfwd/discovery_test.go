package portfwd

import (
	"context"
	"strings"
	"testing"

	"github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

const verbatimGuest = `Active Internet connections (only servers)
Proto Recv-Q Send-Q Local Address           Foreign Address         State
tcp        0      0 :::45455                :::*                    LISTEN`

type fakeRT struct {
	refs      []runtime.SandboxRef
	stdout    string
	execErr   error
	exitCode  int32
	execCalls int
	gotArgv   []string
}

func (f *fakeRT) List(_ context.Context) ([]runtime.SandboxRef, error) {
	return f.refs, nil
}

func (f *fakeRT) Exec(_ context.Context, _ runtime.SandboxRef, req runtime.ExecRequest) (runtime.ExecResult, error) {
	f.execCalls++
	f.gotArgv = req.Argv
	if req.Stdout != nil {
		_, _ = req.Stdout.Write([]byte(f.stdout))
	}
	return runtime.ExecResult{ExitCode: f.exitCode}, f.execErr
}

func TestParseNetstat_Verbatim(t *testing.T) {
	binds := ParseNetstat(verbatimGuest)
	if len(binds) != 1 {
		t.Fatalf("want 1 bind, got %d", len(binds))
	}
	if binds[0].Port != 45455 {
		t.Errorf("port: want 45455 got %d", binds[0].Port)
	}
	if binds[0].BindAddr != "::" {
		t.Errorf("bindaddr: want :: got %q", binds[0].BindAddr)
	}
}

func TestParseNetstat_Forms(t *testing.T) {
	cases := []struct {
		input    string
		wantPort uint16
		wantAddr string
	}{
		{"tcp  0  0  0.0.0.0:8080  0.0.0.0:*  LISTEN", 8080, "0.0.0.0"},
		{"tcp  0  0  127.0.0.1:5173  0.0.0.0:*  LISTEN", 5173, "127.0.0.1"},
		{"tcp  0  0  ::1:9000  :::*  LISTEN", 9000, "::1"},
	}
	for _, c := range cases {
		binds := ParseNetstat(c.input)
		if len(binds) != 1 {
			t.Errorf("input %q: want 1 bind got %d", c.input, len(binds))
			continue
		}
		if binds[0].Port != c.wantPort {
			t.Errorf("input %q: port want %d got %d", c.input, c.wantPort, binds[0].Port)
		}
		if binds[0].BindAddr != c.wantAddr {
			t.Errorf("input %q: addr want %q got %q", c.input, c.wantAddr, binds[0].BindAddr)
		}
	}
}

func TestParseNetstat_IgnoresMalformedAndHeaders(t *testing.T) {
	input := `Active Internet connections (only servers)
Proto Recv-Q Send-Q Local Address           Foreign Address         State
not enough
tcp  0  0  badaddr  0.0.0.0:*  LISTEN
tcp  0  0  0.0.0.0:9999  0.0.0.0:*  ESTABLISHED`
	binds := ParseNetstat(input)
	if len(binds) != 0 {
		t.Errorf("want 0 binds, got %d", len(binds))
	}
}

func TestParseNetstat_Empty(t *testing.T) {
	if len(ParseNetstat("")) != 0 {
		t.Error("want 0 binds for empty input")
	}
}

func TestDiscoverOne_IdentityAndArgv(t *testing.T) {
	ref := runtime.SandboxRef{ID: "abc123", Project: "proj", Name: "mybox", Status: runtime.SandboxStatusRunning}
	rt := &fakeRT{stdout: verbatimGuest}
	ls, err := (&Discoverer{RT: rt}).DiscoverOne(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) != 1 {
		t.Fatalf("want 1 listener, got %d", len(ls))
	}
	if ls[0].Sandbox != ref {
		t.Errorf("identity: got %+v want %+v", ls[0].Sandbox, ref)
	}
	if len(rt.gotArgv) < 2 || rt.gotArgv[0] != "netstat" || rt.gotArgv[1] != "-ltn" {
		t.Errorf("argv: want [netstat -ltn] got %v", rt.gotArgv)
	}
}

func TestDiscoverAll_SkipsNonRunning(t *testing.T) {
	refs := []runtime.SandboxRef{
		{ID: "1", Name: "stopped", Status: runtime.SandboxStatusStopped},
		{ID: "2", Name: "created", Status: runtime.SandboxStatusCreated},
		{ID: "3", Name: "paused", Status: runtime.SandboxStatusPaused},
	}
	rt := &fakeRT{refs: refs}
	ls, err := (&Discoverer{RT: rt}).DiscoverAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) != 0 {
		t.Errorf("want 0 listeners, got %d", len(ls))
	}
	if rt.execCalls != 0 {
		t.Errorf("want 0 Exec calls for non-running sandboxes, got %d", rt.execCalls)
	}
}

func TestDiscoverOne_NonZeroExitIsError(t *testing.T) {
	ref := runtime.SandboxRef{ID: "x1", Name: "failing", Status: runtime.SandboxStatusRunning}
	rt := &fakeRT{exitCode: 1}
	_, err := (&Discoverer{RT: rt}).DiscoverOne(context.Background(), ref)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "x1") || !strings.Contains(err.Error(), "failing") {
		t.Errorf("error must name sandbox ID and Name, got: %v", err)
	}
}
