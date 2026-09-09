package msb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

func liveRangeSpec(name string, mib uint32) coreruntime.SandboxSpec {
	return coreruntime.SandboxSpec{
		Project:   "sra",
		Name:      name,
		ImageRef:  "alpine",
		VCPUs:     1,
		MemoryMiB: mib,
		Motive:    "sR-host-range-allocator",
	}
}

func hostRun(ctx context.Context, argv ...string) (int, string) {
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run()
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	return code, out.String()
}

func guestExec(ctx context.Context, r *Runtime, ref coreruntime.SandboxRef, argv []string) (int32, string) {
	var out bytes.Buffer
	res, err := r.Exec(ctx, ref, coreruntime.ExecRequest{Argv: argv, Stdout: &out, Stderr: &out})
	if err != nil {
		return -1, out.String()
	}
	return res.ExitCode, out.String()
}

func dbOccupied(ctx context.Context, t *testing.T) [rangeMaxBlocks]bool {
	t.Helper()
	occ, err := daemonOccupiedBlocks(ctx)
	if err != nil {
		t.Fatalf("daemonOccupiedBlocks: %v", err)
	}
	return occ
}

func TestLiveRangeAllocDisjoint(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)

	r1 := &Runtime{alloc: NewRangeAllocator()}
	r2 := &Runtime{alloc: NewRangeAllocator()}

	_, listOut := hostRun(ctx, "msb", "list")
	t.Logf("PROOF D0: msb list before window: %s", strings.TrimSpace(listOut))

	before := dbOccupied(ctx, t)
	t.Logf("PROOF D0b: daemon blocks occupied before: %v", before)

	var ref1, ref2 coreruntime.SandboxRef
	cleanup := func() {
		cctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		for _, ref := range []coreruntime.SandboxRef{ref1, ref2} {
			if ref.Name == "" {
				continue
			}
			if h, err := r1.handle(cctx, ref); err == nil {
				_ = h.Kill(cctx)
				_ = h.Remove(cctx)
			}
		}
	}
	t.Cleanup(cleanup)

	var err error
	ref1, err = r1.CreateAndBoot(ctx, liveRangeSpec("ra-disj-1", 512))
	if err != nil {
		t.Fatalf("CreateAndBoot ref1: %v", err)
	}

	after1 := dbOccupied(ctx, t)
	t.Logf("PROOF D1: daemon blocks after sandbox 1: %v", after1)

	ref2, err = r2.CreateAndBoot(ctx, liveRangeSpec("ra-disj-2", 512))
	if err != nil {
		t.Fatalf("CreateAndBoot ref2: %v", err)
	}

	after2 := dbOccupied(ctx, t)
	t.Logf("PROOF D2: daemon blocks after sandbox 2: %v", after2)

	occupied := []int{}
	for i, v := range after2 {
		if v {
			occupied = append(occupied, i)
		}
	}
	if len(occupied) != 2 {
		t.Errorf("D3 FAIL: expected 2 distinct occupied blocks, got %v — allocators may have collided", after2)
	} else {
		t.Logf("PROOF D3 PASS: daemon records show distinct blocks %v (ownership-aware, derived from DB)", occupied)
	}

	cleanup()
	ref1.Name, ref2.Name = "", ""
	_, listOut2 := hostRun(ctx, "msb", "list")
	t.Logf("PROOF D4: msb list after window: %s", strings.TrimSpace(listOut2))
}

func TestLiveRangeAllocSeparateProcess(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)

	r1 := &Runtime{alloc: NewRangeAllocator()}
	ref1, err := r1.CreateAndBoot(ctx, liveRangeSpec("ra-sp-1", 512))
	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}
	defer func() {
		cctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if h, herr := r1.handle(cctx, ref1); herr == nil {
			_ = h.Kill(cctx)
			_ = h.Remove(cctx)
		}
	}()

	after1 := dbOccupied(ctx, t)
	t.Logf("PROOF SP1: block mask after sandbox 1 (process 1): %v", after1)

	msbHome := os.Getenv("MSB_HOME")
	gotest := exec.CommandContext(ctx,
		"go", "test", "-count=1", "-v", "-run", "TestLiveRangeAllocSecondInvocation",
		"./internal/runtime/msb/")
	gotest.Env = append(os.Environ(), "HERDR_MSB_LIVE=1", "MSB_HOME="+msbHome)
	gotest.Dir = "/home/newman/magic/herdr-plugin-msb"
	out2, err2 := gotest.Output()
	outStr := strings.TrimSpace(string(out2))
	if err2 != nil {
		t.Logf("subprocess stderr included in output")
	}
	t.Logf("PROOF SP2: separate process invocation output:\n%s", outStr)
	if !strings.Contains(outStr, "PROOF SP2 PASS") {
		t.Errorf("SP FAIL: second process did not log PROOF SP2 PASS — not disjoint")
	}
}

