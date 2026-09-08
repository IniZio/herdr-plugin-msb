package msb

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

func TestLiveRunEphemeralEgressContained(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)
	r := New()

	newSpec := func() coreruntime.SandboxSpec {
		return coreruntime.SandboxSpec{
			Project:   "",
			Name:      fmt.Sprintf("s19-eph-%d", time.Now().UnixNano()),
			ImageRef:  "alpine",
			VCPUs:     1,
			MemoryMiB: 512,
			Motive:    "nexus3-microsandbox-pivot",
		}
	}

	probe := func(label string, argv []string) (int32, string, string) {
		var out, errBuf bytes.Buffer
		res, err := r.RunEphemeral(ctx, newSpec(), coreruntime.ExecRequest{
			Argv:   argv,
			Stdout: &out,
			Stderr: &errBuf,
		})
		code := int32(-1)
		if err == nil {
			code = res.ExitCode
		}
		t.Logf("%s exit=%d err=%v stdout=%q stderr=%q", label, code, err, out.String(), errBuf.String())
		return code, out.String(), errBuf.String()
	}

	code0, out0, err0 := probe("p0 tool-guard", []string{"sh", "-c", "command -v wget && command -v nc"})
	if code0 != 0 {
		t.Fatalf("p0: wget/nc absent from alpine guest (exit %d, stdout %q, stderr %q); negative probes would be vacuous", code0, out0, err0)
	}

	code1, _, _ := probe("p1 hostname", []string{"wget", "-T5", "-q", "-O-", "https://example.com/"})
	if code1 == 0 {
		t.Errorf("p1: wget https://example.com/ exited 0 through RunEphemeral — default-deny not enforced on hostname")
	}

	code2, _, _ := probe("p2 raw-ip-tcp", []string{"nc", "-z", "-w5", "104.20.23.154", "443"})
	if code2 == 0 {
		t.Errorf("p2: nc -z 104.20.23.154 443 exited 0 through RunEphemeral — raw-IP egress reachable, enforcement is not connection-level")
	}

	code3, out3, err3 := probe("p3 anthropic", []string{"nc", "-z", "-w10", "api.anthropic.com", "443"})
	if code3 != 0 && !strings.Contains(out3+err3, "open") {
		t.Errorf("p3: nc -z api.anthropic.com 443 exit=%d — positive control unreachable; without it a network-less guest would pass this test vacuously", code3)
	}
}
