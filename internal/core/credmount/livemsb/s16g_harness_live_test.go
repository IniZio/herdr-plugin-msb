//go:build live

package livemsb_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IniZio/herdr-plugin-msb/internal/core/credmount/livemsb"
)

// D-2 WAIVER: stub token endpoint; synthetic credentials; HERDR_MSB_LIVE_REFRESH must NOT be set.

func installCurl(t *testing.T, sb *livemsb.Sandbox) {
	t.Helper()
	out, _, code, err := sb.Sh(t.Context(), "apk add --no-cache curl >/dev/null 2>&1 && echo OK")
	if err != nil || code != 0 {
		t.Fatalf("apk add curl: err=%v code=%d out=%s", err, code, out)
	}
}

func stubAndRefresh(credPath, newAccess, newRefresh string) string {
	stubJSON := `{"access_token":"` + newAccess + `","refresh_token":"` + newRefresh + `"}`
	refresh := guestRefreshScript("http://127.0.0.1:18080/token", "s16g-client", credPath)
	return "STUB_JSON='" + stubJSON + "'\n" +
		"(printf 'HTTP/1.1 200 OK\\r\\nContent-Type: application/json\\r\\n\\r\\n'; printf '%s' \"$STUB_JSON\") | nc -l -p 18080 &\n" +
		"sleep 1\n" +
		refresh
}

func TestAC3GDirMountRefreshScript(t *testing.T) {
	if os.Getenv("HERDR_MSB_LIVE") != "1" {
		t.Skip("HERDR_MSB_LIVE not set")
	}

	hostDir := t.TempDir()
	credFile := filepath.Join(hostDir, ".credentials.json")
	if err := os.WriteFile(credFile, []byte(syntheticNestedCreds("s16g-tok-A-dir", "s16g-ref-A-dir")), 0o600); err != nil {
		t.Fatal(err)
	}

	sb := livemsb.RequireSandbox(t, livemsb.SandboxOpts{
		Name:      "s16-g-dir",
		Image:     "alpine",
		MemoryMiB: 512,
		VCPUs:     1,
		DirMounts: []livemsb.BindMount{{HostPath: hostDir, GuestPath: "/mnt/claude-dir"}},
	})
	installCurl(t, sb)

	const newAccess = "s16g-tok-B-dir"
	out, errOut, code, err := sb.Sh(t.Context(), stubAndRefresh("/mnt/claude-dir/.credentials.json", newAccess, "s16g-ref-B-dir"))
	t.Logf("MEASURE AC3G dir stdout: %s", strings.TrimSpace(out))
	t.Logf("MEASURE AC3G dir stderr: %s", strings.TrimSpace(errOut))
	t.Logf("MEASURE AC3G dir exit=%d err=%v", code, err)

	if err != nil || code != 0 || !strings.Contains(out, "REFRESH_OK") {
		t.Fatalf("AC3G dir: script failed: out=%s err=%s exit=%d", out, errOut, code)
	}

	hostBytes, err := os.ReadFile(credFile)
	if err != nil {
		t.Fatalf("read host file: %v", err)
	}
	if !strings.Contains(string(hostBytes), newAccess) {
		t.Fatalf("AC3G dir: host file does not contain new token: %q", string(hostBytes))
	}
	t.Logf("MEASURE AC3G dir: host_file_updated=true host_contains_%s=true", newAccess)
}

func TestAC3GFileMountRefreshScript(t *testing.T) {
	if os.Getenv("HERDR_MSB_LIVE") != "1" {
		t.Skip("HERDR_MSB_LIVE not set")
	}

	hostDir := t.TempDir()
	credFile := filepath.Join(hostDir, ".credentials.json")
	if err := os.WriteFile(credFile, []byte(syntheticNestedCreds("s16g-tok-A-file", "s16g-ref-A-file")), 0o600); err != nil {
		t.Fatal(err)
	}

	sb := livemsb.RequireSandbox(t, livemsb.SandboxOpts{
		Name:      "s16-g-file",
		Image:     "alpine",
		MemoryMiB: 512,
		VCPUs:     1,
		FileMounts: []livemsb.BindMount{{HostPath: credFile, GuestPath: "/mnt/.credentials.json"}},
	})
	installCurl(t, sb)

	out, errOut, code, err := sb.Sh(t.Context(), stubAndRefresh("/mnt/.credentials.json", "s16g-tok-B-file", "s16g-ref-B-file"))
	t.Logf("MEASURE AC3G file stdout: %s", strings.TrimSpace(out))
	t.Logf("MEASURE AC3G file stderr: %s", strings.TrimSpace(errOut))
	t.Logf("MEASURE AC3G file exit=%d err=%v", code, err)

	if err == nil && code == 0 && strings.Contains(out, "REFRESH_OK") {
		t.Fatalf("AC3G file: UNEXPECTEDLY succeeded — rename did not fail EBUSY")
	}
	combined := strings.ToLower(out + " " + errOut)
	if !strings.Contains(combined, "busy") {
		t.Fatalf("AC3G file: expected EBUSY on file-mount rename, got exit=%d out=%q stderr=%q", code, out, errOut)
	}

	hostBytes, err := os.ReadFile(credFile)
	if err != nil {
		t.Fatalf("read host file: %v", err)
	}
	if strings.Contains(string(hostBytes), "s16g-tok-B-file") {
		t.Fatalf("AC3G file: host file was updated — rename succeeded unexpectedly")
	}
	t.Logf("MEASURE AC3G file: rename_failed=true host_unchanged=true exit=%d", code)
}
