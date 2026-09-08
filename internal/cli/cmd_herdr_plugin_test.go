package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IniZio/herdr-plugin-msb/internal/core/portfwd"
)

type runResp struct {
	stdout, stderr string
	code           int
	err            error
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
			return r.stdout, r.stderr, r.code, r.err
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

func mustQueueJSON(t *testing.T, q *Queue) string {
	t.Helper()
	b, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func tickRun(queueJSON string, forwardCode int, capture *[][]string) Runner {
	return func(_ context.Context, argv []string) (string, string, int, error) {
		if capture != nil {
			*capture = append(*capture, append([]string(nil), argv...))
		}
		if argv[len(argv)-1] == RemoteReadCommand {
			return queueJSON, "", 0, nil
		}
		for _, a := range argv {
			if a == "forward" {
				return "", "", forwardCode, nil
			}
		}
		return "", "", 0, nil
	}
}

func countForwardCalls(calls [][]string) int {
	n := 0
	for _, argv := range calls {
		for _, a := range argv {
			if a == "forward" {
				n++
				break
			}
		}
	}
	return n
}

func hasMarkCall(calls [][]string, id uint64) bool {
	mark := RemoteMarkCommand(id)
	for _, argv := range calls {
		if argv[len(argv)-1] == mark {
			return true
		}
	}
	return false
}

func TestQueueRoundTrip(t *testing.T) {
	dir := t.TempDir()
	q := &Queue{
		Version:        1,
		LastConsumedID: 5,
		Pending: []Request{{
			ID:            6,
			Host:          "myhost",
			RemotePort:    8080,
			LocalPort:     8081,
			RemoteBind:    "127.0.0.1",
			Origin:        "origin",
			PaneID:        "pane-1",
			OpenURL:       "http://localhost:8081",
			PluginVersion: "v1.2.3",
			CreatedUnixMS: 1700000000000,
		}},
	}
	if err := WriteQueueAtomic(dir, q); err != nil {
		t.Fatal(err)
	}
	got, err := LoadQueue(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 {
		t.Fatalf("Version: got %d want 1", got.Version)
	}
	if got.LastConsumedID != 5 {
		t.Fatalf("LastConsumedID: got %d want 5", got.LastConsumedID)
	}
	if len(got.Pending) != 1 {
		t.Fatalf("Pending len: got %d want 1", len(got.Pending))
	}
	r, orig := got.Pending[0], q.Pending[0]
	if r.ID != orig.ID {
		t.Fatalf("ID: got %d want %d", r.ID, orig.ID)
	}
	if r.PaneID != orig.PaneID {
		t.Fatalf("PaneID: got %q want %q", r.PaneID, orig.PaneID)
	}
	if r.OpenURL != orig.OpenURL {
		t.Fatalf("OpenURL: got %q want %q", r.OpenURL, orig.OpenURL)
	}
	if r.RemotePort != orig.RemotePort {
		t.Fatalf("RemotePort: got %d want %d", r.RemotePort, orig.RemotePort)
	}
	if r.LocalPort != orig.LocalPort {
		t.Fatalf("LocalPort: got %d want %d", r.LocalPort, orig.LocalPort)
	}
	if r.PluginVersion != orig.PluginVersion {
		t.Fatalf("PluginVersion: got %q want %q", r.PluginVersion, orig.PluginVersion)
	}
}

func TestQueueMissingFile(t *testing.T) {
	q, err := LoadQueue(t.TempDir())
	if err != nil {
		t.Fatalf("missing file: want nil error, got %v", err)
	}
	if q.Version != 1 {
		t.Fatalf("Version: got %d want 1", q.Version)
	}
	if len(q.Pending) != 0 {
		t.Fatalf("Pending: want empty, got %v", q.Pending)
	}
}

func TestWriteQueueAtomic(t *testing.T) {
	dir := t.TempDir()
	if err := WriteQueueAtomic(dir, &Queue{Version: 1}); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, QueueFile+".tmp")
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("tmp file must not remain after rename; stat err: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, QueueFile))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("file mode: got %04o want 0600", mode)
	}
}

func TestNextID(t *testing.T) {
	q := &Queue{}
	if id := q.NextID(); id != 1 {
		t.Fatalf("NextID on empty queue: got %d want 1", id)
	}
	q.Pending = append(q.Pending, Request{ID: 7})
	if id := q.NextID(); id != 8 {
		t.Fatalf("NextID after ID=7: got %d want 8", id)
	}
}

func TestEnqueue_Monotonic(t *testing.T) {
	q := &Queue{}
	r1 := q.Enqueue(Request{RemotePort: 80})
	r2 := q.Enqueue(Request{RemotePort: 81})
	r3 := q.Enqueue(Request{RemotePort: 82})
	if !(r1.ID < r2.ID && r2.ID < r3.ID) {
		t.Fatalf("IDs not monotonic: %d %d %d", r1.ID, r2.ID, r3.ID)
	}
}

func TestEnqueue_SortedAfterOutOfOrder(t *testing.T) {
	q := &Queue{}
	q.Enqueue(Request{ID: 5, RemotePort: 80})
	q.Enqueue(Request{ID: 2, RemotePort: 81})
	q.Enqueue(Request{ID: 8, RemotePort: 82})
	for i := 1; i < len(q.Pending); i++ {
		if q.Pending[i].ID <= q.Pending[i-1].ID {
			t.Fatalf("Pending not sorted at index %d: %d <= %d",
				i, q.Pending[i].ID, q.Pending[i-1].ID)
		}
	}
}

func TestEnqueue_HonoursSuppliedID(t *testing.T) {
	q := &Queue{}
	r := q.Enqueue(Request{ID: 42, RemotePort: 80})
	if r.ID != 42 {
		t.Fatalf("supplied ID: got %d want 42", r.ID)
	}
	if len(q.Pending) == 0 || q.Pending[0].ID != 42 {
		t.Fatalf("Pending[0].ID: got %d want 42", q.Pending[0].ID)
	}
}

func TestPrune_DropsAcked(t *testing.T) {
	now := time.Now()
	ttl := 10 * time.Minute
	ms := uint64(now.UnixMilli())
	q := &Queue{Pending: []Request{
		{ID: 1, CreatedUnixMS: ms},
		{ID: 2, CreatedUnixMS: ms},
		{ID: 3, CreatedUnixMS: ms},
	}}
	q.Prune(2, now, ttl)
	for _, r := range q.Pending {
		if r.ID <= 2 {
			t.Fatalf("Prune must drop ID %d (acked=2)", r.ID)
		}
	}
	if len(q.Pending) != 1 || q.Pending[0].ID != 3 {
		t.Fatalf("Prune: want [ID=3], got %v", q.Pending)
	}
}

func TestPrune_DropsTTL(t *testing.T) {
	now := time.Now()
	ttl := time.Minute
	cutoff := uint64(now.Add(-ttl).UnixMilli())
	q := &Queue{Pending: []Request{
		{ID: 1, CreatedUnixMS: cutoff - 1},
	}}
	q.Prune(0, now, ttl)
	if len(q.Pending) != 0 {
		t.Fatalf("Prune must drop entry older than TTL; remaining: %v", q.Pending)
	}
}

func TestPrune_KeepsFresh(t *testing.T) {
	now := time.Now()
	ttl := time.Minute
	q := &Queue{Pending: []Request{
		{ID: 1, CreatedUnixMS: uint64(now.UnixMilli())},
	}}
	q.Prune(0, now, ttl)
	if len(q.Pending) != 1 {
		t.Fatalf("Prune must keep fresh entry; remaining: %v", q.Pending)
	}
}

func TestPrune_AdvancesLastConsumedID(t *testing.T) {
	now := time.Now()
	q := &Queue{LastConsumedID: 0, Pending: []Request{
		{ID: 10, CreatedUnixMS: uint64(now.UnixMilli())},
	}}
	q.Prune(5, now, 10*time.Minute)
	if q.LastConsumedID != 5 {
		t.Fatalf("LastConsumedID: got %d want 5", q.LastConsumedID)
	}
}

func TestPrune_Boundary(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	ttl := time.Minute
	cutoff := uint64(base.Add(-ttl).UnixMilli())

	qAt := &Queue{Pending: []Request{{ID: 1, CreatedUnixMS: cutoff}}}
	qAt.Prune(0, base, ttl)
	if len(qAt.Pending) != 1 {
		t.Fatalf("entry exactly at TTL cutoff must be kept; remaining: %v", qAt.Pending)
	}

	qBefore := &Queue{Pending: []Request{{ID: 1, CreatedUnixMS: cutoff - 1}}}
	qBefore.Prune(0, base, ttl)
	if len(qBefore.Pending) != 0 {
		t.Fatalf("entry 1ms before TTL cutoff must be pruned; remaining: %v", qBefore.Pending)
	}
}

func TestReadAck(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		n, err := ReadAck(t.TempDir())
		if err != nil || n != 0 {
			t.Fatalf("got %d,%v want 0,nil", n, err)
		}
	})
	t.Run("trailing newline", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, AckFile), []byte("42\n"), 0o600)
		n, err := ReadAck(dir)
		if err != nil || n != 42 {
			t.Fatalf("got %d,%v want 42,nil", n, err)
		}
	})
	t.Run("garbage", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, AckFile), []byte("not-a-number"), 0o600)
		n, err := ReadAck(dir)
		if err != nil || n != 0 {
			t.Fatalf("garbage content: got %d,%v want 0,nil", n, err)
		}
	})
}

