package clientagent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const liveEngine03Record = `{"version":1,"last_consumed_id":0,"pending":[{"id":5,"host":"engine-03","remote_port":8080,"local_port":8080,"remote_bind":"127.0.0.1","origin":"discovery","plugin_version":"0.1.0"}]}`

type applyCall struct {
	argv []string
}

type applyRunner struct {
	queueJSON   string
	ackJSON     string
	forwardCode int
	forwardErr  error
	calls       []applyCall
}

func (a *applyRunner) run(_ context.Context, argv []string) (string, string, int, error) {
	a.calls = append(a.calls, applyCall{argv: append([]string(nil), argv...)})
	last := argv[len(argv)-1]
	if last == RemoteReadCommand {
		return a.queueJSON, "", 0, nil
	}
	if last == RemoteAckReadCommand {
		return a.ackJSON, "", 0, nil
	}
	for _, s := range argv {
		if s == "forward" {
			return "", "ssh: forward refused", a.forwardCode, a.forwardErr
		}
	}
	return "", "", 0, nil
}

func (a *applyRunner) forwards() []string {
	var out []string
	for _, c := range a.calls {
		for i, s := range c.argv {
			if s == "forward" && i+2 < len(c.argv) {
				out = append(out, c.argv[i+2])
			}
		}
	}
	return out
}

func (a *applyRunner) marked(id uint64) bool {
	want := RemoteMarkCommand(id)
	for _, c := range a.calls {
		if c.argv[len(c.argv)-1] == want {
			return true
		}
	}
	return false
}

func (a *applyRunner) anyMark() string {
	for _, c := range a.calls {
		last := c.argv[len(c.argv)-1]
		if strings.Contains(last, AckFile+".$$") {
			return last
		}
	}
	return ""
}

