//go:build live

package portfwd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

func sshCheckRaw(controlPath, host string) (string, int) {
	cmd := exec.Command("ssh", "-O", "check", "-o", "ControlPath="+controlPath, host)
	out, _ := cmd.CombinedOutput()
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	return strings.TrimSpace(string(out)), code
}

func TestLiveTeardownTwoHostAfterPublish(t *testing.T) {
	if os.Getenv("HERDR_MSB_LIVE") != "1" {
		t.Skip("live test: set HERDR_MSB_LIVE=1")
	}
	host := os.Getenv("HERDR_MSB_LIVE_LAPTOP")
	if host == "" {
		t.Skip("live test: set HERDR_MSB_LIVE_LAPTOP=user@host")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	const port uint16 = 45461
	fw := &Forwarder{
		ControlPath: filepath.Join(t.TempDir(), "cm"),
		SSHHost:     host,
		Run:         OSRunner,
	}
	if err := fw.EnsureMaster(ctx); err != nil {
		t.Fatalf("EnsureMaster: %v", err)
	}
	t.Cleanup(func() {
		_, _, _, _ = fw.Run(context.Background(), []string{"ssh", "-O", "exit", "-o", "ControlPath=" + fw.ControlPath, host})
	})

	before, err := fw.Present(ctx, port)
	if err != nil {
		t.Fatalf("Present before: %v", err)
	}
	t.Logf("PROOF T1: local listening socket for :%d before apply = %v (want false)", port, before)
	if before {
		t.Fatalf("T1: port %d already forwarded — control degenerate", port)
	}

	ref := runtime.SandboxRef{ID: "s17-teardown", Name: "s17-teardown", Status: runtime.SandboxStatusRunning}
	mgr := NewManager(fw)
	if err := mgr.Reconcile(ctx, []Listener{{Port: port, BindAddr: "127.0.0.1", Sandbox: ref}}); err != nil {
		t.Fatalf("Reconcile apply: %v", err)
	}
	applied, err := fw.Present(ctx, port)
	if err != nil {
		t.Fatalf("Present after apply: %v", err)
	}
	t.Logf("PROOF T2: local listening socket for :%d after apply = %v (want true)", port, applied)
	if !applied {
		t.Fatalf("T2: forward not visible in the local listening socket")
	}

	checkOutBefore, checkCodeBefore := sshCheckRaw(fw.ControlPath, host)
	if err := mgr.TeardownSandbox(ctx, ref); err != nil {
		t.Fatalf("TeardownSandbox: %v", err)
	}
	gone, err := fw.Present(ctx, port)
	if err != nil {
		t.Fatalf("Present after teardown: %v", err)
	}
	t.Logf("PROOF T3: local listening socket for :%d after TeardownSandbox = %v (want false)", port, gone)
	if gone {
		t.Fatalf("T3: forward still present after teardown")
	}

	checkOutAfter, checkCodeAfter := sshCheckRaw(fw.ControlPath, host)
	t.Logf("PROOF T4 vacuity control: ssh -O check before teardown = %q exit=%d; after teardown = %q exit=%d — identical, so -O check cannot see a per-forward change",
		checkOutBefore, checkCodeBefore, checkOutAfter, checkCodeAfter)

	cancelUnknown := fw.Cancel(ctx, 45462)
	t.Logf("PROOF T5 vacuity control: Cancel of a never-applied forward returned err=%v (ssh -O cancel exits 0 regardless)", cancelUnknown)

	if err := mgr.Reconcile(ctx, []Listener{{Port: port, BindAddr: "127.0.0.1", Sandbox: ref}}); err != nil {
		t.Fatalf("Reconcile re-apply: %v", err)
	}
	reapplied, err := fw.Present(ctx, port)
	if err != nil {
		t.Fatalf("Present after re-apply: %v", err)
	}
	t.Logf("PROOF T6: re-applied forward present = %v (want true)", reapplied)
	if !reapplied {
		t.Fatalf("T6: re-apply did not restore the forward")
	}

	stopped := ref
	stopped.Status = runtime.SandboxStatusStopped
	if err := mgr.Reconcile(ctx, []Listener{{Port: port, BindAddr: "127.0.0.1", Sandbox: stopped}}); err != nil {
		t.Fatalf("Reconcile stopped: %v", err)
	}
	afterStop, err := fw.Present(ctx, port)
	if err != nil {
		t.Fatalf("Present after stop reconcile: %v", err)
	}
	t.Logf("PROOF T7: local listening socket for :%d after the sandbox reports stopped = %v (want false)", port, afterStop)
	if afterStop {
		t.Fatalf("T7: forward survived a stopped sandbox")
	}
}