func TestCheckControlPath(t *testing.T) {
	tests := []struct {
		name    string
		pathLen int
		wantErr bool
	}{
		{"accept 103 bytes", 103, false},
		{"reject 104 bytes", 104, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckControlPath(strings.Repeat("a", tc.pathLen))
			if (err != nil) != tc.wantErr {
				t.Fatalf("CheckControlPath(%d bytes): err=%v wantErr=%v", tc.pathLen, err, tc.wantErr)
			}
		})
	}
}

func TestMasterArgv(t *testing.T) {
	want := []string{
		"ssh", "-M", "-N", "-f",
		"-o", "ControlPath=/run/ctl",
		"-o", "ControlMaster=auto",
		"-o", "ControlPersist=yes",
		"-o", "BatchMode=yes",
		"-o", "GatewayPorts=no",
		"user@host",
	}
	got := MasterArgv("user@host", "/run/ctl")
	if !argvEq(got, want) {
		t.Fatalf("MasterArgv\n got  %v\n want %v", got, want)
	}
}

func TestExecArgv(t *testing.T) {
	want := []string{"ssh", "-S", "/run/ctl", "-o", "BatchMode=yes", "user@host", "echo hi"}
	got := ExecArgv("user@host", "/run/ctl", "echo hi")
	if !argvEq(got, want) {
		t.Fatalf("ExecArgv\n got  %v\n want %v", got, want)
	}
}