func TestLiveRangeAllocSecondInvocation(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)

	occ, err := daemonOccupiedBlocks(ctx)
	if err != nil {
		t.Fatalf("daemonOccupiedBlocks: %v", err)
	}
	t.Logf("PROOF SP2: daemon block mask at second-process startup: %v", occ)

	r := &Runtime{alloc: NewRangeAllocator()}
	base, aerr := r.alloc.Allocate(ctx, "would-be-second")
	if aerr != nil {
		t.Fatalf("PROOF SP2 FAIL: second-process alloc: %v", aerr)
	}
	if base == rangeAllocBase {
		t.Errorf("PROOF SP2 FAIL: second process got base=%d same as first (block 0 should be occupied in DB)", base)
	} else {
		t.Logf("PROOF SP2 PASS: second process allocated base=%d (≠%d) — DB-derived disjoint allocation confirmed", base, rangeAllocBase)
	}
}

func TestLiveRangeAllocOwnershipHole(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)

	blocker, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", rangeAllocBase))
	if err != nil {
		t.Fatalf("bind port %d: %v", rangeAllocBase, err)
	}
	t.Logf("PROOF OH1: bound port %d (blocking base port so microsandbox skips it)", rangeAllocBase)

	r := &Runtime{alloc: NewRangeAllocator()}
	ref, err := r.CreateAndBoot(ctx, liveRangeSpec("ra-oh-1", 512))
	blocker.Close()
	t.Logf("PROOF OH2: released blocker on port %d after create", rangeAllocBase)

	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}
	defer func() {
		cctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if h, herr := r.handle(cctx, ref); herr == nil {
			_ = h.Kill(cctx)
			_ = h.Remove(cctx)
		}
	}()

	ln2, err2 := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", rangeAllocBase))
	tcpFree := err2 == nil
	if tcpFree {
		ln2.Close()
	}
	t.Logf("PROOF OH3: TCP-probe says port %d is free=%v (would cause false 'block unoccupied' with old design)", rangeAllocBase, tcpFree)

	occ, err := daemonOccupiedBlocks(ctx)
	if err != nil {
		t.Fatalf("daemonOccupiedBlocks: %v", err)
	}
	t.Logf("PROOF OH4: daemon-derived block mask: %v", occ)
	if !occ[0] {
		t.Errorf("OH FAIL: daemon shows block 0 free even though sandbox ra-oh-1 has ports there — DB query may be wrong")
	} else {
		t.Logf("PROOF OH4 PASS: daemon correctly shows block 0 occupied despite port %d being free (TCP-probe would give wrong answer here)", rangeAllocBase)
	}

	r2 := &Runtime{alloc: NewRangeAllocator()}
	base2, aerr := r2.alloc.Allocate(ctx, "sra--ra-oh-would-be-second")
	if aerr != nil {
		t.Fatalf("second alloc: %v", aerr)
	}
	if base2 == rangeAllocBase {
		t.Errorf("OH FAIL: second allocator returned base=%d — ownership hole NOT fixed; would collide with ra-oh-1", rangeAllocBase)
	} else {
		t.Logf("PROOF OH5 PASS: second allocator got base=%d (≠%d) — ownership hole correctly closed by DB-derived occupancy", base2, rangeAllocBase)
	}
}

func TestLiveRangeAllocDataTransfer(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)

	r := &Runtime{alloc: NewRangeAllocator()}
	spec := liveRangeSpec("ra-xfer", 512)

	ref, err := r.CreateAndBoot(ctx, spec)
	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}
	CleanupSandbox(t, r, ref)

	guestPort := uint16(3000)
	hostPort := rangeAllocBase + (guestPort - rangeGuestBase)
	t.Logf("PROOF X1: guestPort=%d hostPort=%d (base=%d)", guestPort, hostPort, rangeAllocBase)

	marker := fmt.Sprintf("XFER-%d", time.Now().UnixNano())
	listenScript := fmt.Sprintf(
		"nohup sh -c 'while true; do echo %s | nc -l -p %d; done' >/dev/null 2>&1 & sleep 1; exit 0",
		marker, guestPort,
	)
	armCode, armOut := guestExec(ctx, r, ref, []string{"sh", "-c", listenScript})
	t.Logf("PROOF X2 arm: exit=%d out=%q", armCode, armOut)
	time.Sleep(800 * time.Millisecond)

	_, nsOut := guestExec(ctx, r, ref, []string{"netstat", "-ltn"})
	if !strings.Contains(nsOut, fmt.Sprintf("%d", guestPort)) {
		t.Fatalf("PROOF X2: guest netstat=%q — port %d not listening", nsOut, guestPort)
	}
	t.Logf("PROOF X2: guest netstat shows :%d LISTEN", guestPort)

	addr := fmt.Sprintf("127.0.0.1:%d", hostPort)
	conn, dialErr := net.DialTimeout("tcp", addr, 8*time.Second)
	if dialErr != nil {
		t.Fatalf("PROOF X3 FAIL: dial host %s: %v", addr, dialErr)
	}
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, conn)
	conn.Close()
	received := strings.TrimSpace(buf.String())
	t.Logf("PROOF X3: sent marker=%q received=%q", marker, received)
	if received != marker {
		t.Errorf("X3 FAIL: received %q, want %q", received, marker)
	}

	unpublishedPort := uint16(1023)
	ncCode, _ := hostRun(ctx, "nc", "-z", "-w5", "127.0.0.1", fmt.Sprintf("%d", unpublishedPort))
	t.Logf("PROOF X4 negative: nc -z port=%d (unpublished, below rangeAllocBase) exit=%d (want non-zero)", unpublishedPort, ncCode)
	if ncCode == 0 {
		t.Errorf("X4: unpublished port %d reached from host — negative control degenerate", unpublishedPort)
	}
}

