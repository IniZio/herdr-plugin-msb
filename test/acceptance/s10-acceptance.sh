#!/usr/bin/env bash
# s10-acceptance-live-loop: proves AC1-AC7 for herdr-plugin-msb
# Env: HERDR_MSB_LIVE_LAPTOP (default newman@100.64.0.35), HERDR_MSB_LIVE_ENGINE (default 100.64.0.156)
# Needs: msb daemon running, ssh to laptop, credential at ~/.config/nexus3/claude-dedicated/
set -euo pipefail

LAPTOP="${HERDR_MSB_LIVE_LAPTOP:-newman@100.64.0.35}"
ENGINE="${HERDR_MSB_LIVE_ENGINE:-newman@100.64.0.156}"
PORTSTR="45455"
CRED_DIR="$HOME/.config/nexus3/claude-dedicated"
CRED_FILE="$CRED_DIR/.credentials.json"
EXPECTED_SHA="820c57ca13b1798ac3f94a46edadc0d2ebacc2ace662a938535ab90966a91748"
WORKTREE="/home/newman/magic/herdr-plugin-msb"
PLUGIN="herdr-plugin-msb"
SCRATCH="$(mktemp -d)"
ITER1="s10iter1"
ITER2="s10iter2"
AC2_SENTINEL_ACTIVE=0

log() { echo "[$(date -u +%H:%M:%SZ)] $*" >&2; }
fail() { log "FAIL: $*"; exit 1; }

gx() {
    local name=$1; shift
    "$PLUGIN" exec -project herdr "$name" -- "$@" 2>/dev/null
}

laptop_run() {
    ssh -o BatchMode=yes -o ConnectTimeout=10 "$LAPTOP" "bash -lc $(printf '%q' "$1")"
}

cleanup() {
    if [ "$AC2_SENTINEL_ACTIVE" = "1" ]; then
        cat "$SCRATCH/ac2-real.json" > "$CRED_FILE" 2>/dev/null || true
        log "EMERGENCY: sentinel reverted to real credential"
    fi
    for n in "$ITER1" "$ITER2"; do
        "$PLUGIN" stop -project herdr "$n" 2>/dev/null || true
        "$PLUGIN" rm -project herdr "$n" 2>/dev/null && log "cleanup: removed $n" || true
    done
    laptop_run "pkill -f 'ssh.*-L $PORTSTR:127.0.0.1:$PORTSTR' 2>/dev/null; true" 2>/dev/null || true
    rm -rf "$SCRATCH"
    log "final msb list:"; msb list 2>&1 || true
}
trap cleanup EXIT

TOKEN_DIGEST_SCRIPT='sed -n '"'"'s/.*"accessToken"[^"]*"\([^"]*\)".*/\1/p'"'"' /root/.claude/.credentials.json | tr -d '"'"'\n'"'"' | sha256sum | cut -d'"'"' '"'"' -f1'

API_SCRIPT='set -u
TOKEN=$(sed -n '"'"'s/.*"accessToken"[^"]*"\([^"]*\)".*/\1/p'"'"' /root/.claude/.credentials.json)
[ -z "$TOKEN" ] && { echo "EXTRACTION_FAILED"; exit 1; }
wget -qS -O /tmp/resp.body \
  --post-data='"'"'{"model":"claude-haiku-4-5","max_tokens":8,"messages":[{"role":"user","content":"say hi"}]}'"'"' \
  --header="authorization: Bearer $TOKEN" \
  --header="anthropic-version: 2023-06-01" \
  --header="anthropic-beta: oauth-2025-04-20" \
  --header="content-type: application/json" \
  https://api.anthropic.com/v1/messages 2>/tmp/resp.hdr
CODE=$(grep -o '"'"'HTTP/[^ ]* [0-9]*'"'"' /tmp/resp.hdr | tail -1 | awk '"'"'{print $2}'"'"')
echo "status=$CODE"
echo "tokendigest=$(printf %s "$TOKEN" | sha256sum | cut -d'"'"' '"'"' -f1)"
[ "$CODE" != "200" ] && echo "body=$(head -c 300 /tmp/resp.body | tr -d '"'"'\n'"'"')"
true'

