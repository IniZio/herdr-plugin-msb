#!/usr/bin/env bash
set -euo pipefail

TABLE="inet herdr-uid-boundary"
VALID_BASES="5000 15000 25000 35000 45000 55000"

usage() {
    echo "Usage: $0 apply <base> <uid>" >&2
    echo "       $0 remove <base>" >&2
    echo "       $0 cleanup" >&2
    exit 1
}

validate_base() {
    local base=$1
    for v in $VALID_BASES; do
        [[ "$base" == "$v" ]] && return 0
    done
    echo "error: base $base is not a known block base (valid: $VALID_BASES)" >&2
    exit 1
}

validate_uid() {
    local uid=$1
    [[ "$uid" =~ ^[0-9]+$ ]] || { echo "error: uid must be numeric" >&2; exit 1; }
    [[ "$uid" -ge 1 ]] || { echo "error: uid 0 refused (misconfiguration guard)" >&2; exit 1; }
}

chain_name() { echo "block_$1"; }

cmd_apply() {
    local base=$1 uid=$2
    validate_base "$base"
    validate_uid "$uid"
    local chain
    chain=$(chain_name "$base")
    local top=$(( base + 9999 ))

    nft add table $TABLE
    nft flush chain $TABLE "$chain" 2>/dev/null || true
    nft delete chain $TABLE "$chain" 2>/dev/null || true
    nft add chain $TABLE "$chain" \
        '{ type filter hook output priority 0; policy accept; }'
    nft add rule $TABLE "$chain" \
        ip daddr 127.0.0.1 tcp dport "$base-$top" meta skuid != "$uid" drop
}

cmd_remove() {
    local base=$1
    validate_base "$base"
    local chain
    chain=$(chain_name "$base")
    nft flush chain $TABLE "$chain" 2>/dev/null || true
    nft delete chain $TABLE "$chain" 2>/dev/null || true
}

cmd_cleanup() {
    nft delete table $TABLE 2>/dev/null || true
}

[[ $# -ge 1 ]] || usage
case "$1" in
    apply)   [[ $# -eq 3 ]] || usage; cmd_apply  "$2" "$3" ;;
    remove)  [[ $# -eq 2 ]] || usage; cmd_remove "$2" ;;
    cleanup) [[ $# -eq 1 ]] || usage; cmd_cleanup ;;
    *)       usage ;;
esac
