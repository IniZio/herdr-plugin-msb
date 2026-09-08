package livemsb_test

import "fmt"

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
RESP=$(curl -sS -w '\nHTTP_STATUS=%%{http_code}' -X POST %s \
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
printf '%%s' "$UPDATED" > %s
echo "REFRESH_OK new_access_digest=$(printf '%%s' "$NEW_ACCESS" | sha256sum | cut -d' ' -f1)"`, credPath, tokenEndpoint, clientID, credPath, credPath)
}