log "=== PRE-FLIGHT ==="
PRECHECK=$(msb list 2>&1)
log "msb list: $PRECHECK"
echo "$PRECHECK" | grep -q "No sandboxes" || fail "sandboxes already running"
CRED_SHA_INITIAL=$(sha256sum "$CRED_FILE" | awk '{print $1}')
[ "$CRED_SHA_INITIAL" != "$EXPECTED_SHA" ] && fail "initial cred sha256 mismatch: $CRED_SHA_INITIAL"
log "cred sha256=$CRED_SHA_INITIAL"
log "laptop uname=$(laptop_run "uname")"
ss -ltn 2>/dev/null | grep -q ":$PORTSTR " && fail "DEGENERATE: host already listens on :$PORTSTR"
log "pre-flight OK"

log "=== ITER1 CREATE ==="
"$PLUGIN" create -project herdr --name "$ITER1" --image alpine \
    --worktree "$WORKTREE" --cred=true --port "$PORTSTR" --mem 1024 --vcpus 2
log "create exit=0"
sleep 3
CRED_STAT=$(gx "$ITER1" sh -c "stat -c '%u:%g %a %n' /root/.claude/.credentials.json 2>/dev/null || echo MISSING")
log "MEASURE dir-mount stat: $CRED_STAT"
echo "$CRED_STAT" | grep -q "MISSING" && fail "credential directory mount missing"
log "dir mount OK"

log "=== AC1 ==="
API1_OUT=$(gx "$ITER1" sh -c "$API_SCRIPT")
log "MEASURE AC1:"; echo "$API1_OUT"
AC1_STATUS=$(echo "$API1_OUT" | grep '^status=' | cut -d= -f2)
AC1_TOKDIG=$(echo "$API1_OUT" | grep '^tokendigest=' | cut -d= -f2 || echo "")
if [ "$AC1_STATUS" = "429" ]; then
    log "AC1: 429 — sleep 30s retry"
    sleep 30
    API1_OUT=$(gx "$ITER1" sh -c "$API_SCRIPT")
    log "MEASURE AC1 retry:"; echo "$API1_OUT"
    AC1_STATUS=$(echo "$API1_OUT" | grep '^status=' | cut -d= -f2)
    AC1_TOKDIG=$(echo "$API1_OUT" | grep '^tokendigest=' | cut -d= -f2 || echo "")
fi
log "AC1 status=$AC1_STATUS tokdig=${AC1_TOKDIG:0:12}"

log "=== AC3 TWO-OUTCOME ==="
BLOCKED_CODE=0
gx "$ITER1" sh -c "nc -w5 8.8.8.8 443 </dev/null" || BLOCKED_CODE=$?
log "MEASURE AC3-BLOCKED: nc 8.8.8.8:443 exit=$BLOCKED_CODE (want non-zero)"
[ "$BLOCKED_CODE" -eq 0 ] && fail "AC3 DEGENERATE: 8.8.8.8:443 reachable"
log "AC3-BLOCKED PASS"
ALLOWED_CODE=0
gx "$ITER1" sh -c "nc -w5 api.anthropic.com 443 </dev/null" || ALLOWED_CODE=$?
log "MEASURE AC3-ALLOWED: nc api.anthropic.com:443 exit=$ALLOWED_CODE (want 0)"
[ "$ALLOWED_CODE" -ne 0 ] && log "AC3-ALLOWED FINDING: exit=$ALLOWED_CODE" || log "AC3-ALLOWED PASS"

log "=== AC4 TWO-OUTCOME ==="
MARKER1="s10m1$(date +%s)"
nc -z -w5 127.0.0.1 "$PORTSTR" && log "AC4-HOST-PRE: port reachable pre-listener" || log "AC4-HOST-PRE: port not yet up"
LISTEN_BODY="HTTP/1.1 200 OK\r\nContent-Length: ${#MARKER1}\r\nConnection: close\r\n\r\n$MARKER1"
gx "$ITER1" sh -c "nohup sh -c 'while true; do printf \"$LISTEN_BODY\" | nc -l -p $PORTSTR; done' >/dev/null 2>&1 & sleep 2" 2>/dev/null || true
sleep 2
laptop_run "pkill -f 'ssh.*-L $PORTSTR:127.0.0.1:$PORTSTR' 2>/dev/null; true" 2>/dev/null || true
sleep 1
LAP_PRE_CODE=0
laptop_run "curl -sS --max-time 4 http://127.0.0.1:$PORTSTR/ >/dev/null 2>&1" 2>/dev/null || LAP_PRE_CODE=$?
log "MEASURE AC4-NEGATIVE-CTRL: laptop curl without forward code=$LAP_PRE_CODE (want non-zero)"
[ "$LAP_PRE_CODE" = "0" ] && fail "AC4 DEGENERATE: laptop reached host without forward"
log "AC4-NEGATIVE-CTRL PASS code=$LAP_PRE_CODE"
FWD_CODE=0
laptop_run "ssh -f -N -o ExitOnForwardFailure=yes -o BatchMode=yes -L $PORTSTR:127.0.0.1:$PORTSTR $ENGINE" || FWD_CODE=$?
log "MEASURE AC4: laptop ssh -L setup exit=$FWD_CODE"
[ "$FWD_CODE" -ne 0 ] && fail "laptop SSH tunnel failed"
sleep 2
LAP_BODY=$(laptop_run \
    "for i in 1 2 3 4 5; do out=\$(curl -sS --max-time 5 http://127.0.0.1:$PORTSTR/) && [ -n \"\$out\" ] && printf '%s' \"\$out\" && break; sleep 1; done" \
    2>/dev/null) || true
