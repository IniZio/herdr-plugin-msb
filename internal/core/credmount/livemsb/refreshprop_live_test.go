//go:build live

package livemsb_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IniZio/herdr-plugin-msb/internal/core/credmount"
	"github.com/IniZio/herdr-plugin-msb/internal/core/credmount/livemsb"
)

const (
	guestCredDir    = "/root/.claude"
	guestCredFile   = "/root/.claude/.credentials.json"
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
	t.Logf("MEASURE backup verified: path=%s bytes=%d token_nonempty=true", propBackupPath, len(data))
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

func TestAC1KeepAlive(t *testing.T) {
	// WAIVER D-2: synthetic credentials; token endpoint not contacted; loop monitors file digest changes only, makes no API calls.
	if os.Getenv("HERDR_MSB_LIVE") != "1" {
		t.Skip("HERDR_MSB_LIVE not set")
	}

	dir := t.TempDir()
	credFile := filepath.Join(dir, "creds.json")
	tokenA := "synthetic-keepalive-token-AAAA1111"
	tokenB := "synthetic-keepalive-token-BBBB2222"
	if err := os.WriteFile(credFile, []byte(syntheticNestedCreds(tokenA, "ref-x")), 0600); err != nil {
		t.Fatal(err)
	}

	sb := livemsb.RequireSandbox(t, livemsb.SandboxOpts{
		Name:       "s16-ac1-keepalive",
		Image:      "alpine",
		MemoryMiB:  512,
		VCPUs:      1,
		FileMounts: []livemsb.BindMount{{HostPath: credFile, GuestPath: "/mnt/creds.json"}},
	})

	script := guestKeepAliveLoopScript("/mnt/creds.json", "", 4, 3)
	outCh := make(chan string, 1)
	go func() {
		out, _, _, _ := sb.Sh(t.Context(), script)
		outCh <- out
	}()

	time.Sleep(5 * time.Second)
	if err := os.WriteFile(credFile, []byte(syntheticNestedCreds(tokenB, "ref-x")), 0600); err != nil {
		t.Fatalf("rotate credential file: %v", err)
	}
	t.Logf("MEASURE AC1: host rotated file to token-B at 5s into loop")

	rawOut := <-outCh
	t.Logf("MEASURE AC1 keep-alive loop output:\n%s", rawOut)

	digA := digest(tokenA)
	digB := digest(tokenB)
	staleCount, freshCount, cachedBCount := 0, 0, 0
	for _, line := range strings.Split(rawOut, "\n") {
		if strings.Contains(line, "cached_digest="+digA[:12]) {
			staleCount++
		}
		if strings.Contains(line, "file_digest="+digB[:12]) {
			freshCount++
		}
		if strings.Contains(line, "cached_digest="+digB[:12]) {
			cachedBCount++
		}
	}
	t.Logf("MEASURE AC1: stale_iters=%d fresh_file_reads=%d cached_became_B=%d (digA=%s digB=%s)",
		staleCount, freshCount, cachedBCount, digA[:12], digB[:12])
	if freshCount == 0 {
		t.Fatalf("AC1 UNMET: no iteration observed file_digest=digB — host rotation never reached the guest")
	}
	if cachedBCount > 0 {
		t.Fatalf("AC1 UNMET: CACHED_DIG became digB in %d iters — loop is re-caching the rotated token", cachedBCount)
	}
}

func TestAC2AC6RefreshPropagation(t *testing.T) {
	requireRefreshBudget(t)

	credsPath := credmount.DefaultStorePath()
	rm, err := credmount.DirMount(credmount.DefaultStoreDir(), guestCredDir, false)
	if err != nil {
		t.Fatalf("credmount.DirMount rejected the store dir: %v", err)
	}
	backupForProp(t, credsPath)
	// NOTE(TBR-8): no pre-flight probe for the token endpoint is possible without spending the
	// attempt it guards; messages API and OAuth token endpoint are separate rate limiters.
	// Waiting policy: do not retry within 1h of a 429 on platform.claude.com/v1/oauth/token.

	before, err := credmount.Load(credsPath)
	if err != nil {
		t.Fatalf("load live store: %v", err)
	}
	oldAccess := before.AccessToken
	if oldAccess == "" {
		t.Fatalf("live store carries an empty access token")
	}

	mounts := []livemsb.BindMount{{HostPath: rm.HostPath, GuestPath: rm.GuestPath, ReadOnly: rm.ReadOnly}}
	sbA := livemsb.RequireSandbox(t, livemsb.SandboxOpts{
		Name: "s16-ac2-refresher", Image: "alpine", MemoryMiB: guestMemMiB, VCPUs: 1, DirMounts: mounts,
	})
	sbB := livemsb.RequireSandbox(t, livemsb.SandboxOpts{
		Name: "s16-ac6-bystander", Image: "alpine", MemoryMiB: guestMemMiB, VCPUs: 1, DirMounts: mounts,
	})

	provisionCurl(t, sbA, "sandbox A")
	provisionCurl(t, sbB, "sandbox B")

	statusA0, digA0 := guestCall(t, sbA, "pre-refresh sandbox A")
	statusB0, digB0 := guestCall(t, sbB, "pre-refresh sandbox B")
	if statusA0 != "200" || statusB0 != "200" {
		t.Fatalf("baseline not established: A=%s B=%s", statusA0, statusB0)
	}
	if digA0 != digB0 {
		t.Fatalf("guests read different tokens from one store: %s vs %s", short(digA0), short(digB0))
	}

	refreshCtx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	attemptAt := time.Now().UTC().Format(time.RFC3339)

	refreshScript := guestRefreshScript(credmount.DefaultTokenEndpoint, credmount.DefaultClientID, guestCredFile)
	rOut, rErr, rCode, rExecErr := sbA.Sh(refreshCtx, refreshScript)
	if rExecErr != nil || rCode != 0 || !strings.Contains(rOut, "REFRESH_OK") {
		reread, lerr := credmount.Load(credsPath)
		untouched := lerr == nil && reread.AccessToken == oldAccess
		stillGood := untouched && hostAPIStatus(t, oldAccess) == 200
		t.Fatalf("AC2/AC6 BLOCKED: guest refresh at %s failed (exit=%d store_untouched=%v old_token_still_200=%v): %s %s",
			attemptAt, rCode, untouched, stillGood, rOut, rErr)
	}
	t.Logf("MEASURE guest refresh (AC2) succeeded at %s: %s", attemptAt, strings.TrimSpace(rOut))

	after, err := credmount.Load(credsPath)
	if err != nil {
		t.Fatalf("load host store after guest refresh: %v", err)
	}
	if after.AccessToken == oldAccess {
		t.Fatalf("guest refresh completed but access token did not rotate on host side")
	}

	oldStatus := hostAPIStatus(t, oldAccess)
	t.Logf("MEASURE old token after rotation: http=%d", oldStatus)

	statusA1, digA1 := guestCall(t, sbA, "post-refresh sandbox A (AC2)")
	if digA1 == digA0 {
		t.Errorf("AC2 UNMET: sandbox A still reads pre-refresh token (digest unchanged %s)", short(digA0))
	}
	if statusA1 != "200" {
		t.Errorf("AC2 UNMET: sandbox A next call after guest refresh returned HTTP %s, want 200", statusA1)
	}

	statusB1, digB1 := guestCall(t, sbB, "post-refresh sandbox B (AC6)")
	if statusB1 == "401" {
		t.Errorf("AC6 UNMET: bystander sandbox B observed HTTP 401 from sandbox A's rotation (digest %s)", short(digB1))
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
