//go:build live

package livemsb_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IniZio/herdr-plugin-msb/internal/core/credmount"
	"github.com/IniZio/herdr-plugin-msb/internal/core/credmount/livemsb"
)

const (
	guestCredsPath = "/mnt/creds.json"
	guestProbePath = "/mnt/probe.txt"
	backupPath     = "/tmp/claude-1003/s16-creds-backup.json"
)

func hostCredsPath(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home dir: %v", err)
	}
	return filepath.Join(home, ".config", "nexus3", "creds.json")
}

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func short(d string) string {
	if len(d) > 12 {
		return d[:12]
	}
	return d
}

func backupStore(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store for backup: %v", err)
	}
	if err := os.WriteFile(backupPath, data, 0o600); err != nil {
		t.Fatalf("write backup %s: %v", backupPath, err)
	}
	t.Logf("MEASURE credential-store backup: path=%s bytes=%d mode=0600", backupPath, len(data))
}

func apiCallScript() string {
	return `set -u
TOKEN=$(sed -n 's/.*"access_token"[^"]*"\([^"]*\)".*/\1/p' ` + guestCredsPath + `)
if [ -z "$TOKEN" ]; then echo "status=000 reason=no-token-in-mount"; exit 0; fi
CODE=$(curl -sS -o /tmp/resp.body -w '%{http_code}' -A 'curl/8.5.0' \
  -H "authorization: Bearer $TOKEN" \
  -H 'anthropic-version: 2023-06-01' \
  -H 'anthropic-beta: oauth-2025-04-20' \
  -H 'content-type: application/json' \
  -d '{"model":"claude-haiku-4-5","max_tokens":8,"messages":[{"role":"user","content":"say hi"}]}' \
  https://api.anthropic.com/v1/messages)
echo "status=$CODE"
echo "tokendigest=$(printf %s "$TOKEN" | sha256sum | cut -d' ' -f1)"
if [ "$CODE" != "200" ]; then echo "body=$(head -c 200 /tmp/resp.body | tr -d '\n')"; fi`
}

func tokenDigestScript() string {
	return `sed -n 's/.*"access_token"[^"]*"\([^"]*\)".*/\1/p' ` + guestCredsPath +
		` | tr -d '\n' | sha256sum | cut -d' ' -f1`
}

func firstField(out string) string {
	if f := strings.Fields(out); len(f) > 0 {
		return f[0]
	}
	return ""
}

func field(out, key string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, key+"=") {
			return strings.TrimPrefix(line, key+"=")
		}
	}
	return ""
}

