package portfwd

import (
	"context"
	"testing"
)

type runResp struct {
	stdout, stderr string
	code           int
}

func seqRun(capture *[][]string, resps []runResp) Runner {
	i := 0
	return func(_ context.Context, argv []string) (string, string, int, error) {
		if capture != nil {
			*capture = append(*capture, append([]string(nil), argv...))
		}
		if i < len(resps) {
			r := resps[i]
			i++
			return r.stdout, r.stderr, r.code, nil
		}
		return "", "", 0, nil
	}
}

func argvEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

const (
	testSock = "/run/user/1000/cm-portfwd-test.sock"
	testHost = "sandbox-host"
)

func TestMasterAliveExit0(t *testing.T) {
	var calls [][]string
	f := &Forwarder{ControlPath: testSock, SSHHost: testHost, Run: seqRun(&calls, []runResp{{code: 0}})}
	alive, err := f.MasterAlive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !alive {
		t.Fatal("want alive=true for exit 0")
	}
	want := []string{"ssh", "-O", "check", "-o", "ControlPath=" + testSock, testHost}
	if !argvEq(calls[0], want) {
		t.Fatalf("check argv\n got  %v\n want %v", calls[0], want)
	}
}

func TestMasterAliveExit255(t *testing.T) {
	f := &Forwarder{ControlPath: testSock, SSHHost: testHost, Run: seqRun(nil, []runResp{{code: 255}})}
	alive, err := f.MasterAlive(context.Background())
	if err != nil || alive {
		t.Fatalf("want false,nil got %v,%v", alive, err)
	}
}

func TestMasterAliveUnexpectedExit(t *testing.T) {
	f := &Forwarder{ControlPath: testSock, SSHHost: testHost, Run: seqRun(nil, []runResp{{code: 1}})}
	_, err := f.MasterAlive(context.Background())
	if err == nil {
		t.Fatal("want error for unexpected exit code 1")
	}
}

func TestEnsureMasterWhenAlive(t *testing.T) {
	var calls [][]string
	f := &Forwarder{ControlPath: testSock, SSHHost: testHost, Run: seqRun(&calls, []runResp{{code: 0}})}
	if err := f.EnsureMaster(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("alive master: want 1 call, got %d", len(calls))
	}
}

func TestEnsureMasterWhenDead(t *testing.T) {
	var calls [][]string
	f := &Forwarder{
		ControlPath: testSock,
		SSHHost:     testHost,
		Run: seqRun(&calls, []runResp{
			{code: 255},
			{code: 0},
		}),
	}
	if err := f.EnsureMaster(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("dead master: want 2 calls, got %d", len(calls))
	}
	wantOpen := []string{
		"ssh", "-M", "-N", "-f",
		"-o", "ControlMaster=yes",
		"-o", "ControlPath=" + testSock,
		"-o", "ControlPersist=60",
		"-o", "BatchMode=yes",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout=10",
		testHost,
	}
	if !argvEq(calls[1], wantOpen) {
		t.Fatalf("open master argv\n got  %v\n want %v", calls[1], wantOpen)
	}
}

func TestApplyArgv(t *testing.T) {
	var calls [][]string
	f := &Forwarder{ControlPath: testSock, SSHHost: testHost, Run: seqRun(&calls, []runResp{{code: 0}})}
	if err := f.Apply(context.Background(), 3000); err != nil {
		t.Fatal(err)
	}
	want := []string{"ssh", "-O", "forward", "-L", "3000:127.0.0.1:3000", "-o", "ControlPath=" + testSock, testHost}
	if !argvEq(calls[0], want) {
		t.Fatalf("apply argv\n got  %v\n want %v", calls[0], want)
	}
}

func TestApplySamePortInvariant(t *testing.T) {
	var calls [][]string
	f := &Forwarder{ControlPath: testSock, SSHHost: testHost, Run: seqRun(&calls, []runResp{{code: 0}})}
	if err := f.Apply(context.Background(), 8080); err != nil {
		t.Fatal(err)
	}
	for i, arg := range calls[0] {
		if arg != "-L" || i+1 >= len(calls[0]) {
			continue
		}
		if calls[0][i+1] != "8080:127.0.0.1:8080" {
			t.Fatalf("same-port invariant violated: -L %q", calls[0][i+1])
		}
		return
	}
	t.Fatal("no -L argument in apply argv")
}

func TestCancelArgv(t *testing.T) {
	var calls [][]string
	f := &Forwarder{ControlPath: testSock, SSHHost: testHost, Run: seqRun(&calls, []runResp{{code: 0}})}
	if err := f.Cancel(context.Background(), 3000); err != nil {
		t.Fatal(err)
	}
	want := []string{"ssh", "-O", "cancel", "-L", "3000:127.0.0.1:3000", "-o", "ControlPath=" + testSock, testHost}
	if !argvEq(calls[0], want) {
		t.Fatalf("cancel argv\n got  %v\n want %v", calls[0], want)
	}
}

func TestPresentArgv(t *testing.T) {
	var calls [][]string
	f := &Forwarder{ControlPath: testSock, SSHHost: testHost, Run: seqRun(&calls, []runResp{{code: 0}})}
	_, _ = f.Present(context.Background(), 80)
	want := []string{"ss", "-ltn"}
	if !argvEq(calls[0], want) {
		t.Fatalf("present argv\n got  %v\n want %v", calls[0], want)
	}
}

func TestPresentTrueIPv6(t *testing.T) {
	ssOut := "Netid State Local Address:Port\ntcp LISTEN [::1]:45456 *:*"
	f := &Forwarder{ControlPath: testSock, SSHHost: testHost, Run: seqRun(nil, []runResp{{stdout: ssOut, code: 0}})}
	ok, err := f.Present(context.Background(), 45456)
	if err != nil || !ok {
		t.Fatalf("want true,nil got %v,%v", ok, err)
	}
}

func TestPresentTrueIPv4(t *testing.T) {
	ssOut := "tcp LISTEN 127.0.0.1:45456 0.0.0.0:*"
	if !portInOutput(ssOut, 45456) {
		t.Fatal("want true for 127.0.0.1:45456 form")
	}
}

func TestPresentFalse(t *testing.T) {
	ssOut := "Netid State Local Address:Port\ntcp LISTEN 127.0.0.1:22 0.0.0.0:*"
	f := &Forwarder{ControlPath: testSock, SSHHost: testHost, Run: seqRun(nil, []runResp{{stdout: ssOut, code: 0}})}
	ok, err := f.Present(context.Background(), 45456)
	if err != nil || ok {
		t.Fatalf("want false,nil got %v,%v", ok, err)
	}
}

func TestPresentPrefixNonMatch(t *testing.T) {
	if portInOutput("tcp LISTEN [::1]:45456 *:*", 4545) {
		t.Fatal("port 4545 must not match :45456")
	}
}

func TestPresentSuffixNonMatch(t *testing.T) {
	if portInOutput("tcp LISTEN [::1]:454560 *:*", 45456) {
		t.Fatal("port 45456 must not match :454560")
	}
}
