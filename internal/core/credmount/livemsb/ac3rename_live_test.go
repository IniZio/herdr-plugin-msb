//go:build live

package livemsb_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IniZio/herdr-plugin-msb/internal/core/credmount/livemsb"
)

func TestAC3DirMountRenameSucceeds(t *testing.T) {
	if os.Getenv("HERDR_MSB_LIVE") != "1" {
		t.Skip("HERDR_MSB_LIVE not set")
	}

	hostDir := t.TempDir()
	credFile := filepath.Join(hostDir, ".credentials.json")
	if err := os.WriteFile(credFile, []byte(syntheticNestedCreds("tok-before", "ref-before")), 0o600); err != nil {
		t.Fatal(err)
	}

	sb := livemsb.RequireSandbox(t, livemsb.SandboxOpts{
		Name:      "s16-ac3-dir",
		Image:     "alpine",
		MemoryMiB: 512,
		VCPUs:     1,
		DirMounts: []livemsb.BindMount{
			{HostPath: hostDir, GuestPath: "/mnt/claude-creds"},
		},
	})

	ctx := context.Background()
	script := `
TMPF=/mnt/claude-creds/.credentials.json.tmpXXX
printf '{"claudeAiOauth":{"accessToken":"tok-after-dir","refreshToken":"r","expiresAt":9999999999999,"scopes":[]}}' > "$TMPF"
mv "$TMPF" /mnt/claude-creds/.credentials.json
RENAME_EXIT=$?
echo "rename_exit=$RENAME_EXIT"
`
	stdout, stderr, code, err := sb.Sh(ctx, script)
	if err != nil {
		t.Fatalf("sh error: %v", err)
	}
	t.Logf("stdout=%q stderr=%q code=%d", stdout, stderr, code)

	renameExit := field(stdout, "rename_exit")
	if renameExit != "0" {
		t.Fatalf("dir-mount rename failed: rename_exit=%q (expected 0)", renameExit)
	}

	hostBytes, err := os.ReadFile(credFile)
	if err != nil {
		t.Fatalf("read host file after rename: %v", err)
	}
	if !strings.Contains(string(hostBytes), "tok-after-dir") {
		t.Fatalf("host file not updated: contents=%q", string(hostBytes))
	}
	t.Logf("MEASURE dir-mount rename: propagated=true host_contains_tok-after-dir=true")
}

func TestAC3FileMountRenameEBUSY(t *testing.T) {
	if os.Getenv("HERDR_MSB_LIVE") != "1" {
		t.Skip("HERDR_MSB_LIVE not set")
	}

	hostDir := t.TempDir()
	credFile := filepath.Join(hostDir, ".credentials.json")
	origCreds := syntheticNestedCreds("tok-file-orig", "ref-file-orig")
	if err := os.WriteFile(credFile, []byte(origCreds), 0o600); err != nil {
		t.Fatal(err)
	}

	sb := livemsb.RequireSandbox(t, livemsb.SandboxOpts{
		Name:      "s16-ac3-file",
		Image:     "alpine",
		MemoryMiB: 512,
		VCPUs:     1,
		FileMounts: []livemsb.BindMount{
			{HostPath: credFile, GuestPath: "/mnt/.credentials.json"},
		},
	})

	ctx := context.Background()
	script := `
TMPF=/mnt/.credentials.json.tmp$$
printf '{"claudeAiOauth":{"accessToken":"tok-after-file","refreshToken":"r","expiresAt":9999999999999,"scopes":[]}}' > "$TMPF"
mv "$TMPF" /mnt/.credentials.json
RENAME_EXIT=$?
rm -f "$TMPF"
echo "rename_exit=$RENAME_EXIT"
`
	stdout, stderr, code, err := sb.Sh(ctx, script)
	if err != nil {
		t.Fatalf("sh error: %v", err)
	}
	t.Logf("stdout=%q stderr=%q code=%d", stdout, stderr, code)

	hostBytes, err := os.ReadFile(credFile)
	if err != nil {
		t.Fatalf("read host file after rename attempt: %v", err)
	}
	hostContents := string(hostBytes)

	renameExit := field(stdout, "rename_exit")
	t.Logf("MEASURE file-mount rename: rename_exit=%q host_digest=%s", renameExit, short(digest(hostContents)))

	if strings.Contains(hostContents, "tok-after-file") {
		t.Fatalf("AC3 UNEXPECTED: file-mount rename succeeded — host file now contains tok-after-file")
	}
	if renameExit == "0" {
		t.Fatalf("AC3 UNEXPECTED: file-mount rename succeeded (exit=0) — expected EBUSY/EXDEV")
	}
}
