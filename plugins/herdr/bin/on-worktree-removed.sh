#!/bin/sh
# Hook: worktree.removed  (herdr 0.8.0 envelope)
set -eu

STATE_DIR="${XDG_STATE_HOME:-$HOME/.local/state}/herdr-plugin-msb"
mkdir -p -m 0700 "$STATE_DIR"
LOG="$STATE_DIR/herdr-events.log"

TS=$(date -u '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || printf 'unknown')
EVT="${HERDR_PLUGIN_EVENT:-worktree.removed}"

if ! command -v jq >/dev/null 2>&1; then
    printf '%s event=%s workspace=- worktree=- source=none reason=jq-missing\n' \
        "$TS" "$EVT" >> "$LOG"
    exit 0
fi

WS_ID="${HERDR_WORKSPACE_ID:-}"
SOURCE=env
if [ -z "$WS_ID" ]; then
    WS_ID=$(printf '%s' "${HERDR_PLUGIN_EVENT_JSON:-}" \
        | jq -r '.data.workspace_id // empty' 2>/dev/null) || true
    SOURCE=json
fi
[ -z "$WS_ID" ] && WS_ID='-'

WT_PATH=$(printf '%s' "${HERDR_PLUGIN_EVENT_JSON:-}" \
    | jq -r '.data.worktree.path // empty' 2>/dev/null) || true
[ -z "$WT_PATH" ] && WT_PATH='-'

printf '%s event=%s workspace=%s worktree=%s source=%s\n' \
    "$TS" "$EVT" "$WS_ID" "$WT_PATH" "$SOURCE" >> "$LOG"

if [ "$WS_ID" = '-' ]; then
    printf '%s event=%s space-prune=skipped reason=no-workspace-id\n' \
        "$TS" "$EVT" >> "$LOG"
    exit 0
fi

PLUGIN=''
if [ -n "${HERDR_PLUGIN_ROOT:-}" ] && [ -x "$HERDR_PLUGIN_ROOT/herdr-plugin-msb" ]; then
    PLUGIN="$HERDR_PLUGIN_ROOT/herdr-plugin-msb"
elif [ -x "./herdr-plugin-msb" ]; then
    PLUGIN="./herdr-plugin-msb"
else
    PLUGIN=$(command -v herdr-plugin-msb 2>/dev/null) || true
fi

if [ -z "$PLUGIN" ]; then
    printf '%s event=%s space-prune=skipped reason=binary-not-found\n' \
        "$TS" "$EVT" >> "$LOG"
    exit 0
fi

set +e
PRUNE_OUT=$("$PLUGIN" space-prune --apply --kill-running --workspace "$WS_ID" 2>&1)
PRUNE_RC=$?
set -e

printf '%s event=%s space-prune=done rc=%s output=%s\n' \
    "$TS" "$EVT" "$PRUNE_RC" "$PRUNE_OUT" >> "$LOG"