func TestForwardArgv(t *testing.T) {
	spec := "127.0.0.1:8080:127.0.0.1:5000"
	want := []string{"ssh", "-S", "/run/ctl", "-O", "forward", "-L", spec, "user@host"}
	got := ForwardArgv("user@host", "/run/ctl", spec)
	if !argvEq(got, want) {
		t.Fatalf("ForwardArgv\n got  %v\n want %v", got, want)
	}
}

func TestCancelArgv(t *testing.T) {
	spec := "127.0.0.1:8080:127.0.0.1:5000"
	want := []string{"ssh", "-S", "/run/ctl", "-O", "cancel", "-L", spec, "user@host"}
	got := CancelArgv("user@host", "/run/ctl", spec)
	if !argvEq(got, want) {
		t.Fatalf("CancelArgv\n got  %v\n want %v", got, want)
	}
}

func TestForwardSpec(t *testing.T) {
	r := Request{LocalPort: 3001, RemotePort: 5000}
	got := ForwardSpec(r)
	want := "127.0.0.1:3001:127.0.0.1:5000"
	if got != want {
		t.Fatalf("ForwardSpec: got %q want %q", got, want)
	}
}

func TestForwardSpec_SwapDetectable(t *testing.T) {
	r := Request{LocalPort: 3001, RemotePort: 5000}
	spec := ForwardSpec(r)
	swapped := "127.0.0.1:5000:127.0.0.1:3001"
	if spec == swapped {
		t.Fatalf("ForwardSpec %q is indistinguishable from swapped local/remote", spec)
	}
	if !strings.Contains(spec, ":3001:") {
		t.Fatalf("ForwardSpec %q: local port 3001 must appear first", spec)
	}
	if !strings.HasSuffix(spec, ":5000") {
		t.Fatalf("ForwardSpec %q: remote port 5000 must appear last", spec)
	}
}

