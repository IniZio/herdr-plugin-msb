#!/bin/sh
# Hook: worktree.created  (herdr 0.8.0 envelope)
# Env: HERDR_PLUGIN_EVENT, HERDR_PLUGIN_EVENT_JSON (envelope), HERDR_WORKSPACE_ID
set -eu

STATE_DIR="${XDG_STATE_HOME:-$HOME/.local/state}/herdr-plugin-msb"
mkdir -p -m 0700 "$STATE_DIR"
LOG="$STATE_DIR/herdr-events.log"

TS=$(date -u '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || printf 'unknown')
EVT="${HERDR_PLUGIN_EVENT:-worktree.created}"

if ! command -v jq >/dev/null 2>&1; then
    printf '%s event=%s workspace=- worktree=- source=none reason=jq-missing\n' \
        "$TS" "$EVT" >> "$LOG"
    exit 0
fi

WS_ID="${HERDR_WORKSPACE_ID:-}"
SOURCE=env
if [ -z "$WS_ID" ]; then
    WS_ID=$(printf '%s' "${HERDR_PLUGIN_EVENT_JSON:-}" \
        | jq -r '.data.workspace.workspace_id // empty' 2>/dev/null) || true
    SOURCE=json
fi
[ -z "$WS_ID" ] && WS_ID='-'

WT_PATH=$(printf '%s' "${HERDR_PLUGIN_EVENT_JSON:-}" \
    | jq -r '.data.worktree.path // empty' 2>/dev/null) || true
[ -z "$WT_PATH" ] && WT_PATH='-'

printf '%s event=%s workspace=%s worktree=%s source=%s\n' \
    "$TS" "$EVT" "$WS_ID" "$WT_PATH" "$SOURCE" >> "$LOG"