func TestACCredentialMountLive(t *testing.T) {
	if os.Getenv("HERDR_MSB_LIVE") != "1" {
		t.Skip("HERDR_MSB_LIVE not set")
	}

	credsPath := hostCredsPath(t)
	if _, err := credmount.FileMount(credsPath, guestCredsPath, false); err != nil {
		t.Fatalf("credmount.FileMount rejected the store path: %v", err)
	}
	backupStore(t, credsPath)

	hostCreds, err := credmount.Load(credsPath)
	if err != nil {
		t.Fatalf("load host creds: %v", err)
	}
	t.Logf("MEASURE host store at start: expires_in=%s token_digest=%s",
		hostCreds.ExpiresIn(time.Now()).Round(time.Second), short(digest(hostCreds.AccessToken)))

	probeDir := t.TempDir()
	probePath := filepath.Join(probeDir, "probe.txt")
	if err := os.WriteFile(probePath, []byte("probe-init"), 0o644); err != nil {
		t.Fatal(err)
	}

	sb := livemsb.RequireSandbox(t, livemsb.SandboxOpts{
		Name:      "s16-credmount",
		Image:     "alpine",
		MemoryMiB: 1024,
		VCPUs:     2,
		FileMounts: []livemsb.BindMount{
			{HostPath: credsPath, GuestPath: guestCredsPath},
			{HostPath: probePath, GuestPath: guestProbePath},
		},
	})

	ctx := context.Background()
	sh := func(script string) (string, int) {
		t.Helper()
		stdout, stderr, code, err := sb.Sh(ctx, script)
		if err != nil {
			t.Fatalf("guest sh failed: %v (stderr=%q)", err, stderr)
		}
		if strings.TrimSpace(stdout) == "" && strings.TrimSpace(stderr) != "" {
			return strings.TrimSpace(stderr), code
		}
		return strings.TrimSpace(stdout), code
	}

	if out, code := sh("apk add --no-cache curl >/dev/null 2>&1 && curl --version | head -1"); code != 0 {
		t.Fatalf("apk add curl in guest failed: code=%d out=%q", code, out)
	} else {
		t.Logf("MEASURE guest curl: %s", out)
	}

	statOut, _ := sh("stat -c '%u:%g %a' " + guestCredsPath + "; id -u")
	t.Logf("MEASURE guest view of mounted store: %s", strings.ReplaceAll(statOut, "\n", " | "))

	var preRefreshDigest string

	t.Run("AC1_guest_real_api_call_from_mount", func(t *testing.T) {
		out, code := sh(apiCallScript())
		status := field(out, "status")
		preRefreshDigest = field(out, "tokendigest")
		t.Logf("MEASURE AC1 guest->api.anthropic.com/v1/messages: status=%s exit=%d guest_token_digest=%s",
			status, code, short(preRefreshDigest))
		if status != "200" {
			t.Fatalf("AC1 UNMET: guest API call status=%s out=%q", status, out)
		}
		if preRefreshDigest != digest(hostCreds.AccessToken) {
			t.Fatalf("AC1 UNMET: guest token digest %s != host %s", short(preRefreshDigest), short(digest(hostCreds.AccessToken)))
		}
	})

	t.Run("AC4_guest_process_reads_token_blast_radius", func(t *testing.T) {
		want := digest(hostCreds.AccessToken)

		defaultOut, _ := sh(tokenDigestScript() + `; id -un`)
		defaultDigest := firstField(defaultOut)
		t.Logf("MEASURE AC4 default guest identity read: digest=%s host_digest=%s match=%v raw=%q",
			short(defaultDigest), short(want), defaultDigest == want, strings.ReplaceAll(defaultOut, "\n", " | "))

		script := `adduser -D -H probe 2>/dev/null || true
cat > /tmp/read.sh <<'EOS'
` + tokenDigestScript() + `
EOS
chmod 755 /tmp/read.sh
su probe -s /bin/sh -c /tmp/read.sh 2>&1`
		nonRootOut, _ := sh(script)
		nonRootDigest := firstField(nonRootOut)
		t.Logf("MEASURE AC4 non-root guest user read: digest=%s match=%v raw=%q",
			short(nonRootDigest), nonRootDigest == want, strings.ReplaceAll(nonRootOut, "\n", " | "))

		if defaultDigest != want {
			t.Fatalf("AC4 UNMET: the guest's default process identity could not read the host access token (got %q)", defaultOut)
		}
		if nonRootDigest != want {
			t.Logf("MEASURE AC4 containment boundary: the mount keeps the host file mode, so a non-root guest user is DENIED while every default (root) process in the guest reads the live token in full")
		}
	})

	t.Run("AC2_host_refresh_midsession", func(t *testing.T) {
		if preRefreshDigest == "" {
			t.Fatal("AC2 precondition missing: AC1 did not record a pre-refresh token digest")
		}

		probeTmp := filepath.Join(probeDir, ".probe-swap")
		if err := os.WriteFile(probeTmp, []byte("probe-renamed"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(probeTmp, probePath); err != nil {
			t.Fatal(err)
		}
		renameOut, _ := sh("cat " + guestProbePath)
		renameVisible := renameOut == "probe-renamed"
		t.Logf("MEASURE AC2 write-mode rename (Save semantics, new inode): propagated=%v guest_saw=%q",
			renameVisible, renameOut)

		if err := os.WriteFile(probePath, []byte("probe-inplace"), 0o644); err != nil {
			t.Fatal(err)
		}
		inplaceOut, _ := sh("cat " + guestProbePath)
		t.Logf("MEASURE AC2 write-mode in-place (SaveInPlace semantics, same inode): propagated=%v guest_saw=%q",
			inplaceOut == "probe-inplace", inplaceOut)

		oldTokenDigest := preRefreshDigest
		refresher := credmount.Refresher{UserAgent: "curl/8.5.0"}
		refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		updated, err := refresher.RefreshFile(refreshCtx, credsPath, true)
		if err != nil {
			t.Fatalf("AC2 BLOCKED: host OAuth refresh returned non-2xx on a single attempt; no retry is made and the store is left untouched by RefreshFile: %v", err)
		}
		newDigest := digest(updated.AccessToken)
		t.Logf("MEASURE AC2 host refresh (RefreshFile inPlace=true): ok=true rotated=%v new_expires_in=%s new_digest=%s",
			newDigest != oldTokenDigest, updated.ExpiresIn(time.Now()).Round(time.Second), short(newDigest))

		onDisk, err := credmount.Load(credsPath)
		if err != nil {
			t.Fatalf("AC2 UNMET: store unreadable after refresh: %v", err)
		}
		if digest(onDisk.AccessToken) != newDigest {
			t.Fatalf("AC2 UNMET: refresh not persisted to %s", credsPath)
		}

		rawSeen, _ := sh(tokenDigestScript())
		guestSeen := firstField(rawSeen)
		reread := guestSeen == newDigest
		t.Logf("MEASURE AC2 guest re-read after host refresh: guest_digest=%s new=%s old=%s reread=%v cached_stale=%v",
			short(guestSeen), short(newDigest), short(oldTokenDigest), reread, guestSeen == oldTokenDigest)

		out, code := sh(apiCallScript())
		status := field(out, "status")
		t.Logf("MEASURE AC2 guest next api call after host refresh: status=%s exit=%d digest=%s",
			status, code, short(field(out, "tokendigest")))

		if !reread {
			t.Fatalf("AC2 FINDING: guest did NOT see the new token bytes through the mount (guest=%s new=%s); next call status=%s",
				short(guestSeen), short(newDigest), status)
		}
		if status != "200" {
			t.Fatalf("AC2 FINDING: guest saw the new token but its next real API call returned status=%s out=%q", status, out)
		}
	})

	names, err := livemsb.ListNames(ctx)
	if err == nil {
		t.Logf("MEASURE msb list at end of test body: %v", names)
	}
}