func queueWith(t *testing.T, rs ...Request) string {
	t.Helper()
	b, err := json.Marshal(&Queue{Version: 1, Pending: rs})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTickAppliesRequestDeclaredForTargetHost(t *testing.T) {
	now := time.Now()
	r := Request{
		ID: 5, Host: "engine-03", RemotePort: 8080, LocalPort: 8080,
		RemoteBind: "127.0.0.1", Origin: "discovery", PluginVersion: "0.1.0",
		CreatedUnixMS: uint64(now.UnixMilli()),
	}
	rn := &applyRunner{queueJSON: queueWith(t, r)}
	a := &Agent{Target: "engine-03", ControlPath: "/run/t.ctl", TTL: 10 * time.Minute, Run: rn.run}

	applied, settled, err := a.Tick(context.Background(), now)
	if err != nil {
		t.Fatalf("Tick: unexpected error %v", err)
	}
	fw := rn.forwards()
	if len(fw) != 1 || fw[0] != "127.0.0.1:8080:127.0.0.1:8080" {
		t.Fatalf("forward specs applied: got %v, want [127.0.0.1:8080:127.0.0.1:8080]; ack issued=%q", fw, rn.anyMark())
	}
	if len(applied) != 1 || applied[0] != 5 {
		t.Fatalf("applied ids: got %v want [5]", applied)
	}
	if settled != 5 {
		t.Fatalf("settled: got %d want 5", settled)
	}
	if !rn.marked(5) {
		t.Fatalf("ack for id 5 not issued after a successful forward")
	}
}

func TestTickDoesNotAckHostMismatch(t *testing.T) {
	now := time.Now()
	r := Request{
		ID: 5, Host: "other-engine", RemotePort: 8080, LocalPort: 8080,
		CreatedUnixMS: uint64(now.UnixMilli()),
	}
	rn := &applyRunner{queueJSON: queueWith(t, r)}
	var reported []string
	a := &Agent{
		Target: "engine-03", ControlPath: "/run/t.ctl", TTL: 10 * time.Minute,
		Run:    rn.run,
		Report: func(s string) { reported = append(reported, s) },
	}

	applied, settled, _ := a.Tick(context.Background(), now)
	if len(applied) != 0 {
		t.Fatalf("applied: got %v want none for a foreign host", applied)
	}
	if m := rn.anyMark(); m != "" {
		t.Fatalf("ack issued for an unapplied request: %q", m)
	}
	if settled != 0 {
		t.Fatalf("settled: got %d want 0 for a foreign host", settled)
	}
	if len(reported) == 0 {
		t.Fatalf("no operator report emitted for skipped request 5")
	}
	if !strings.Contains(reported[0], "other-engine") || !strings.Contains(reported[0], "engine-03") {
		t.Fatalf("report must name both the declared host and the served host, got %q", reported[0])
	}
}

func TestTickDoesNotAckExpiredRequest(t *testing.T) {
	now := time.Now()
	rn := &applyRunner{queueJSON: liveEngine03Record}
	var reported []string
	a := &Agent{
		Target: "engine-03", ControlPath: "/run/t.ctl", TTL: 10 * time.Minute,
		Run:    rn.run,
		Report: func(s string) { reported = append(reported, s) },
	}

	applied, settled, _ := a.Tick(context.Background(), now)
	if len(applied) != 0 {
		t.Fatalf("applied: got %v want none for an expired request", applied)
	}
	if m := rn.anyMark(); m != "" {
		t.Fatalf("ack issued for an unapplied expired request: %q", m)
	}
	if settled != 0 {
		t.Fatalf("settled: got %d want 0", settled)
	}
	if len(reported) == 0 {
		t.Fatalf("no operator report emitted for expired request 5")
	}
	if !strings.Contains(reported[0], "expired") {
		t.Fatalf("report must say the request expired, got %q", reported[0])
	}
}

func TestTickPartialBatchAcksOnlyAppliedPrefix(t *testing.T) {
	now := time.Now()
	ms := uint64(now.UnixMilli())
	ok1 := Request{ID: 1, Host: "engine-03", RemotePort: 8001, LocalPort: 8001, CreatedUnixMS: ms}
	stale := Request{ID: 2, Host: "engine-03", RemotePort: 8002, LocalPort: 8002}
	ok3 := Request{ID: 3, Host: "engine-03", RemotePort: 8003, LocalPort: 8003, CreatedUnixMS: ms}
	rn := &applyRunner{queueJSON: queueWith(t, ok1, stale, ok3)}
	a := &Agent{
		Target: "engine-03", ControlPath: "/run/t.ctl", TTL: 10 * time.Minute,
		Run:    rn.run,
		Report: func(string) {},
	}

	_, settled, _ := a.Tick(context.Background(), now)
	if settled != 1 {
		t.Fatalf("settled: got %d want 1 (only the applied prefix)", settled)
	}
	if rn.marked(2) || rn.marked(3) {
		t.Fatalf("ack must not pass the unapplied request 2; marks: %v", rn.calls)
	}
	if !rn.marked(1) {
		t.Fatalf("ack for the applied request 1 not issued")
	}
}

func TestTickReportsForwardFailure(t *testing.T) {
	now := time.Now()
	r := Request{ID: 1, Host: "engine-03", RemotePort: 8080, LocalPort: 8080, CreatedUnixMS: uint64(now.UnixMilli())}
	rn := &applyRunner{queueJSON: queueWith(t, r), forwardCode: 255}
	var reported []string
	a := &Agent{
		Target: "engine-03", ControlPath: "/run/t.ctl", TTL: 10 * time.Minute,
		Run:    rn.run,
		Report: func(s string) { reported = append(reported, s) },
	}

	_, settled, err := a.Tick(context.Background(), now)
	if err == nil {
		t.Fatalf("Tick must return an error when ssh -O forward exits non-zero")
	}
	if settled != 0 {
		t.Fatalf("settled: got %d want 0 after a failed forward", settled)
	}
	if len(reported) == 0 {
		t.Fatalf("forward failure was not reported to the operator")
	}
	if !strings.Contains(reported[0], "forward refused") {
		t.Fatalf("report must carry ssh stderr, got %q", reported[0])
	}
}

func TestServeReportsTickError(t *testing.T) {
	rn := &applyRunner{queueJSON: "", forwardErr: nil}
	rn.queueJSON = "{"
	var reported []string
	a := &Agent{
		Target: "engine-03", ControlPath: "/run/t.ctl", Poll: time.Millisecond,
		Run:    rn.run,
		Report: func(s string) { reported = append(reported, s) },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := a.Serve(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Serve: %v", err)
	}
	if len(reported) == 0 {
		t.Fatalf("Serve swallowed the tick error instead of reporting it")
	}
	if !strings.Contains(reported[0], "parse remote queue") {
		t.Fatalf("Serve report must carry the tick error, got %q", reported[0])
	}
}

func TestSpawnIfAbsentCapturesChildOutput(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-agent")
	body := "#!/bin/sh\necho agent-stderr-marker 1>&2\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := SpawnIfAbsent(dir, script, "engine-03"); err != nil {
		t.Fatal(err)
	}
	logPath := AgentLogPath(dir, "engine-03")
	deadline := time.Now().Add(5 * time.Second)
	var got []byte
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(logPath)
		if err == nil && strings.Contains(string(b), "agent-stderr-marker") {
			got = b
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(string(got), "agent-stderr-marker") {
		t.Fatalf("child stderr not captured at %s; content=%q", logPath, string(got))
	}
}