func TestLiveRangeAllocBlockLifecycle(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)

	r := &Runtime{alloc: NewRangeAllocator()}
	spec := liveRangeSpec("ra-lc", 512)

	ref, err := r.CreateAndBoot(ctx, spec)
	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if h, herr := r.handle(cctx, ref); herr == nil {
			_ = h.Kill(cctx)
			_ = h.Remove(cctx)
		}
	})

	liveBlocks := dbOccupied(ctx, t)
	t.Logf("PROOF L1: daemon block mask (sandbox live): %v", liveBlocks)
	if !liveBlocks[0] {
		t.Errorf("L1: block 0 not occupied in daemon DB — unexpected")
	}

	r2 := &Runtime{alloc: NewRangeAllocator()}
	base2, aerr := r2.alloc.Allocate(ctx, "sra--ra-lc-other")
	if aerr != nil {
		t.Fatalf("second alloc while first live: %v", aerr)
	}
	t.Logf("PROOF L2: second fresh alloc (while first live) got base=%d (want != %d)", base2, rangeAllocBase)
	if base2 == rangeAllocBase {
		t.Errorf("L2 FAIL: second allocator returned same base=%d as live sandbox — collision", rangeAllocBase)
	}

	if h2, herr2 := r.handle(ctx, ref); herr2 == nil {
		_ = h2.Kill(ctx)
	}
	if err := r.Remove(ctx, ref); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	t.Logf("PROOF L3: sandbox removed")

	r3 := &Runtime{alloc: NewRangeAllocator()}
	base3, aerr3 := r3.alloc.Allocate(ctx, "sra--ra-lc-after-free")
	if aerr3 != nil {
		t.Fatalf("PROOF L3 FAIL: alloc after remove: %v", aerr3)
	}
	t.Logf("PROOF L3: after remove, fresh alloc got base=%d (want %d — DB entry removed)", base3, rangeAllocBase)
	if base3 != rangeAllocBase {
		t.Errorf("L3 FAIL: after remove, expected base=%d, got %d — DB entry not cleared", rangeAllocBase, base3)
	}
}

func TestLiveRangeAllocBootMetrics(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)

	r := &Runtime{alloc: NewRangeAllocator()}
	spec := liveRangeSpec("ra-metrics", 512)

	t0 := time.Now()
	ref, err := r.CreateAndBoot(ctx, spec)
	bootElapsed := time.Since(t0)
	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}
	CleanupSandbox(t, r, ref)

	t.Logf("PROOF M1: boot elapsed with 10k-port block = %s", bootElapsed)

	sb, err := r.connect(ctx, ref)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	metrics, merr := sb.Metrics(ctx)
	if merr != nil {
		t.Logf("Metrics: %v", merr)
	} else {
		t.Logf("PROOF M2: MemoryBytes=%d (%.1f MiB) MemoryLimitBytes=%d Uptime=%s",
			metrics.MemoryBytes, float64(metrics.MemoryBytes)/1024/1024,
			metrics.MemoryLimitBytes, metrics.Uptime)
	}
	if err := sb.Detach(ctx); err != nil {
		t.Logf("Detach: %v", err)
	}

	_, psOut := hostRun(ctx, "sh", "-c", "ps -eo rss,comm | grep libkrun | awk '{sum+=$1} END {printf \"%d\", sum}'")
	t.Logf("PROOF M3: host libkrun aggregate RSS = %s KiB", strings.TrimSpace(psOut))

	_, ssOut := hostRun(ctx, "sh", "-c",
		fmt.Sprintf("ss -tnlp | awk '$4 ~ /^127\\.0\\.0\\.1:/ {split($4,a,\":\"); p=a[2]+0; if(p>=%d && p<=%d) count++} END {print count+0}'",
			rangeAllocBase, uint32(rangeAllocBase)+uint32(rangeBlockSize)-1),
	)
	actual := strings.TrimSpace(ssOut)
	t.Logf("PROOF M4: host listeners in range %d:%d = %s (requested %d)",
		rangeAllocBase, uint32(rangeAllocBase)+uint32(rangeBlockSize)-1, actual, rangeBlockSize)
	if actual == "0" {
		t.Errorf("M4: zero listeners in host range — port publishing did not land")
	}
}
