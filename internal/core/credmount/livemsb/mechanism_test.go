package livemsb_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func syntheticNestedCreds(access, refresh string) string {
	return fmt.Sprintf(
		`{"claudeAiOauth":{"accessToken":%q,"refreshToken":%q,"expiresAt":9999999999999,"scopes":[]}}`,
		access, refresh,
	)
}

func TestAC1LoopScriptReadsTokenOnce(t *testing.T) {
	script := guestKeepAliveLoopScript("/mnt/creds.json", "https://stub.invalid", 3, 1)
	cachedPat := "CACHED_DIG="
	loopPat := "for I in "
	lp := strings.Index(script, loopPat)
	if lp < 0 {
		t.Fatalf("script missing for-loop: %s", script)
	}
	cc := strings.Count(script, cachedPat)
	if cc != 1 {
		t.Fatalf("CACHED_DIG assigned %d times (want exactly 1); script re-caches token each iter: %s", cc, script)
	}
	cp := strings.Index(script, cachedPat)
	if cp > lp {
		t.Fatalf("CACHED_DIG assignment is INSIDE loop (cp=%d > lp=%d); cached digest re-set each iter", cp, lp)
	}
	t.Logf("MEASURE AC1 loop script: CACHED_DIG assigned once at pos %d, loop at pos %d", cp, lp)
}

func TestAC2GuestRefreshMechanism(t *testing.T) {
	// WAIVER D-2: stub token endpoint; no real OAuth refresh spent.
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not in PATH")
	}

	newAccess := "synthetic-new-access-aabbcc"
	newRefresh := "synthetic-new-refresh-ddeeff"
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "want POST", http.StatusMethodNotAllowed)
			return
		}
		resp := map[string]interface{}{
			"access_token":  newAccess,
			"refresh_token": newRefresh,
			"expires_in":    3600,
			"token_type":    "Bearer",
		}
		w.Header().Set("content-type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Logf("stub encode: %v", err)
		}
	}))
	defer stub.Close()

	dir := t.TempDir()
	credFile := filepath.Join(dir, "creds.json")
	oldAccess := "synthetic-old-access-112233"
	oldRefresh := "synthetic-old-refresh-445566"
	if err := os.WriteFile(credFile, []byte(syntheticNestedCreds(oldAccess, oldRefresh)), 0600); err != nil {
		t.Fatal(err)
	}

	script := guestRefreshScript(stub.URL, "test-client", credFile)
	out, err := exec.Command("/bin/sh", "-c", script).CombinedOutput()
	t.Logf("MEASURE AC2 mechanism output: %s", strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("refresh script: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "REFRESH_OK") {
		t.Fatalf("script did not print REFRESH_OK: %s", out)
	}

	updated, err := os.ReadFile(credFile)
	if err != nil {
		t.Fatalf("read updated creds: %v", err)
	}
	if !strings.Contains(string(updated), newAccess) {
		t.Fatalf("new access token absent from updated file\ngot: %s", updated)
	}
	if strings.Contains(string(updated), oldAccess) {
		t.Fatalf("old access token still present after in-place write\ngot: %s", updated)
	}
	t.Logf("MEASURE AC2 mechanism: in-place write OK; new token present, old absent")
}

func TestAC2GuestRefreshMechanismNegative(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not in PATH")
	}

	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusBadRequest)
	}))
	defer stub.Close()

	dir := t.TempDir()
	credFile := filepath.Join(dir, "creds.json")
	if err := os.WriteFile(credFile, []byte(syntheticNestedCreds("tok-a", "ref-a")), 0600); err != nil {
		t.Fatal(err)
	}

	script := guestRefreshScript(stub.URL, "bad-client", credFile)
	out, _ := exec.Command("/bin/sh", "-c", script).CombinedOutput()
	t.Logf("MEASURE AC2 mechanism negative: %s", strings.TrimSpace(string(out)))
	if strings.Contains(string(out), "REFRESH_OK") {
		t.Fatalf("negative control: REFRESH_OK on HTTP 400 — check is vacuous")
	}
	t.Logf("MEASURE AC2 mechanism negative: confirmed REFRESH_OK absent on 400")
}

func TestAC3PartialBodySalvage(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not in PATH")
	}

	partial := `{"access_token":"partial-new-tok`

	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("hijack not supported")
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		defer conn.Close()
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 999\r\n\r\n" + partial)
		_ = buf.Flush()
	}))
	defer stub.Close()

	dir := t.TempDir()

	cred1 := filepath.Join(dir, "creds1.json")
	if err := os.WriteFile(cred1, []byte(syntheticNestedCreds("tok-ac3a", "ref-ac3a")), 0o600); err != nil {
		t.Fatal(err)
	}

	oldScript := fmt.Sprintf(`set -eu
REFRESH_TOKEN=$(sed -n 's/.*"refreshToken"[^"]*"\([^"]*\)".*/\1/p' %s)
if [ -z "$REFRESH_TOKEN" ]; then echo "REFRESH_TOKEN_EMPTY"; exit 1; fi
PROBE_TMP=$(dirname %s)/.creds-probe-$$.tmp
if ! { cp %s "$PROBE_TMP" && mv "$PROBE_TMP" %s; }; then rm -f "$PROBE_TMP" || true; echo "PREFLIGHT_RENAME_FAILED"; exit 1; fi
RESP=$(curl -sS --max-time 45 -w '\nHTTP_STATUS=%%{http_code}' -X POST %s \
  -H 'content-type: application/x-www-form-urlencoded' -A 'curl/8.5.0' \
  --data-urlencode 'grant_type=refresh_token' \
  --data-urlencode "refresh_token=$REFRESH_TOKEN" \
  --data-urlencode 'client_id=ac3-client')
echo "REACHED body=$RESP"`,
		cred1, cred1, cred1, cred1, stub.URL)

	beforeOut, _ := exec.Command("/bin/sh", "-c", oldScript).CombinedOutput()
	t.Logf("AC3 BEFORE (body lost):\n%s", strings.TrimSpace(string(beforeOut)))
	if strings.Contains(string(beforeOut), "partial") {
		t.Fatalf("AC3 BEFORE: partial bytes visible — defect not reproduced; test is vacuous")
	}
	if strings.Contains(string(beforeOut), "REACHED") {
		t.Fatalf("AC3 BEFORE: REACHED printed — set -e did not abort; test is vacuous")
	}

	cred2 := filepath.Join(dir, "creds2.json")
	if err := os.WriteFile(cred2, []byte(syntheticNestedCreds("tok-ac3b", "ref-ac3b")), 0o600); err != nil {
		t.Fatal(err)
	}
	afterOut, _ := exec.Command("/bin/sh", "-c", guestRefreshScript(stub.URL, "ac3-client", cred2)).CombinedOutput()
	t.Logf("AC3 AFTER (body salvaged):\n%s", strings.TrimSpace(string(afterOut)))
	if !strings.Contains(string(afterOut), "partial") {
		t.Fatalf("AC3 AFTER: partial bytes absent — salvage did not work; got: %q", string(afterOut))
	}
	t.Logf("MEASURE AC3: before_body_lost=true after_body_salvaged=true")
}