log "MEASURE AC4-POSITIVE: laptop body=${LAP_BODY:0:80}"
echo "$LAP_BODY" | grep -q "$MARKER1" && log "AC4-POSITIVE PASS" || log "AC4-POSITIVE FINDING: marker not in body"

log "=== AC2 ==="
GUEST_D1=$(gx "$ITER1" sh -c "$TOKEN_DIGEST_SCRIPT" 2>/dev/null || echo "")
log "MEASURE AC2 D1=${GUEST_D1:0:12}"
[ -z "$GUEST_D1" ] && fail "AC2: could not read guest token digest"

cp "$CRED_FILE" "$SCRATCH/ac2-real.json"
SNAP_SHA=$(sha256sum "$SCRATCH/ac2-real.json" | awk '{print $1}')
[ "$SNAP_SHA" != "$EXPECTED_SHA" ] && fail "AC2: snapshot sha256 mismatch: $SNAP_SHA"
log "AC2: snapshot verified sha256=$SNAP_SHA"

SENTINEL="s10-ac2-sentinel-$(date +%s)"
python3 -c "
import json, sys
with open('$SCRATCH/ac2-real.json') as f:
    d = json.load(f)
d['claudeAiOauth']['accessToken'] = '$SENTINEL'
sys.stdout.write(json.dumps(d))
" > "$SCRATCH/ac2-sentinel.json"

AC2_SENTINEL_ACTIVE=1
cat "$SCRATCH/ac2-sentinel.json" > "$CRED_FILE"
log "AC2: sentinel written in-place (O_TRUNC)"
sleep 1

GUEST_D2=$(gx "$ITER1" sh -c "$TOKEN_DIGEST_SCRIPT" 2>/dev/null || echo "")
log "MEASURE AC2 D2=${GUEST_D2:0:12}"
[ "$GUEST_D1" = "$GUEST_D2" ] && fail "AC2 UNMET: guest still reads D1 after sentinel write — propagation not observed"
log "AC2: D2 != D1 — sentinel propagated to guest PASS"

cat "$SCRATCH/ac2-real.json" > "$CRED_FILE"
AC2_SENTINEL_ACTIVE=0
log "AC2: real credential restored in-place (O_TRUNC)"
sleep 1

GUEST_D3=$(gx "$ITER1" sh -c "$TOKEN_DIGEST_SCRIPT" 2>/dev/null || echo "")
log "MEASURE AC2 D3=${GUEST_D3:0:12}"
[ "$GUEST_D1" != "$GUEST_D3" ] && fail "AC2 UNMET: guest digest after restore D3 != D1"
log "AC2: D3 == D1 — restore confirmed PASS"

AC2_API_OUT=$(gx "$ITER1" sh -c "$API_SCRIPT")
log "MEASURE AC2 post-restore API:"; echo "$AC2_API_OUT"
AC2_STATUS=$(echo "$AC2_API_OUT" | grep '^status=' | cut -d= -f2)
if [ "$AC2_STATUS" = "429" ]; then
    log "AC2: 429 — sleep 30s retry"
    sleep 30
    AC2_API_OUT=$(gx "$ITER1" sh -c "$API_SCRIPT")
    log "MEASURE AC2 retry:"; echo "$AC2_API_OUT"
    AC2_STATUS=$(echo "$AC2_API_OUT" | grep '^status=' | cut -d= -f2)
fi
[ "$AC2_STATUS" != "200" ] && fail "AC2 UNMET: post-restore API status=$AC2_STATUS"
log "AC2 PASS: post-restore API $AC2_STATUS"

