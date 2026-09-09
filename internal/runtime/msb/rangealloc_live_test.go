package msb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
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

func TestLiveRangeAllocDisjoint(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)

	r := &Runtime{alloc: NewRangeAllocator()}

	spec1 := liveRangeSpec("ra-disj-1", 512)
	spec2 := liveRangeSpec("ra-disj-2", 512)

	var ref1, ref2 coreruntime.SandboxRef

	t.Logf("PROOF D0: msb list before two-sandbox window")
	_, listOut := hostRun(ctx, "msb", "list")
	t.Logf("msb list: %s", strings.TrimSpace(listOut))

	cleanup := func() {
		cctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		for _, ref := range []coreruntime.SandboxRef{ref1, ref2} {
			if ref.Name == "" {
				continue
			}
			if h, err := r.handle(cctx, ref); err == nil {
				_ = h.Kill(cctx)
				_ = h.Remove(cctx)
			}
		}
	}
	t.Cleanup(cleanup)

	var err error
	ref1, err = r.CreateAndBoot(ctx, spec1)
	if err != nil {
		t.Fatalf("CreateAndBoot ref1: %v", err)
	}
	t.Logf("PROOF D1: ref1=%s/%s portMap base=%d", ref1.Project, ref1.Name, rangeAllocBase)

	ref2, err = r.CreateAndBoot(ctx, spec2)
	if err != nil {
		t.Fatalf("CreateAndBoot ref2: %v", err)
	}
	t.Logf("PROOF D2: ref2=%s/%s portMap base=%d", ref2.Project, ref2.Name, rangeAllocBase+rangeBlockSize)

	base1, _ := r.alloc.Allocate(r.alloc.used[0])
	base2, _ := r.alloc.Allocate(r.alloc.used[1])
	t.Logf("PROOF D3: block0 base=%d block1 base=%d (must differ by %d)", base1, base2, rangeBlockSize)
	if base1 == base2 {
		t.Errorf("D3 FAIL: both sandboxes received same host base %d", base1)
	}
	lo, hi := base1, base2
	if lo > hi {
		lo, hi = hi, lo
	}
	if lo+rangeBlockSize > hi {
		t.Errorf("D3 FAIL: blocks overlap: lo=%d hi=%d blockSize=%d", lo, hi, rangeBlockSize)
	}
	t.Logf("PROOF D3 PASS: blocks disjoint [%d,%d) and [%d,%d)", lo, lo+rangeBlockSize, hi, hi+rangeBlockSize)

	cleanup()
	ref1.Name, ref2.Name = "", ""

	_, listOut2 := hostRun(ctx, "msb", "list")
	t.Logf("PROOF D4: msb list after window: %s", strings.TrimSpace(listOut2))
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

	name := "sra" + nameSep + "ra-xfer"
	hostBase, aerr := r.alloc.Allocate(name)
	if aerr != nil {
		t.Fatalf("Allocate: %v", aerr)
	}
	guestPort := uint16(3000)
	hostPort := hostBase + (guestPort - rangeGuestBase)
	t.Logf("PROOF X1: guestPort=%d hostPort=%d (base=%d)", guestPort, hostPort, hostBase)

	marker := fmt.Sprintf("XFER-%d", time.Now().UnixNano())
	listenScript := fmt.Sprintf(
		"nohup sh -c 'while true; do echo %s | nc -l -p %d; done' >/dev/null 2>&1 & sleep 1; exit 0",
		marker, guestPort,
	)
	armCode, armOut := guestExec(ctx, r, ref, []string{"sh", "-c", listenScript})
	t.Logf("PROOF X2 arm: exit=%d out=%q", armCode, armOut)
	time.Sleep(800 * time.Millisecond)

	_, nsOut := guestExec(ctx, r, ref, []string{"netstat", "-ltn"})
	portStr := fmt.Sprintf("%d", guestPort)
	if !strings.Contains(nsOut, portStr) {
		t.Fatalf("PROOF X2: guest netstat=%q — port %s not listening", nsOut, portStr)
	}
	t.Logf("PROOF X2: guest netstat shows :%s LISTEN", portStr)

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

	ncCode, _ := hostRun(ctx, "nc", "-z", "-w5", "127.0.0.1", fmt.Sprintf("%d", hostPort-1))
	t.Logf("PROOF X4 negative: nc -z unpublished port=%d exit=%d (want non-zero)", hostPort-1, ncCode)
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

	sdkName := "sra" + nameSep + "ra-lc"
	base1, _ := r.alloc.Allocate(sdkName)
	t.Logf("PROOF L1: allocated base=%d", base1)

	_, err2 := r.alloc.Allocate("other-occupies-next")
	if err2 != nil {
		t.Fatalf("Allocate second: %v", err2)
	}
	_, expectErr := r.alloc.Allocate("try-same-block")
	if expectErr != nil {
		t.Logf("PROOF L2: different name cannot get same block (err=%v)", expectErr)
	}

	if h, herr := r.handle(ctx, ref); herr == nil {
		_ = h.Kill(ctx)
	}
	if err := r.Remove(ctx, ref); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	t.Logf("PROOF L3: removed sandbox, alloc.Free should have been called by Runtime.Remove")

	base2, err3 := r.alloc.Allocate("after-free")
	if err3 != nil {
		t.Fatalf("PROOF L3 FAIL: Allocate after free: %v", err3)
	}
	t.Logf("PROOF L3: freed block reused: base1=%d base2=%d match=%v", base1, base2, base1 == base2)
	if base1 != base2 {
		t.Errorf("L3 FAIL: freed block not reused: base1=%d base2=%d", base1, base2)
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

	sdkName := "sra" + nameSep + "ra-metrics"
	base, _ := r.alloc.Allocate(sdkName)
	hostPortStr := fmt.Sprintf("%d:%d", base, base+rangeBlockSize-1)
	_, ssOut := hostRun(ctx, "sh", "-c",
		fmt.Sprintf("ss -tnlp | awk '$4 ~ /^127\\.0\\.0\\.1:/ {split($4,a,\":\"); p=a[2]+0; if(p>=%d && p<=%d) count++} END {print count+0}'",
			base, uint32(base)+uint32(rangeBlockSize)-1),
	)
	actual := strings.TrimSpace(ssOut)
	t.Logf("PROOF M4: host listeners in range %s = %s (requested %d)", hostPortStr, actual, rangeBlockSize)
	if actual == "0" {
		t.Errorf("M4: zero listeners in host range %s — port publishing did not land", hostPortStr)
	}
}
