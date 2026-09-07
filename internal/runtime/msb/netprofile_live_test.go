package msb

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

func TestLiveNetProfileEgress(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()

	spec := coreruntime.SandboxSpec{
		Project:   "",
		Name:      fmt.Sprintf("s03-egress-%d", time.Now().UnixNano()),
		ImageRef:  "alpine",
		VCPUs:     1,
		MemoryMiB: 512,
		Motive:    "nexus3-microsandbox-pivot",
	}
	ref, err := r.CreateAndBoot(ctx, spec)
	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}
	CleanupSandbox(t, r, ref)

	for _, tool := range []string{"wget", "nc"} {
		res, err := r.Exec(ctx, ref, coreruntime.ExecRequest{Argv: []string{"which", tool}})
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("tool %q absent from alpine guest (exit %d, err %v); negative probes would be vacuous", tool, res.ExitCode, err)
		}
	}

	run := func(argv []string) (int32, string, string) {
		var out, errBuf bytes.Buffer
		res, err := r.Exec(ctx, ref, coreruntime.ExecRequest{Argv: argv, Stdout: &out, Stderr: &errBuf})
		var code int32 = -1
		if err == nil {
			code = res.ExitCode
		}
		return code, out.String(), errBuf.String()
	}

	code1, out1, err1 := run([]string{"wget", "-T5", "-q", "-O-", "https://example.com/"})
	t.Logf("p1 hostname exit=%d stdout=%q stderr=%q", code1, out1, err1)
	if code1 == 0 {
		t.Errorf("p1: wget https://example.com/ exited 0 — default-deny not enforced on hostname")
	}

	code2, out2, err2 := run([]string{"nc", "-z", "-w5", "104.20.23.154", "443"})
	t.Logf("p2 raw-ip-tcp exit=%d stdout=%q stderr=%q", code2, out2, err2)
	if code2 == 0 {
		t.Errorf("p2: nc -z 104.20.23.154 443 exited 0 — raw-IP egress reachable, enforcement is not connection-level")
	}

	code3, out3, err3 := run([]string{"nc", "-z", "-w10", "api.anthropic.com", "443"})
	t.Logf("p3 anthropic exit=%d stdout=%q stderr=%q", code3, out3, err3)
	if code3 != 0 && !strings.Contains(out3+err3, "open") {
		t.Errorf("p3: nc -z api.anthropic.com 443 exit=%d — API endpoint unreachable; credential mount slice requires it", code3)
	}
}
