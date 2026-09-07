# portfwd/discovery — design rationale

## Why host-side port enumeration is impossible here

microsandbox (v0.6.17) gives every sandbox a private userspace network stack
(smoltcp). The guest kernel sees real sockets; the host kernel does not. Three
approaches were measured on this host and ruled out:

**Host `ss -ltnp` / `/proc/net/tcp`**: shows only the host's own sockets. The
sandbox process shares the host network namespace (`net:[4026531833]`) yet the
guest sockets live inside the guest kernel, not the host kernel. A listener on
guest port 45455 was invisible to host `ss` while live.

**`ip netns list` / `/var/run/netns/`**: both empty. There is no named host
netns for a sandbox to enter. `nsenter -t <pid> -n ss` returns "Operation not
permitted" — same reason: no separate netns exists.

**`msb inspect --format json`**: reported `"ports": []` while a listener was
live on 45455. microsandbox only knows ports the operator declared at create
time. Unpublished listeners are invisible to the API.

## Why the exec/vsock path works

`msb exec <name> -- netstat -ltn` crosses no network boundary. It uses the
vsock exec channel that microsandbox establishes from host to guest at boot.
This was measured twice and returned the listener correctly both times.

## Why `netstat` and not `ss`

The guest image is Alpine-minimal. Measurement confirmed it ships
`/bin/netstat` (from net-tools) and `/usr/bin/nc`. It has no `ss` (iproute2),
no `python3`, no `socat`. `ss` would produce a "not found" exec error on every
real sandbox.

## Why identity comes from our records, not process ancestry

The `msb exec` call does not return a pid, and the runtime seam (`ExecResult`)
carries no pid field by design. Even if a pid were available, the sandbox
process shares the host netns, making port-to-pid attribution via `/proc`
unreliable across netns boundaries (three failure modes are documented in
`portfwd-netns-attribution-fails.md` in the project memory). The `SandboxRef`
we pass into `DiscoverOne` comes from `Runtime.List`, which reads the live
sandbox registry — the authoritative, tamper-resistant source of identity.
Carrying that ref by value into every `Listener` means callers never need to
re-query for ownership.

## Local-address parsing

`netstat -ltn` prints the local address in one of four forms:

| netstat output | BindAddr | notes |
|---|---|---|
| `:::PORT` | `::` | IPv6 all-interfaces |
| `0.0.0.0:PORT` | `0.0.0.0` | IPv4 all-interfaces |
| `127.0.0.1:PORT` | `127.0.0.1` | IPv4 loopback |
| `::1:PORT` | `::1` | IPv6 loopback |

Splitting on the **last** `:` handles all four forms unambiguously. A loopback
bind still forwards correctly via the vsock exec channel, but callers need to
know the bind scope, so `BindAddr` is preserved in `Listener`.
