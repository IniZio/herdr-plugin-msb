//go:build live

package livemsb_test

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/IniZio/herdr-plugin-msb/internal/core/credmount"
	"github.com/IniZio/herdr-plugin-msb/internal/core/credmount/livemsb"
)

const (
	guestPropPath   = "/mnt/creds.json"
	refreshRawDir   = "/tmp/claude-1003/s16-refresh-raw"
	propBackupPath  = "/tmp/claude-1003/s16-creds-prop-backup.json"
	guestMemMiB     = 1024
	apiMessagesURL  = "https://api.anthropic.com/v1/messages"
	oauthBetaHeader = "oauth-2025-04-20"
)

func requireRefreshBudget(t *testing.T) {
	t.Helper()
	if os.Getenv("HERDR_MSB_LIVE") != "1" {
		t.Skip("HERDR_MSB_LIVE not set")
	}
	if os.Getenv("HERDR_MSB_LIVE_REFRESH") != "1" {
		t.Skip("HERDR_MSB_LIVE_REFRESH not set: this test spends a rate-limited OAuth refresh")
	}
}

func backupForProp(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store for backup: %v", err)
	}
	if err := os.WriteFile(propBackupPath, data, 0o600); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	c, err := credmount.Load(propBackupPath)
	if err != nil {
		t.Fatalf("backup is not independently loadable: %v", err)
	}
	if c.AccessToken == "" {
		t.Fatalf("backup carries an empty access token")
	}
	if code := hostAPIStatus(t, c.AccessToken); code != 200 {
		t.Fatalf("backup token is not independently usable: HTTP %d", code)
	}
	t.Logf("MEASURE backup verified usable: path=%s bytes=%d http=200", propBackupPath, len(data))
}

