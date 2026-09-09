#!/usr/bin/env bash
set -euo pipefail

REAL_HOME="$HOME"
LIVE_SOCKET="$(realpath -m "$REAL_HOME/.config/herdr/herdr.sock")"
LIVE_STATE="$(realpath -m "$REAL_HOME/.local/state")"

REAL_GOCACHE="$(go env GOCACHE)"
REAL_GOMODCACHE="$(go env GOMODCACHE)"
REAL_GOPATH="$(go env GOPATH)"

HTEST="$(mktemp -d /tmp/herdr-test-XXXXXX)"
SERVER_PID=

cleanup() {
    local saved=$?
    if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
        HERDR_SOCKET_PATH="$HTEST/herdr.sock" herdr server stop 2>/dev/null || true
        sleep 0.3
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    rm -rf "$HTEST"
    return $saved
}
trap cleanup EXIT INT TERM HUP

export HERDR_SOCKET_PATH="$HTEST/herdr.sock"
export XDG_STATE_HOME="$HTEST/state"
export XDG_CONFIG_HOME="$HTEST/config"
export HOME="$HTEST"
export GOCACHE="$REAL_GOCACHE"
export GOMODCACHE="$REAL_GOMODCACHE"
export GOPATH="$REAL_GOPATH"
export HERDR_MSB_TEST_ISOLATED=1
unset HERDR_WORKSPACE_ID HERDR_PANE_ID HERDR_TAB_ID HERDR_ENV

OUT_SOCKET="$(realpath -m "$HERDR_SOCKET_PATH")"
if [ "$OUT_SOCKET" = "$LIVE_SOCKET" ]; then
    echo "ABORT (test-session incident guard): outgoing HERDR_SOCKET_PATH resolves to the live operator socket $LIVE_SOCKET — this has destroyed the operator session twice by triggering space-convert against w8. Refusing to proceed." >&2
    exit 2
fi

if [ -z "${XDG_STATE_HOME:-}" ]; then
    echo "ABORT (test-session incident guard): XDG_STATE_HOME is unset — would reach the live state dir and mutate herdr-space-bindings.json. Refusing to proceed." >&2
    exit 2
fi
OUT_STATE="$(realpath -m "$XDG_STATE_HOME")"
if [ "$OUT_STATE" = "$LIVE_STATE" ]; then
    echo "ABORT (test-session incident guard): outgoing XDG_STATE_HOME=$XDG_STATE_HOME resolves to the live state dir $LIVE_STATE — this has destroyed the operator session twice. Refusing to proceed." >&2
    exit 2
fi

mkdir -p "$HTEST/state" "$HTEST/config"

if command -v herdr >/dev/null 2>&1; then
    env -u HERDR_WORKSPACE_ID -u HERDR_PANE_ID -u HERDR_TAB_ID -u HERDR_ENV \
        HERDR_SOCKET_PATH="$HTEST/herdr.sock" \
        XDG_STATE_HOME="$HTEST/state" \
        XDG_CONFIG_HOME="$HTEST/config" \
        HOME="$HTEST" \
        herdr server </dev/null >"$HTEST/server.log" 2>&1 &
    SERVER_PID=$!

    i=0
    while [ $i -lt 20 ]; do
        [ -S "$HTEST/herdr.sock" ] && break
        sleep 0.5
        i=$((i + 1))
    done

    if [ ! -S "$HTEST/herdr.sock" ]; then
        echo "WARNING (test-session): herdr server did not start after 10s — failing closed. Tests that reach herdr will fail loudly." >&2
        export HERDR_SOCKET_PATH="$HTEST/absent/herdr.sock"
        SERVER_PID=
    fi
else
    echo "WARNING (test-session): herdr not on PATH — failing closed. Tests that reach herdr will fail loudly." >&2
    export HERDR_SOCKET_PATH="$HTEST/absent/herdr.sock"
fi

"$@"
rc=$?
exit $rc
