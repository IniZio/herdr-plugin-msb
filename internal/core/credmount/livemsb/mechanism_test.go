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
