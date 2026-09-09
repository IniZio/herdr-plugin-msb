# UID Boundary — nftables per-sandbox port isolation

## Threat

Each sandbox publishes a 10,000-port block on `127.0.0.1` (bases 5000, 15000, 25000,
35000, 45000, 55000; block size 10,000). Without a boundary, any local process —
including processes running as other host UIDs — can connect to those ports. On a
multi-account host (e.g. Engine-03 with `petercheng` UID=1000, `rickmak` UID=1001) that
means any compromised or stale account can reach the sandbox's published surface.

## Rule anatomy

```
Table:  inet herdr-uid-boundary
Chain:  block_<base>   (one per block base; underscore, not hyphen)
Type:   filter hook output priority 0; policy accept;
Rule:   ip daddr 127.0.0.1 tcp dport <base>-<base+9999> meta skuid != <uid> drop
```

`meta skuid` resolves the socket's UID in `init_user_ns` — the host UID — at the
kernel's output hook before a packet leaves the loopback stack. Any outbound TCP
SYN targeting the port range from a socket whose host UID is not `<uid>` is dropped.

## What the rule protects and what it does not

**Protects:** processes running as genuinely different host UIDs (e.g. petercheng UID=1000)
cannot connect to the owner's port block. The `skuid` check fires on the source socket's
host UID regardless of what address or interface the packet targets.

**Does not protect — same-user user namespaces:** `unshare -U --map-user=65534` started
by the sandbox owner (e.g. newman UID=1003) does not bypass the rule because the kernel
resolves `from_kuid_munged(init_user_ns, sk->sk_uid)` — that returns 1003, the host UID,
even inside the namespace. The uid_map `65534 → 1003 1` makes the kernel treat the
socket as owned by 1003. Same-user namespace tricks are therefore *permitted* by the
rule, which is correct: they represent the owner.

**Does not protect — root (UID 0):** root bypasses the `skuid != <uid>` predicate by
definition. No rule restricts root on its own engine.

**Does not protect — inherited file descriptors:** a process that receives an already-
connected socket via `SCM_RIGHTS` inherits the open connection; the rule only fires on
new connections.

## Privilege requirement

`nft` (`/usr/sbin/nft`) requires `CAP_NET_ADMIN`. The plugin runs as a normal user (e.g.
UID 1003). Shell scripts cannot hold file capabilities (only ELF binaries can). The helper
must therefore be invoked via `sudo` with a `NOPASSWD` sudoers entry.

## Installation

1. Copy and make executable:

```sh
sudo install -o root -g root -m 755 \
    scripts/nft-uid-boundary.sh /usr/local/libexec/herdr-nft-boundary
```

2. Add a sudoers entry (edit with `visudo -f /etc/sudoers.d/herdr-nft`):

```
newman ALL=(root) NOPASSWD: /usr/local/libexec/herdr-nft-boundary *
```

The wildcard allows any arguments; the fixed path prevents substituting a different
script. No other `sudo` privilege is granted.

## Script interface

| Invocation | Effect |
|---|---|
| `sudo herdr-nft-boundary apply <base> <uid>` | Create/replace chain for block, install drop rule |
| `sudo herdr-nft-boundary remove <base>` | Flush and delete chain for block (idempotent) |
| `sudo herdr-nft-boundary cleanup` | Delete the entire `inet herdr-uid-boundary` table |

`<base>` must be one of the six known block bases. `<uid>` must be numeric and ≥ 1 (UID 0
is refused as a misconfiguration guard). Both `remove` and `cleanup` treat a missing
table or chain as success.

## Lifecycle

- **After successful sandbox creation:** call `apply <base> <owner-uid>`.
- **Before or during sandbox removal:** call `remove <base>`.
- **Plugin startup / operator reset:** call `cleanup` to wipe any stale chains from a
  previous run, then re-apply rules for any sandbox found running.

The `apply` sub-command flushes and deletes any pre-existing chain before recreating it,
so repeated `apply` calls for the same block are idempotent.

## Probe methodology

The `unshare -U --map-user=65534` probe used in the original assessment demonstrated the
absence of 127.0.0.1 network isolation (any process can connect by default). It is **not**
the correct probe for verifying the nft rule, because host UID remains 1003 inside that
namespace and the rule permits it.

Correct verification requires two probes with opposite expected outcomes:

1. **Owner succeeds:** from the owner's shell (UID 1003), `nc -w 3 127.0.0.1 <port>` —
   expect connection or clean refusal from the service, not a firewall drop.
2. **Other user blocked:** `su - petercheng -c 'nc -w 3 127.0.0.1 <port>'` — expect
   timeout (packet dropped).
3. **Control — rule removed:** remove the rule, repeat probe 2 — expect success. This
   confirms the drop in probe 2 was caused by the rule, not by the service refusing.

A single passing probe cannot distinguish an honoured rule from an ignored one.

## Proportionality assessment

Engine-03 is described as single-operator. The other `/etc/passwd` accounts (petercheng,
rickmak) are stale. The nft boundary provides defence in depth but is **not** a critical
security control for this deployment. The principal residual risk — exploiting a sandbox's
published ports from a different account — is only meaningful if a stale account is
compromised or an attacker pivots to the machine. Install the boundary as a cheap
hardening measure; it does not materially change the threat posture of a genuine
single-operator machine.
