//go:build live

package livemsb_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/IniZio/herdr-plugin-msb/internal/core/credmount"
	"github.com/IniZio/herdr-plugin-msb/internal/core/credmount/livemsb"
)

const (
	guestCredPath = "/mnt/creds.json"
	sentinelAC3   = "HERDR-AC3-SENTINEL-DO-NOT-USE"
	apiURL        = "https://api.anthropic.com/v1/messages"
)

func defaultStorePath(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	p := filepath.Join(home, ".config", "nexus3", "creds.json")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("credential store not found at %s: %v", p, err)
	}
	return p
}

func inodeOf(path string) (uint64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Sys().(*syscall.Stat_t).Ino, nil
}

func copyCredFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0600)
}

// guestAPIScript reads the access_token from guestPath and POSTs to Anthropic, printing "STATUS=<N>".
func guestAPIScript(guestPath string) string {
	body := `{"model":"claude-3-5-haiku-20241022","max_tokens":8,"messages":[{"role":"user","content":"ping"}]}`
	return fmt.Sprintf(
		`TOKEN=$(grep '"access_token"' %s | awk -F'"' '{print $4}'); `+
			`OUT=$(wget -q -O - --server-response `+
			`--post-data='%s' `+
			`--header="authorization: Bearer $TOKEN" `+
			`--header="anthropic-version: 2023-06-01" `+
			`--header="anthropic-beta: oauth-2025-04-20" `+
			`--header="content-type: application/json" `+
			`%s 2>&1); `+
			`CODE=$(echo "$OUT" | grep 'HTTP/' | tail -1 | awk '{print $2}'); `+
			`echo "STATUS=$CODE"`,
		guestPath, body, apiURL,
	)
}

func parseStatus(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "STATUS=") {
			return strings.TrimPrefix(line, "STATUS=")
		}
	}
	return "UNKNOWN"
}

func TestGuestWriteDirection(t *testing.T) {
	if os.Getenv("HERDR_MSB_LIVE") != "1" {
		t.Skip("HERDR_MSB_LIVE not set")
	}

	store := defaultStorePath(t)

	dir := t.TempDir()
	work := filepath.Join(dir, "creds.json")
	if err := copyCredFile(store, work); err != nil {
		t.Fatalf("copy store: %v", err)
	}

	inoBefore, err := inodeOf(work)
	if err != nil {
		t.Fatalf("inode before: %v", err)
	}
	bytesBefore, err := os.ReadFile(work)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	sb := livemsb.RequireSandbox(t, livemsb.SandboxOpts{
		Name:      "s16-ac3-write",
		Image:     "alpine",
		MemoryMiB: 512,
		VCPUs:     1,
		FileMounts: []livemsb.BindMount{
			{HostPath: work, GuestPath: guestCredPath},
		},
	})

	ctx := context.Background()
	script := fmt.Sprintf(
		`if echo '%s' > %s; then echo writeExit=0; else echo writeExit=1; fi`,
		sentinelAC3, guestCredPath,
	)
	stdout, stderr, code, err := sb.Sh(ctx, script)
	if err != nil {
		t.Fatalf("guest sh: %v", err)
	}
	t.Logf("MEASURE guest-write exit=%d stdout=%q stderr=%q",
		code, strings.TrimSpace(stdout), strings.TrimSpace(stderr))

	inoAfter, _ := inodeOf(work)
	bytesAfter, _ := os.ReadFile(work)

	writeLanded := !bytes.Equal(bytesBefore, bytesAfter)
	t.Logf("MEASURE inode-before=%d inode-after=%d inode-changed=%v",
		inoBefore, inoAfter, inoBefore != inoAfter)
	t.Logf("MEASURE guest-write-landed=%v", writeLanded)

	if writeLanded {
		t.Logf("FINDING s16-AC3(a): mount is read-write; guest write reached host inode; restoring working copy")
		if rerr := copyCredFile(store, work); rerr != nil {
			t.Errorf("restore working copy after guest write: %v", rerr)
		}
	} else {
		t.Logf("FINDING s16-AC3(b): mount blocked guest write; host bytes unchanged")
	}
}

func TestConcurrentMountContention(t *testing.T) {
	if os.Getenv("HERDR_MSB_LIVE") != "1" {
		t.Skip("HERDR_MSB_LIVE not set")
	}
	if os.Getenv("HERDR_MSB_LIVE_PAIR") != "1" {
		t.Skip("HERDR_MSB_LIVE_PAIR not set")
	}

	store := defaultStorePath(t)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	backup := filepath.Join(home, ".config", "nexus3", "creds.json.ac6-backup")
	if err := copyCredFile(store, backup); err != nil {
		t.Fatalf("backup store: %v", err)
	}
	t.Cleanup(func() { os.Remove(backup) })

	dir := t.TempDir()
	work := filepath.Join(dir, "creds.json")
	if err := copyCredFile(store, work); err != nil {
		t.Fatalf("copy store: %v", err)
	}

	ctx := context.Background()

	sb1 := livemsb.RequireSandbox(t, livemsb.SandboxOpts{
		Name:      "s16-contention-a",
		Image:     "alpine",
		MemoryMiB: 1024,
		VCPUs:     1,
		FileMounts: []livemsb.BindMount{{HostPath: work, GuestPath: guestCredPath}},
	})
	sb2 := livemsb.RequireSandbox(t, livemsb.SandboxOpts{
		Name:      "s16-contention-b",
		Image:     "alpine",
		MemoryMiB: 1024,
		VCPUs:     1,
		FileMounts: []livemsb.BindMount{{HostPath: work, GuestPath: guestCredPath}},
	})
	// Belt-and-braces: explicit removes in addition to RequireSandbox t.Cleanup.
	defer func() {
		_ = sb1.Remove(context.Background())
		_ = sb2.Remove(context.Background())
	}()

	// Host-side refresh in-place: SaveInPlace rewrites same inode so both guests see the new token.
	refresher := credmount.Refresher{}
	updated, rerr := refresher.RefreshFile(ctx, work, true)
	if rerr != nil {
		t.Logf("MEASURE host-refresh-err=%v", rerr)
		t.Skipf("host refresh failed: %v", rerr)
	}
	t.Logf("MEASURE host-refresh-ok expires_at=%s", updated.ExpiresAt)

	// Persist new token to real store so user session stays valid after this test.
	if serr := credmount.Save(store, updated); serr != nil {
		t.Errorf("persist refresh to real store: %v", serr)
	}

	script := guestAPIScript(guestCredPath)

	out1, _, code1, err1 := sb1.Sh(ctx, script)
	status1 := parseStatus(out1)
	t.Logf("MEASURE sb1 exit=%d status=%s err=%v", code1, status1, err1)

	out2, _, code2, err2 := sb2.Sh(ctx, script)
	status2 := parseStatus(out2)
	t.Logf("MEASURE sb2 exit=%d status=%s err=%v", code2, status2, err2)

	both401 := status1 == "401" && status2 == "401"
	t.Logf("MEASURE contention sb1=%s sb2=%s both-401=%v", status1, status2, both401)

	// Contention invariant: both-401 means neither side has a valid token; a broker is required.
	if both401 {
		t.Errorf("s16-AC6 CONTENTION DEMONSTRATED: both sandboxes got 401 after host refresh; shared mount is insufficient without a broker")
	}
}
