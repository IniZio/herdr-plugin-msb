package livemsb_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func guestKeepAliveLoopScript(credPath, apiEndpoint string, nIter, sleepSec int) string {
	_ = apiEndpoint
	return fmt.Sprintf(`set -u
TOKEN=$(sed -n 's/.*"accessToken"[^"]*"\([^"]*\)".*/\1/p' %s)
if [ -z "$TOKEN" ]; then echo "EXTRACTION_FAILED"; exit 1; fi
CACHED_DIG=$(printf '%%s' "$TOKEN" | sha256sum | cut -d' ' -f1)
echo "loop_start cached_digest=$CACHED_DIG"
for I in $(seq 1 %d); do
  sleep %d
  FILE_TOKEN=$(sed -n 's/.*"accessToken"[^"]*"\([^"]*\)".*/\1/p' %s)
  FILE_DIG=$(printf '%%s' "$FILE_TOKEN" | sha256sum | cut -d' ' -f1)
  echo "iter=$I cached_digest=$CACHED_DIG file_digest=$FILE_DIG stale=$([ "$FILE_DIG" != "$CACHED_DIG" ] && echo true || echo false)"
done`, credPath, nIter, sleepSec, credPath)
}

func guestRefreshScript(tokenEndpoint, clientID, credPath string) string {
	return fmt.Sprintf(`set -eu
REFRESH_TOKEN=$(sed -n 's/.*"refreshToken"[^"]*"\([^"]*\)".*/\1/p' %s)
if [ -z "$REFRESH_TOKEN" ]; then echo "REFRESH_TOKEN_EMPTY"; exit 1; fi
PROBE_DIR=$(dirname %s)
PROBE_TMP="$PROBE_DIR/.creds-probe-$$.tmp"
PROBE_SNT="$PROBE_DIR/.creds-probe-$$.snt"
if ! { printf 'probe' > "$PROBE_TMP" && mv "$PROBE_TMP" "$PROBE_SNT" && rm -f "$PROBE_SNT"; }; then
  rm -f "$PROBE_TMP" "$PROBE_SNT"
  echo "PREFLIGHT_RENAME_FAILED"
  exit 1
fi
RESP=$(curl -sS --max-time 45 -w '\nHTTP_STATUS=%%{http_code}' -X POST %s \
  -H 'content-type: application/x-www-form-urlencoded' \
  -A 'curl/8.5.0' \
  --data-urlencode 'grant_type=refresh_token' \
  --data-urlencode "refresh_token=$REFRESH_TOKEN" \
  --data-urlencode 'client_id=%s')
STATUS=$(printf '%%s' "$RESP" | grep '^HTTP_STATUS=' | cut -d= -f2)
BODY=$(printf '%%s' "$RESP" | grep -v '^HTTP_STATUS=')
if [ "$STATUS" != "200" ]; then echo "TOKEN_ENDPOINT_FAILED status=$STATUS body=$BODY"; exit 1; fi
NEW_ACCESS=$(printf '%%s' "$BODY" | sed -n 's/.*"access_token"[^"]*"\([^"]*\)".*/\1/p')
NEW_REFRESH=$(printf '%%s' "$BODY" | sed -n 's/.*"refresh_token"[^"]*"\([^"]*\)".*/\1/p')
if [ -z "$NEW_ACCESS" ]; then echo "NEW_ACCESS_EMPTY body=$BODY"; exit 1; fi
UPDATED=$(sed \
  -e 's/"accessToken"[[:space:]]*:[[:space:]]*"[^"]*"/"accessToken": "'"$NEW_ACCESS"'"/' \
  -e 's/"refreshToken"[[:space:]]*:[[:space:]]*"[^"]*"/"refreshToken": "'"${NEW_REFRESH:-$REFRESH_TOKEN}"'"/' \
  %s)
if [ -z "$UPDATED" ]; then echo "SED_FAILED"; exit 1; fi
TMPF=$(dirname %s)/.creds-tmp-$$.json
printf '%%s' "$UPDATED" > "$TMPF"
mv "$TMPF" %s
echo "REFRESH_OK new_access_digest=$(printf '%%s' "$NEW_ACCESS" | sha256sum | cut -d' ' -f1)"`,
		credPath, credPath, tokenEndpoint, clientID, credPath, credPath, credPath)
}

func TestAC4ProbeAbortsBeforePost(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not in PATH")
	}

	var mu sync.Mutex
	seen := 0
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen++
		mu.Unlock()
		http.Error(w, "unexpected contact", http.StatusInternalServerError)
	}))
	defer stub.Close()

	dir := t.TempDir()
	credFile := filepath.Join(dir, "creds.json")
	if err := os.WriteFile(credFile, []byte(syntheticNestedCreds("tok-ac4", "ref-ac4")), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)

	script := guestRefreshScript(stub.URL, "ac4-client", credFile)
	out, execErr := exec.Command("/bin/sh", "-c", script).CombinedOutput()
	t.Logf("MEASURE AC4 abort output: %s", strings.TrimSpace(string(out)))

	if execErr == nil {
		t.Fatalf("AC4: script exited 0, expected nonzero — probe did not abort")
	}
	if !strings.Contains(string(out), "PREFLIGHT_RENAME_FAILED") {
		t.Fatalf("AC4: expected PREFLIGHT_RENAME_FAILED in output, got: %q", string(out))
	}

	stub.Close()
	mu.Lock()
	finalSeen := seen
	mu.Unlock()
	if finalSeen != 0 {
		t.Fatalf("AC4: stub recorded %d requests (want 0); probe did not abort before POST", finalSeen)
	}
	t.Logf("MEASURE AC4: probe_aborted=true stub_requests=%d exit_nonzero=true", finalSeen)
}
