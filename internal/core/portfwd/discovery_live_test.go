//go:build live

package portfwd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

func silentRun(name string, args ...string) { _ = exec.Command(name, args...).Run() }

func TestLiveDiscoveryProcNet(t *testing.T) {
	if os.Getenv("HERDR_MSB_LIVE") != "1" {
		t.Skip("live test: set HERDR_MSB_LIVE=1")
	}
	ctx := context.Background()
	rt := msbCLI{t: t}
	const sbName = "s24-disc"

	mustRun(t, "msb", "create", "-n", sbName, "-m", "512M", "alpine")
	t.Cleanup(func() { silentRun("msb", "remove", "--force", sbName) })

	var ref runtime.SandboxRef
	for i := 0; i < 30; i++ {
		refs, err := rt.List(ctx)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, r := range refs {
			if r.Name == sbName && r.Status == runtime.SandboxStatusRunning {
				ref = r
			}
		}
		if ref.Name != "" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if ref.Name == "" {
		t.Fatalf("sandbox %s did not reach running", sbName)
	}
	t.Logf("SANDBOX READY: id=%q status=%q", ref.ID, ref.Status)

	for _, port := range []int{80, 3000, 50000} {
		argv := fmt.Sprintf("nc -l -p %d >/dev/null 2>&1 & echo $! >/tmp/nc%d.pid", port, port)
		if _, err := rt.Exec(ctx, ref, runtime.ExecRequest{Argv: []string{"sh", "-c", argv}}); err != nil {
			t.Fatalf("start listener %d: %v", port, err)
		}
	}
	time.Sleep(500 * time.Millisecond)

	d := &Discoverer{RT: rt}

	ls1, err := d.DiscoverOne(ctx, ref)
	if err != nil {
		t.Fatalf("discover positive: %v", err)
	}
	t.Logf("POSITIVE DISCOVERY: %+v", ls1)

	fr := FilterListeners(ls1, nil)
	t.Logf("FILTER RESULT: Forwardable=%+v OutOfRange=%+v Reserved=%+v", fr.Forwardable, fr.OutOfRange, fr.Reserved)

	found3000 := false
	for _, l := range ls1 {
		if l.Port == 3000 {
			found3000 = true
		}
	}
	if !found3000 {
		t.Fatalf("POSITIVE FAIL: port 3000 not discovered; got %+v", ls1)
	}

	has3000Fwd, has80Res, has50000OOR := false, false, false
	for _, l := range fr.Forwardable {
		if l.Port == 3000 {
			has3000Fwd = true
		}
	}
	for _, l := range fr.Reserved {
		if l.Port == 80 {
			has80Res = true
		}
	}
	for _, l := range fr.OutOfRange {
		if l.Port == 50000 {
			has50000OOR = true
		}
	}
	if !has3000Fwd {
		t.Errorf("AC2 Forwardable FAIL: 3000 not in Forwardable; filter=%+v", fr)
	}
	if !has80Res {
		t.Errorf("AC2 Reserved FAIL: 80 not in Reserved; filter=%+v", fr)
	}
	if !has50000OOR {
		t.Errorf("AC4 OutOfRange FAIL: 50000 not in OutOfRange; filter=%+v", fr)
	}

	if _, err := rt.Exec(ctx, ref, runtime.ExecRequest{
		Argv: []string{"sh", "-c", "kill $(cat /tmp/nc3000.pid) 2>/dev/null; true"},
	}); err != nil {
		t.Fatalf("kill listener 3000: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	ls2, err := d.DiscoverOne(ctx, ref)
	if err != nil {
		t.Fatalf("discover negative: %v", err)
	}
	t.Logf("NEGATIVE DISCOVERY (3000 stopped): %+v", ls2)

	for _, l := range ls2 {
		if l.Port == 3000 {
			t.Fatalf("NEGATIVE FAIL: port 3000 still reported after stop; got %+v", ls2)
		}
	}
	t.Log("AC1 PASS: port 3000 absent after listener stop")
}
