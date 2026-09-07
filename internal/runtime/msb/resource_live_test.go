package msb

import (
	"strconv"
	"strings"
	"testing"

	msbsdk "github.com/superradcompany/microsandbox/sdk/go"
)

func TestLiveResourceFields(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()

	spec := LiveSpec("resource")
	spec.VCPUs = 2
	spec.MemoryMiB = 1024

	ref, err := r.CreateAndBoot(ctx, spec)
	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}
	CleanupSandbox(t, r, ref)

	sb, err := r.connect(ctx, ref)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	out, err := sb.Exec(ctx, "sh", []string{"-c", "nproc"})
	if err != nil {
		t.Fatalf("Exec nproc: %v", err)
	}
	ncpu := strings.TrimSpace(out.Stdout())
	t.Logf("nproc = %s", ncpu)
	if ncpu != "2" {
		t.Errorf("nproc = %q, want 2", ncpu)
	}

	out, err = sb.Exec(ctx, "sh", []string{"-c", "grep MemTotal /proc/meminfo"})
	if err != nil {
		t.Fatalf("Exec meminfo: %v", err)
	}
	memLine := strings.TrimSpace(out.Stdout())
	t.Logf("MemTotal line: %s", memLine)
	fields := strings.Fields(memLine)
	if len(fields) < 2 {
		t.Fatalf("unexpected MemTotal line: %q", memLine)
	}
	memKiB, err := strconv.Atoi(fields[1])
	if err != nil {
		t.Fatalf("parse MemTotal kB: %v", err)
	}
	if memKiB <= 786432 {
		t.Errorf("MemTotal = %d KiB (%.0f MiB), want > 786432 KiB (768 MiB); near-512 MiB means MemoryMiB field not applied", memKiB, float64(memKiB)/1024)
	}
	if memKiB > 1048576 {
		t.Errorf("MemTotal = %d KiB (%.0f MiB), want <= 1048576 KiB (1024 MiB)", memKiB, float64(memKiB)/1024)
	}

	if metrics, merr := sb.Metrics(ctx); merr != nil {
		t.Logf("Metrics: %v", merr)
	} else {
		t.Logf("Metrics: CPUPercent=%.2f MemoryBytes=%d MemoryLimitBytes=%d Uptime=%s",
			metrics.CPUPercent, metrics.MemoryBytes, metrics.MemoryLimitBytes, metrics.Uptime)
	}

	if err := sb.Detach(ctx); err != nil {
		t.Fatalf("Detach: %v", err)
	}

	h, err := r.handle(ctx, ref)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	cfg, err := h.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	t.Logf("cfg.CPUs=%d cfg.MemoryMiB=%d", cfg.CPUs, cfg.MemoryMiB)
	if cfg.CPUs != 2 {
		t.Errorf("cfg.CPUs = %d, want 2", cfg.CPUs)
	}
	if cfg.MemoryMiB != 1024 {
		t.Errorf("cfg.MemoryMiB = %d, want 1024", cfg.MemoryMiB)
	}

	_, modErr := h.Modify(ctx, msbsdk.ModifyOptions{MemoryMiB: 1536})
	if modErr != nil {
		t.Logf("Modify(MemoryMiB=1536): %v (optional, non-fatal)", modErr)
		return
	}
	sb2, err := r.connect(ctx, ref)
	if err != nil {
		t.Logf("connect after Modify: %v (optional, non-fatal)", err)
		return
	}
	out2, err := sb2.Exec(ctx, "sh", []string{"-c", "grep MemTotal /proc/meminfo"})
	if err != nil {
		t.Logf("Exec meminfo after Modify: %v (optional, non-fatal)", err)
		_ = sb2.Detach(ctx)
		return
	}
	t.Logf("MemTotal after Modify(1536 MiB): %s", strings.TrimSpace(out2.Stdout()))
	_ = sb2.Detach(ctx)
}