func hostAPIStatus(t *testing.T, token string) int {
	t.Helper()
	body := `{"model":"claude-haiku-4-5","max_tokens":8,"messages":[{"role":"user","content":"say hi"}]}`
	req, err := http.NewRequest(http.MethodPost, apiMessagesURL, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build host api request: %v", err)
	}
	req.Header.Set("authorization", "Bearer "+token)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", oauthBetaHeader)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("User-Agent", "curl/8.5.0")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("host api call: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func provisionCurl(t *testing.T, sb *livemsb.Sandbox, label string) {
	t.Helper()
	out, errOut, code, err := sb.Sh(t.Context(), "apk add --no-cache curl >/dev/null 2>&1 && curl --version | head -1")
	if err != nil || code != 0 {
		t.Fatalf("%s: apk add curl failed: %v (exit=%d out=%q stderr=%q)", label, err, code, out, errOut)
	}
	t.Logf("MEASURE %s curl: %s", label, strings.TrimSpace(out))
}

func guestCall(t *testing.T, sb *livemsb.Sandbox, label string) (status, tokDigest string) {
	t.Helper()
	out, errOut, code, err := sb.Sh(t.Context(), apiCallScript())
	if err != nil {
		t.Fatalf("%s: guest exec: %v (exit=%d stderr=%s)", label, err, code, errOut)
	}
	if strings.Contains(out, "EXTRACTION_FAILED") {
		t.Fatalf("%s: token extraction failed in guest, not an auth result: %s", label, out)
	}
	status = field(out, "status")
	tokDigest = field(out, "tokendigest")
	t.Logf("MEASURE %s: status=%s tokendigest=%s body=%s", label, status, short(tokDigest), field(out, "body"))
	return status, tokDigest
}

func TestAC2AC6RefreshPropagation(t *testing.T) {
	requireRefreshBudget(t)

	credsPath := credmount.DefaultStorePath()
	if _, err := credmount.FileMount(credsPath, guestPropPath, false); err != nil {
		t.Fatalf("credmount.FileMount rejected the store path: %v", err)
	}
	backupForProp(t, credsPath)

	before, err := credmount.Load(credsPath)
	if err != nil {
		t.Fatalf("load live store: %v", err)
	}
	oldAccess := before.AccessToken
	if oldAccess == "" {
		t.Fatalf("live store carries an empty access token")
	}
	if err := os.MkdirAll(refreshRawDir, 0o700); err != nil {
		t.Fatalf("mkdir raw dir: %v", err)
	}

	mounts := []livemsb.BindMount{{HostPath: credsPath, GuestPath: guestPropPath}}
	sbA := livemsb.RequireSandbox(t, livemsb.SandboxOpts{
		Name: "s16-ac2-refresher", Image: "alpine", MemoryMiB: guestMemMiB, VCPUs: 1, FileMounts: mounts,
	})
	sbB := livemsb.RequireSandbox(t, livemsb.SandboxOpts{
		Name: "s16-ac6-bystander", Image: "alpine", MemoryMiB: guestMemMiB, VCPUs: 1, FileMounts: mounts,
	})

	provisionCurl(t, sbA, "sandbox A")
	provisionCurl(t, sbB, "sandbox B")

	statusA0, digA0 := guestCall(t, sbA, "pre-refresh sandbox A")
	statusB0, digB0 := guestCall(t, sbB, "pre-refresh sandbox B")
	if statusA0 != "200" || statusB0 != "200" {
		t.Fatalf("baseline not established: A=%s B=%s", statusA0, statusB0)
	}
	if digA0 != digB0 {
		t.Fatalf("the two guests read different tokens from one store: %s vs %s", short(digA0), short(digB0))
	}

	refresher := credmount.Refresher{RawDir: refreshRawDir, UserAgent: "curl/8.5.0"}
	refreshCtx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	attemptAt := time.Now().UTC().Format(time.RFC3339)
	after, rerr := refresher.RefreshFile(refreshCtx, credsPath, true)
	if rerr != nil {
		reread, lerr := credmount.Load(credsPath)
		untouched := lerr == nil && reread.AccessToken == oldAccess
		stillGood := untouched && hostAPIStatus(t, oldAccess) == 200
		t.Fatalf("AC2/AC6 BLOCKED: host refresh at %s failed (store_untouched=%v old_token_still_200=%v): %q",
			attemptAt, untouched, stillGood, strings.ReplaceAll(rerr.Error(), "\n", " "))
	}
	t.Logf("MEASURE host refresh succeeded at %s: rotated=%v raw_dir=%s",
		attemptAt, after.AccessToken != oldAccess, refreshRawDir)
	if after.AccessToken == oldAccess {
		t.Fatalf("refresh returned 2xx but the access token did not rotate")
	}

	oldStatus := hostAPIStatus(t, oldAccess)
	t.Logf("MEASURE old token after rotation: http=%d", oldStatus)

	statusA1, digA1 := guestCall(t, sbA, "post-refresh sandbox A (AC2)")
	if digA1 == digA0 {
		t.Errorf("AC2 UNMET: sandbox A still reads the pre-refresh token (digest unchanged %s); the guest cached it rather than re-reading the mount", short(digA0))
	}
	if statusA1 != "200" {
		t.Errorf("AC2 UNMET: sandbox A next call after host refresh returned HTTP %s, want 200", statusA1)
	}

	statusB1, digB1 := guestCall(t, sbB, "post-refresh sandbox B (AC6)")
	if statusB1 == "401" {
		t.Errorf("AC6 UNMET: bystander sandbox B observed HTTP 401 caused by sandbox A's rotation (digest %s)", short(digB1))
	} else if statusB1 != "200" {
		t.Errorf("AC6 UNMET: bystander sandbox B returned HTTP %s, want 200", statusB1)
	}

	t.Logf("MEASURE verdict: AC2 A_status=%s A_digest_changed=%v | AC6 B_status=%s old_token_http=%d",
		statusA1, digA1 != digA0, statusB1, oldStatus)

	if err := sbA.Remove(context.Background()); err != nil {
		t.Logf("remove A: %v", err)
	}
	if err := sbB.Remove(context.Background()); err != nil {
		t.Logf("remove B: %v", err)
	}
}
