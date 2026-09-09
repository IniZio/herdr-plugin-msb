#!/bin/sh
# Env: HERDR_MSB_SANDBOX (required), HERDR_MSB_PROJECT (default: herdr)
set -e

PLUGIN=$(command -v herdr-plugin-msb 2>/dev/null) || true
if [ -z "$PLUGIN" ]; then
    printf "pane.sh: herdr-plugin-msb not found on PATH\n" >&2
    exit 1
fi

case "$1" in
    shell)
        SANDBOX="${HERDR_MSB_SANDBOX:-}"
        if [ -z "$SANDBOX" ]; then
            printf "pane.sh shell: HERDR_MSB_SANDBOX is not set\n" >&2
            exit 1
        fi
        PROJECT="${HERDR_MSB_PROJECT:-herdr}"

        GUEST_SHELL=$("$PLUGIN" exec -project "$PROJECT" "$SANDBOX" -- \
            /bin/sh -c 'command -v bash 2>/dev/null || echo /bin/sh' 2>/dev/null \
            | tr -d '\r' | tail -n 1)
        if [ -z "$GUEST_SHELL" ]; then
            GUEST_SHELL=/bin/sh
        fi

        GUEST_CWD="${HERDR_MSB_GUEST_CWD:-/workspace}"
        case "$GUEST_SHELL" in
            */bash) exec "$PLUGIN" exec -pty -project "$PROJECT" "$SANDBOX" -- \
                        /bin/sh -c 'cd '"$GUEST_CWD"' 2>/dev/null || cd /; exec "$0" "$@"' \
                        "$GUEST_SHELL" -l ;;
            *)      exec "$PLUGIN" exec -pty -project "$PROJECT" "$SANDBOX" -- \
                        /bin/sh -c 'cd '"$GUEST_CWD"' 2>/dev/null || cd /; exec "$0" "$@"' \
                        "$GUEST_SHELL" ;;
        esac
        ;;
    *)
        printf "pane.sh: unknown subcommand '%s'\n" "$1" >&2
        exit 1
        ;;
esac