func TestRemoteMarkCommand(t *testing.T) {
	cmd := RemoteMarkCommand(99)
	if !strings.Contains(cmd, "99") {
		t.Fatalf("RemoteMarkCommand: want id 99 in snippet; got: %s", cmd)
	}
	if !strings.Contains(cmd, "mv") {
		t.Fatalf("RemoteMarkCommand: want 'mv' rename; got: %s", cmd)
	}
	if !strings.Contains(cmd, StateDirNS) {
		t.Fatalf("RemoteMarkCommand: want state-dir namespace %q; got: %s", StateDirNS, cmd)
	}
}

func TestRemoteReadCommand_NoWrite(t *testing.T) {
	cmd := RemoteReadCommand
	if strings.Contains(cmd, "mv") {
		t.Fatalf("RemoteReadCommand must not rename/write; got: %s", cmd)
	}
	if strings.Contains(cmd, "> ") {
		t.Fatalf("RemoteReadCommand must not redirect-write; got: %s", cmd)
	}
}

func TestStateDir(t *testing.T) {
	t.Run("XDG wins", func(t *testing.T) {
		got, err := StateDir(func(k string) string {
			if k == "XDG_STATE_HOME" {
				return "/xdg"
			}
			return "/home/u"
		})
		want := "/xdg/" + StateDirNS
		if err != nil || got != want {
			t.Fatalf("XDG: got %q,%v want %q,nil", got, err, want)
		}
	})
	t.Run("HOME fallback", func(t *testing.T) {
		got, err := StateDir(func(k string) string {
			if k == "HOME" {
				return "/home/u"
			}
			return ""
		})
		want := "/home/u/.local/state/" + StateDirNS
		if err != nil || got != want {
			t.Fatalf("HOME: got %q,%v want %q,nil", got, err, want)
		}
	})
	t.Run("both empty errors", func(t *testing.T) {
		_, err := StateDir(func(string) string { return "" })
		if err == nil {
			t.Fatal("want error when both XDG_STATE_HOME and HOME are empty")
		}
	})
}

func TestAgent_IsOurs(t *testing.T) {
	a := &Agent{HostName: "myhost"}
	tests := []struct {
		name string
		host string
		want bool
	}{
		{"empty host matches any", "", true},
		{"exact match", "myhost", true},
		{"different host rejects", "otherhost", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := a.IsOurs(Request{Host: tc.host})
			if got != tc.want {
				t.Fatalf("IsOurs(%q): got %v want %v", tc.host, got, tc.want)
			}
		})
	}
}

