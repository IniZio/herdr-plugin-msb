package msb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

func TestPublishTwoHostReach(t *testing.T) {
	RequireLive(t)
	laptop := os.Getenv("HERDR_MSB_LIVE_LAPTOP")
	if laptop == "" {
		t.Skipf("two-host publish test: set HERDR_MSB_LIVE_LAPTOP=user@host (e.g. newman@100.64.0.35) to enable")
	}
	engine := os.Getenv("HERDR_MSB_LIVE_ENGINE")
	if engine == "" {
		engine = "newman@100.64.0.156"
	}

	port := uint16(45455)
	if v := os.Getenv("HERDR_MSB_LIVE_PORT"); v != "" {
		p, err := strconv.ParseUint(v, 10, 16)
		if err != nil || p == 0 {
			t.Fatalf("bad HERDR_MSB_LIVE_PORT=%q: %v", v, err)
		}
		port = uint16(p)
	}
	portStr := fmt.Sprintf("%d", port)

	ctx := LiveContext(t)
	r := New()

	hostRun := func(argv ...string) (int, string, string) {
		var out, errBuf bytes.Buffer
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Stdout = &out
		cmd.Stderr = &errBuf
		_ = cmd.Run()
		code := 0
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		return code, out.String(), errBuf.String()
	}

	guestRun := func(ref coreruntime.SandboxRef, argv []string) (int32, string, string) {
		var out, errBuf bytes.Buffer
		res, err := r.Exec(ctx, ref, coreruntime.ExecRequest{Argv: argv, Stdout: &out, Stderr: &errBuf})
		if err != nil {
			return -1, out.String(), errBuf.String()
		}
		return res.ExitCode, out.String(), errBuf.String()
	}

	sshRun := func(cmd string) (int, string, string) {
		quoted := "bash -lc '" + strings.ReplaceAll(cmd, "'", `'\''`) + "'"
		return hostRun("ssh", "-o", "BatchMode=yes", laptop, quoted)
	}

	_, ssOut, _ := hostRun("ss", "-ltn")
	if strings.Contains(ssOut, ":"+portStr) {
		t.Fatalf("PROOF P0: DEGENERATE host already listens on :%s — test would prove nothing\n%s", portStr, ssOut)
	}
	t.Logf("PROOF P0: host ss -ltn shows no :%s LISTEN — baseline clean", portStr)

	name := fmt.Sprintf("s04-pub-%d", time.Now().UnixNano())
	spec := coreruntime.SandboxSpec{
		Project:   "s04",
		Name:      name,
		ImageRef:  "alpine",
		VCPUs:     1,
		MemoryMiB: 512,
		Motive:    "nexus3-microsandbox-pivot",
	}

	ref, err := r.CreateAndBoot(ctx, spec)
	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}

	var liveRef = ref
	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if h, herr := r.handle(cleanCtx, liveRef); herr == nil {
			_ = h.Kill(cleanCtx)
			_ = h.Remove(cleanCtx)
		}
	})

	marker1 := fmt.Sprintf("mk1-%d", time.Now().UnixNano())
	guestRun(ref, []string{"sh", "-c", fmt.Sprintf("echo %s > /tmp/marker.txt", marker1)})

	listenScript := fmt.Sprintf(
		"nohup sh -c \"while true; do printf 'HTTP/1.1 200 OK\\r\\nContent-Length: %d\\r\\nConnection: close\\r\\n\\r\\n%s' | nc -l -p %s; done\" >/dev/null 2>&1 & sleep 1; exit 0",
		len(marker1), marker1, portStr,
	)
	armCode, armOut, armErr := guestRun(ref, []string{"sh", "-c", listenScript})
	t.Logf("PROOF P1 arm: exit=%d out=%q err=%q", armCode, armOut, armErr)
	time.Sleep(600 * time.Millisecond)

	_, nsOut, _ := guestRun(ref, []string{"netstat", "-ltn"})
	if !strings.Contains(nsOut, portStr) {
		t.Fatalf("PROOF P1: guest netstat -ltn=%q — port %s not listening after arm", nsOut, portStr)
	}
	t.Logf("PROOF P1: guest netstat shows :%s LISTEN; marker1=%s", portStr, marker1)

	ncCode, _, _ := hostRun("nc", "-z", "-w5", "127.0.0.1", portStr)
	t.Logf("PROOF P2: host nc -z 127.0.0.1:%s exit=%d (want non-zero — unpublished)", portStr, ncCode)
	if ncCode == 0 {
		t.Fatalf("P2: unpublished guest port reachable from host — proof degenerate")
	}

	pub, ok := interface{}(r).(coreruntime.Publisher)
	if !ok {
		t.Fatal("*Runtime does not implement coreruntime.Publisher — Publish not yet landed in this tree")
	}
	t0 := time.Now()
	pubRef, err := pub.Publish(ctx, spec, []uint16{port})
	elapsed := time.Since(t0)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	liveRef = pubRef
	ref = pubRef
	t.Logf("PROOF P3: Publish elapsed=%s sandbox=%s", elapsed, ref.Name)

	catCode, catOut, _ := guestRun(ref, []string{"cat", "/tmp/marker.txt"})
	t.Logf("PROOF P3: cat /tmp/marker.txt exit=%d out=%q (want non-zero — state destroyed by recreate)", catCode, catOut)
	if catCode == 0 {
		t.Errorf("P3: /tmp/marker.txt readable after Publish recreate — state-loss cost not demonstrated")
	}

	marker2 := fmt.Sprintf("mk2-%d", time.Now().UnixNano())
	listenScript2 := fmt.Sprintf(
		"nohup sh -c \"while true; do printf 'HTTP/1.1 200 OK\\r\\nContent-Length: %d\\r\\nConnection: close\\r\\n\\r\\n%s' | nc -l -p %s; done\" >/dev/null 2>&1 & sleep 1; exit 0",
		len(marker2), marker2, portStr,
	)
	guestRun(ref, []string{"sh", "-c", listenScript2})
	time.Sleep(600 * time.Millisecond)

	_, nsOut2, _ := guestRun(ref, []string{"netstat", "-ltn"})
	if !strings.Contains(nsOut2, portStr) {
		t.Fatalf("PROOF P4: guest netstat=%q — port %s not re-armed", nsOut2, portStr)
	}
	t.Logf("PROOF P4: guest netstat shows :%s re-armed; marker2=%s", portStr, marker2)

	ncCode2, _, _ := hostRun("nc", "-z", "-w5", "127.0.0.1", portStr)
	t.Logf("PROOF P5: host nc -z 127.0.0.1:%s exit=%d (want 0 — published)", portStr, ncCode2)
	if ncCode2 != 0 {
		t.Fatalf("P5: published port unreachable from host exit=%d", ncCode2)
	}

	hresp, httpErr := http.Get(fmt.Sprintf("http://127.0.0.1:%s/", portStr))
	var body string
	if httpErr == nil {
		b, _ := io.ReadAll(hresp.Body)
		hresp.Body.Close()
		body = string(b)
	}
	t.Logf("PROOF P5: http GET http://127.0.0.1:%s/ err=%v body=%q", portStr, httpErr, body)
	if httpErr != nil || !strings.Contains(body, marker2) {
		t.Fatalf("P5: body %q does not contain marker2=%q (err=%v)", body, marker2, httpErr)
	}

	portFwdPat := fmt.Sprintf("ssh -f -N .*-L %s:127.0.0.1:%s", portStr, portStr)
	killFwd := func() {
		sshRun(fmt.Sprintf("pkill -f '%s' 2>/dev/null; true", portFwdPat))
		time.Sleep(400 * time.Millisecond)
	}
	t.Cleanup(func() {
		killFwd()
		_, lapNs, _ := sshRun(fmt.Sprintf("netstat -an -p tcp | grep -E '[.:]%s ' | grep LISTEN || true", portStr))
		t.Logf("PROOF P6e t.Cleanup: laptop LISTEN after forward kill: %q", strings.TrimSpace(lapNs))
	})

	_, lapNsPre, _ := sshRun(fmt.Sprintf("netstat -an -p tcp | grep -E '[.:]%s ' | grep LISTEN || true", portStr))
	t.Logf("PROOF P6a: laptop pre-forward LISTEN on :%s = %q (want empty)", portStr, strings.TrimSpace(lapNsPre))
	if strings.Contains(lapNsPre, "LISTEN") {
		t.Fatalf("P6a: laptop already listens on :%s — negative control degenerate", portStr)
	}

	fwdCmd := fmt.Sprintf("ssh -f -N -o ExitOnForwardFailure=yes -o BatchMode=yes -L %s:127.0.0.1:%s %s", portStr, portStr, engine)
	fwdCode, _, fwdStderr := sshRun(fwdCmd)
	t.Logf("PROOF P6b: forward from laptop to %s -L %s:%s:127.0.0.1:%s exit=%d stderr=%q",
		engine, portStr, portStr, portStr, fwdCode, fwdStderr)
	if fwdCode != 0 {
		t.Fatalf("P6b: laptop-side ssh -L exited %d: %s", fwdCode, fwdStderr)
	}
	time.Sleep(1200 * time.Millisecond)

	_, lapNsPost, _ := sshRun(fmt.Sprintf("netstat -an -p tcp | grep -E '[.:]%s ' | grep LISTEN || true", portStr))
	t.Logf("PROOF P6c: laptop LISTEN after forward: %q (want LISTEN — read from local socket)", strings.TrimSpace(lapNsPost))
	if !strings.Contains(lapNsPost, "LISTEN") {
		t.Fatalf("P6c: laptop has no LISTEN on :%s after forward — tunnel not established", portStr)
	}

	t.Logf("PROOF P6 port invariant: port=%s local-L-port=%s remote-L-port=%s published-host-port=%s guest-port=%s — all the same number",
		portStr, portStr, portStr, portStr, portStr)

	hcCode, hcBody, _ := hostRun("curl", "-sS", "--max-time", "10", fmt.Sprintf("http://127.0.0.1:%s/", portStr))
	t.Logf("PROOF P6d control: host curl exit=%d body=%q (host reach at the same instant)", hcCode, hcBody)

	curlCode, curlBody, _ := sshRun(fmt.Sprintf("for i in 1 2 3 4 5 6; do out=$(curl -sS --max-time 5 http://127.0.0.1:%s/) && [ -n \"$out\" ] && printf '%%s' \"$out\" && exit 0; sleep 1; done; exit 7", portStr))
	t.Logf("PROOF P6d: laptop curl http://127.0.0.1:%s/ exit=%d body=%q", portStr, curlCode, curlBody)
	if !strings.Contains(curlBody, marker2) {
		t.Fatalf("P6d: laptop curl body=%q does not contain marker2=%q (exit=%d)", curlBody, marker2, curlCode)
	}

	killFwd()
	_, lapNsGone, _ := sshRun(fmt.Sprintf("netstat -an -p tcp | grep -E '[.:]%s ' | grep LISTEN || true", portStr))
	t.Logf("PROOF P6e: laptop LISTEN after pkill: %q (want empty)", strings.TrimSpace(lapNsGone))
	if strings.Contains(lapNsGone, "LISTEN") {
		t.Errorf("P6e: laptop LISTEN socket still present after pkill: %q", lapNsGone)
	}

	fwdCode2, _, _ := sshRun(fwdCmd)
	t.Logf("PROOF P7 pre: re-established forward exit=%d", fwdCode2)
	time.Sleep(1200 * time.Millisecond)

	if _, stopErr := r.Stop(ctx, ref); stopErr != nil {
		t.Fatalf("Stop: %v", stopErr)
	}
	time.Sleep(600 * time.Millisecond)

	ncAfter, _, _ := hostRun("nc", "-z", "-w5", "127.0.0.1", portStr)
	t.Logf("PROOF P7: host nc -z 127.0.0.1:%s after Stop exit=%d (want non-zero)", portStr, ncAfter)
	if ncAfter == 0 {
		t.Errorf("P7: port still reachable from host after Stop")
	}

	curlAfterCode, curlAfterBody, _ := sshRun(fmt.Sprintf("curl -sS --max-time 5 http://127.0.0.1:%s/", portStr))
	t.Logf("PROOF P7: laptop curl after Stop exit=%d body=%q (want failure)", curlAfterCode, curlAfterBody)
	if strings.Contains(curlAfterBody, marker2) {
		t.Errorf("P7: laptop curl body still contains marker2 after Stop — port teardown not confirmed")
	}
	if curlAfterCode == 0 {
		t.Errorf("P7: laptop curl exited 0 after Stop — port not torn down")
	}
}
