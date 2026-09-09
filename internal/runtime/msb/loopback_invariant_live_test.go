package msb

import (
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

func listenOnce(t *testing.T, timeout time.Duration) (port int, await func() ([]byte, bool)) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	ch := make(chan []byte, 1)
	go func() {
		defer ln.Close()
		_ = ln.(*net.TCPListener).SetDeadline(time.Now().Add(timeout))
		conn, err := ln.Accept()
		if err != nil {
			ch <- nil
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(timeout))
		b, _ := io.ReadAll(conn)
		ch <- b
	}()
	port = ln.Addr().(*net.TCPAddr).Port
	await = func() ([]byte, bool) {
		select {
		case b := <-ch:
			return b, len(b) > 0
		case <-time.After(timeout):
			return nil, false
		}
	}
	return
}

func TestLiveEgressInvariantLoopbackIsolation(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()

	port1, await1 := listenOnce(t, 10*time.Second)
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port1))
	if err != nil {
		t.Fatalf("PROOF: instrument dial failed: %v", err)
	}
	_, _ = fmt.Fprint(conn, "ENGINE-INSTRUMENT-CHECK\n")
	conn.Close()
	b1, ok1 := await1()
	t.Logf("PROOF: instrument check received %d bytes: %q arrived=%v", len(b1), b1, ok1)
	if !ok1 {
		t.Fatalf("PROOF: instrument listener broken — all subsequent probes are vacuous")
	}

	port2, await2 := listenOnce(t, 20*time.Second)
	spec2 := coreruntime.SandboxSpec{
		Name:      fmt.Sprintf("sM2-shipped-%d", time.Now().UnixNano()),
		ImageRef:  "alpine",
		VCPUs:     1,
		MemoryMiB: 512,
		Motive:    "sM2-egress-invariant",
	}
	ref2, err := r.CreateAndBoot(ctx, spec2)
	if err != nil {
		t.Fatalf("CreateAndBoot shipped: %v", err)
	}
	CleanupSandbox(t, r, ref2)
	argv2 := fmt.Sprintf("echo FAKE-OAUTH-TOKEN-SHIPPED | nc -w3 host.microsandbox.internal %d; true", port2)
	if _, err := r.Exec(ctx, ref2, coreruntime.ExecRequest{Argv: []string{"sh", "-c", argv2}}); err != nil {
		t.Logf("PROOF: shipped exec error (expected for blocked): %v", err)
	}
	b2, arrived2 := await2()
	t.Logf("PROOF: shipped profile received %d bytes arrived=%v", len(b2), arrived2)
	if arrived2 {
		t.Errorf("PROOF: containment broken — shipped profile reached engine loopback: %q", strings.TrimSpace(string(b2)))
	}

	port3, await3 := listenOnce(t, 20*time.Second)
	spec3 := coreruntime.SandboxSpec{
		Name:      fmt.Sprintf("sM2-hostallow-%d", time.Now().UnixNano()),
		ImageRef:  "alpine",
		VCPUs:     1,
		MemoryMiB: 512,
		Motive:    "sM2-egress-invariant",
		NetRules:  []coreruntime.NetRule{{Action: coreruntime.NetAllow, Host: "host"}},
	}
	ref3, err := r.CreateAndBoot(ctx, spec3)
	if err != nil {
		t.Fatalf("CreateAndBoot host-allow: %v", err)
	}
	CleanupSandbox(t, r, ref3)
	argv3 := fmt.Sprintf("echo FAKE-OAUTH-TOKEN-HOSTALLOW | nc -w5 host.microsandbox.internal %d; true", port3)
	if _, err := r.Exec(ctx, ref3, coreruntime.ExecRequest{Argv: []string{"sh", "-c", argv3}}); err != nil {
		t.Logf("PROOF: host-allow exec error: %v", err)
	}
	b3, arrived3 := await3()
	t.Logf("PROOF: host-allow received %d bytes arrived=%v", len(b3), arrived3)
	if !arrived3 {
		t.Errorf("PROOF: RED control failed — host-allow profile could not reach engine loopback; test cannot distinguish block from broken instrument")
	}
}