func TestAgent_Tick_Success(t *testing.T) {
	now := time.Now()
	ms := uint64(now.UnixMilli())
	req1 := Request{ID: 1, Host: "", RemotePort: 8001, LocalPort: 8001, CreatedUnixMS: ms}
	req2 := Request{ID: 2, Host: "", RemotePort: 8002, LocalPort: 8002, CreatedUnixMS: ms}
	qj := mustQueueJSON(t, &Queue{Version: 1, Pending: []Request{req1, req2}})

	var calls [][]string
	a := &Agent{
		Target:      "user@sandbox",
		ControlPath: "/run/test.ctl",
		HostName:    "myhost",
		TTL:         10 * time.Minute,
		Run:         tickRun(qj, 0, &calls),
	}

	applied, settled, err := a.Tick(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 2 {
		t.Fatalf("applied: got %v want 2 entries", applied)
	}
	if settled != 2 {
		t.Fatalf("settled: got %d want 2", settled)
	}
	if n := countForwardCalls(calls); n != 2 {
		t.Fatalf("ForwardArgv calls: got %d want 2", n)
	}
	if !hasMarkCall(calls, 2) {
		t.Fatalf("RemoteMarkCommand(2) not issued; calls: %v", calls)
	}
}

func TestAgent_Tick_StallOnFirstFailure(t *testing.T) {
	now := time.Now()
	ms := uint64(now.UnixMilli())
	req1 := Request{ID: 1, Host: "", RemotePort: 8001, LocalPort: 8001, CreatedUnixMS: ms}
	req2 := Request{ID: 2, Host: "", RemotePort: 8002, LocalPort: 8002, CreatedUnixMS: ms}
	qj := mustQueueJSON(t, &Queue{Version: 1, Pending: []Request{req1, req2}})

	var calls [][]string
	a := &Agent{
		Target:      "user@sandbox",
		ControlPath: "/run/test.ctl",
		HostName:    "myhost",
		TTL:         10 * time.Minute,
		Run:         tickRun(qj, 1, &calls),
	}

	_, settled, _ := a.Tick(context.Background(), now)
	if settled != 0 {
		t.Fatalf("stall: settled must be 0 when first forward fails, got %d", settled)
	}
	if hasMarkCall(calls, 1) || hasMarkCall(calls, 2) {
		t.Fatalf("stall: no mark must be issued after first-forward failure; calls: %v", calls)
	}
}

func TestDeclarer_Declare_OnePerListener(t *testing.T) {
	dir := t.TempDir()
	d := &Declarer{
		Dir:           dir,
		Host:          "myhost",
		PluginVersion: "v1",
		TTL:           10 * time.Minute,
	}
	ls := []portfwd.Listener{
		{Port: 3000, BindAddr: "127.0.0.1"},
		{Port: 4000, BindAddr: "127.0.0.1"},
	}
	reqs, err := d.Declare(ls, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 2 {
		t.Fatalf("Declare: got %d requests want 2", len(reqs))
	}
}

func TestDeclarer_Declare_Idempotent(t *testing.T) {
	dir := t.TempDir()
	d := &Declarer{
		Dir:           dir,
		Host:          "myhost",
		PluginVersion: "v1",
		TTL:           10 * time.Minute,
	}
	ls := []portfwd.Listener{{Port: 3000, BindAddr: "127.0.0.1"}}
	now := time.Now()
	if _, err := d.Declare(ls, now); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Declare(ls, now); err != nil {
		t.Fatal(err)
	}
	q, err := LoadQueue(dir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, r := range q.Pending {
		if r.RemotePort == 3000 {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("Declare idempotent: want 1 entry for port 3000, got %d", count)
	}
}

func TestDeclarer_Declare_WritesFile(t *testing.T) {
	dir := t.TempDir()
	d := &Declarer{
		Dir:           dir,
		Host:          "myhost",
		PluginVersion: "v1",
		TTL:           10 * time.Minute,
	}
	ls := []portfwd.Listener{{Port: 3000, BindAddr: "127.0.0.1"}}
	if _, err := d.Declare(ls, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, QueueFile)); err != nil {
		t.Fatalf("queue file not written: %v", err)
	}
}