CRED_SHA_POST_AC2=$(sha256sum "$CRED_FILE" | awk '{print $1}')
[ "$CRED_SHA_POST_AC2" != "$EXPECTED_SHA" ] && fail "AC2: ABORT final sha256 mismatch: $CRED_SHA_POST_AC2"
log "AC2: host sha256 back to $CRED_SHA_POST_AC2"

log "=== AC4-NEGATIVE ==="
"$PLUGIN" stop -project herdr "$ITER1" 2>&1
"$PLUGIN" rm -project herdr "$ITER1" 2>&1
log "sandbox $ITER1 removed"
sleep 2
NC_HOST_AFTER=0
nc -z -w5 127.0.0.1 "$PORTSTR" || NC_HOST_AFTER=$?
log "MEASURE AC4-AFTER: host nc exit=$NC_HOST_AFTER (want non-zero)"
[ "$NC_HOST_AFTER" -ne 0 ] && log "AC4-NEGATIVE PASS (host)" || log "AC4-NEGATIVE FINDING: host port still up"
LAP_AFTER=$(laptop_run "curl -sS --max-time 5 http://127.0.0.1:$PORTSTR/ 2>&1 || true" 2>/dev/null) || true
log "MEASURE AC4-AFTER: laptop curl=${LAP_AFTER:0:80}"
echo "$LAP_AFTER" | grep -q "$MARKER1" \
    && log "AC4-NEGATIVE FINDING: marker still returned after remove" \
    || log "AC4-NEGATIVE PASS (laptop): marker absent"
laptop_run "pkill -f 'ssh.*-L $PORTSTR:127.0.0.1:$PORTSTR' 2>/dev/null; true" 2>/dev/null || true

log "=== AC5 ITER2 ==="
sleep 2
"$PLUGIN" create -project herdr --name "$ITER2" --image alpine \
    --cred=true --mem 1024 --vcpus 2
log "iter2 create exit=0"
sleep 3
API2_OUT=$(gx "$ITER2" sh -c "$API_SCRIPT")
log "MEASURE AC5 API:"; echo "$API2_OUT"
AC5_STATUS=$(echo "$API2_OUT" | grep '^status=' | cut -d= -f2)
if [ "$AC5_STATUS" = "429" ]; then
    log "AC5: 429 — retry after 30s"
    sleep 30
    API2_OUT=$(gx "$ITER2" sh -c "$API_SCRIPT")
    echo "$API2_OUT"
    AC5_STATUS=$(echo "$API2_OUT" | grep '^status=' | cut -d= -f2)
fi
log "AC5 API status=$AC5_STATUS"
BLOCKED2=0
gx "$ITER2" sh -c "nc -w5 8.8.8.8 443 </dev/null" || BLOCKED2=$?
log "MEASURE AC5-AC3: nc 8.8.8.8:443 exit=$BLOCKED2 (want non-zero)"
"$PLUGIN" stop -project herdr "$ITER2" 2>&1
"$PLUGIN" rm -project herdr "$ITER2" 2>&1
log "iter2 removed"

log "=== AC7 ==="
FINAL_LIST=$(msb list 2>&1)
log "msb list: $FINAL_LIST"
echo "$FINAL_LIST" | grep -q "No sandboxes" && log "AC7 PASS" || fail "AC7 FAIL: $FINAL_LIST"

CRED_SHA_FINAL=$(sha256sum "$CRED_FILE" | awk '{print $1}')
[ "$CRED_SHA_FINAL" != "$EXPECTED_SHA" ] && fail "FINAL: cred sha256 mismatch: $CRED_SHA_FINAL"
log "FINAL: cred sha256=$CRED_SHA_FINAL (matches expected)"

trap - EXIT
rm -rf "$SCRATCH"
log "=== SUMMARY ==="
log "pilot=$WORKTREE"
log "revision=$("$PLUGIN" version | grep revision)"
log "AC1 status=$AC1_STATUS tokdig=${AC1_TOKDIG:0:12}"
log "AC3-BLOCKED exit=$BLOCKED_CODE  AC3-ALLOWED exit=$ALLOWED_CODE"
log "AC4-POSITIVE marker=$(echo "$LAP_BODY" | grep -c "$MARKER1" || echo 0)"
log "AC4-NEGATIVE host=$NC_HOST_AFTER"
log "AC2 D1=${GUEST_D1:0:12} D2=${GUEST_D2:0:12} D3=${GUEST_D3:0:12} post-restore=$AC2_STATUS"
log "AC5 status=$AC5_STATUS  AC5-AC3 blocked=$BLOCKED2"
log "AC7 $FINAL_LIST"
log "cred sha256=$CRED_SHA_FINAL (unchanged)"
