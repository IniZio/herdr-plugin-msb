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

const verbatimProcNet = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n   0: 00000000:B18F 00000000:0000 0A 00000000:00000000 00:00000000 00000000"

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
	rt := &fakeRT{stdout: verbatimProcNet}
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
	if len(rt.gotArgv) < 3 || rt.gotArgv[0] != "sh" || !strings.Contains(rt.gotArgv[2], "cat /proc/net/tcp") {
		t.Errorf("argv: want sh -c 'cat /proc/net/tcp ...', got %v", rt.gotArgv)
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

func TestParseProcNetTCP_ByteOrder(t *testing.T) {
	// naive big-endian parse of 0100007F gives 1.0.0.127 (WRONG); correct LE byte extraction gives 127.0.0.1
	line := "  0: 0100007F:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000"
	binds := ParseProcNetTCP(line)
	if len(binds) != 1 {
		t.Fatalf("want 1 bind, got %d", len(binds))
	}
	if binds[0].Port != 3000 {
		t.Errorf("port: want 3000 got %d", binds[0].Port)
	}
	if binds[0].BindAddr != "127.0.0.1" {
		t.Errorf("addr: want 127.0.0.1 got %q", binds[0].BindAddr)
	}
}

func TestParseProcNetTCP_Forms(t *testing.T) {
	ipv4Cases := []struct {
		input    string
		wantPort uint16
		wantAddr string
	}{
		{"  0: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000", 80, "0.0.0.0"},
		{"  0: 0100007F:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000", 3000, "127.0.0.1"},
		{"  0: 00000000:C350 00000000:0000 0A 00000000:00000000 00:00000000 00000000", 50000, "0.0.0.0"},
	}
	for _, c := range ipv4Cases {
		binds := ParseProcNetTCP(c.input)
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

	tcp6Line := "  0: 00000000000000000000000001000000:1F90 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000"
	binds := ParseProcNetTCP6(tcp6Line)
	if len(binds) != 1 {
		t.Fatalf("ipv6: want 1 bind got %d", len(binds))
	}
	if binds[0].Port != 8080 {
		t.Errorf("ipv6 port: want 8080 got %d", binds[0].Port)
	}
	if binds[0].BindAddr != "::1" {
		t.Errorf("ipv6 addr: want ::1 got %q", binds[0].BindAddr)
	}
}

func TestParseProcNetTCP_SkipsNonListen(t *testing.T) {
	line := "  0: 0100007F:0BB8 0100007F:C350 01 00000000:00000000 00:00000000 00000000"
	if len(ParseProcNetTCP(line)) != 0 {
		t.Error("want 0 binds for ESTABLISHED state")
	}
}

func TestParseProcNetTCP_Empty(t *testing.T) {
	if len(ParseProcNetTCP("")) != 0 {
		t.Error("want 0 binds for empty input")
	}
}

func TestFilterListeners_ReservedBelow1024(t *testing.T) {
	ls := []Listener{{Port: 80}}
	r := FilterListeners(ls, nil)
	if len(r.Reserved) != 1 || len(r.Forwardable) != 0 || len(r.OutOfRange) != 0 {
		t.Errorf("port 80: want Reserved got %+v", r)
	}
}

func TestFilterListeners_ForwardablePort(t *testing.T) {
	ls := []Listener{{Port: 3000}}
	r := FilterListeners(ls, nil)
	if len(r.Forwardable) != 1 || len(r.Reserved) != 0 || len(r.OutOfRange) != 0 {
		t.Errorf("port 3000: want Forwardable got %+v", r)
	}
}

func TestFilterListeners_OutOfRange(t *testing.T) {
	ls := []Listener{{Port: 50000}}
	r := FilterListeners(ls, nil)
	if len(r.OutOfRange) != 1 || len(r.Reserved) != 0 || len(r.Forwardable) != 0 {
		t.Errorf("port 50000: want OutOfRange got %+v", r)
	}
}

func TestFilterListeners_ExcludeList(t *testing.T) {
	ls := []Listener{{Port: 3000}}
	r := FilterListeners(ls, []uint16{3000})
	if len(r.Reserved) != 1 || len(r.Forwardable) != 0 {
		t.Errorf("port 3000 excluded: want Reserved got %+v", r)
	}
}
