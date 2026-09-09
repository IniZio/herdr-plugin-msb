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

## D-24 proc-based discovery (supersedes netstat)

### Why /proc/net/tcp instead of netstat

`netstat` requires net-tools in the guest image. A distroless or minimal image
ships neither `netstat` nor `ss`. `/proc/net/tcp` is part of the Linux kernel
virtual filesystem and is present in every Linux guest regardless of userland.
The exec path (`sh -c "cat /proc/net/tcp /proc/net/tcp6 2>/dev/null"`) has no
dependency on any installed binary beyond `sh`, which every image that runs a
`cmd` has.

### /proc/net/tcp address byte-order

Each entry in `/proc/net/tcp` has the form:
```
sl  local_address           rem_address  st ...
 0: 0100007F:0BB8  00000000:0000  0A ...
```

The address field `0100007F` is `sin_addr.s_addr` printed with `%08X` as a
native 32-bit integer on the host. On a little-endian (x86) machine, the
network-order address `127.0.0.1 = 0x7F000001` is stored in memory as bytes
`[7F, 00, 00, 01]` but read by the CPU as `0x0100007F` — the byte-reversed
value. To recover the correct IP, extract bytes as `[byte(v), byte(v>>8),
byte(v>>16), byte(v>>24)]`, which yields `[0x7F, 0x00, 0x00, 0x01]` = 127.0.0.1.

A naive big-endian parse of `0100007F` gives `1.0.0.127` (wrong). Both parse
paths are exercised in `TestParseProcNetTCP_ByteOrder` with the opposite outcome
asserted as a negative control.

The port field (`0BB8` = 3000) is already in host order via `ntohs()` and is
parsed directly.

For `/proc/net/tcp6`, the 128-bit address is four consecutive little-endian
32-bit words (32 hex chars total). The same byte-extraction applies to each
word to produce the 16-byte IPv6 address.

### Reserved-set rationale

The filter (`FilterListeners`) classifies listeners into three buckets:

| Bucket | Condition | Action |
|---|---|---|
| Reserved | port < 1024 or in operator exclude list | never forwarded |
| OutOfRange | port > 11023 (outside the 10k guest block) | reported distinctly; forwarding requires sandbox recreate |
| Forwardable | port in [1024, 11023] and not reserved | forwarded |

Ports below 1024 are privileged; nothing a dev server binds there would be
intentional in this context. The operator exclude list accommodates custom
control ports (e.g. a local proxy or health endpoint the operator does not want
exposed). The Linux ephemeral range (32768-60999) is NOT excluded — VS Code's
Remote Containers spec excludes nothing there, and a dev server can legitimately
bind to an ephemeral port via `SO_REUSEPORT` or explicit bind.

The plugin has no fixed guest control ports of its own: `msb exec` uses the SDK
FFI vsock channel, not a guest-side listening socket. No plugin-specific port
reservation was found in the codebase; none is added here.

### Out-of-range port reporting

A listener on a guest port above 11023 cannot be forwarded without tearing down
and recreating the sandbox (the 10k host-port block is allocated at create time
and cannot grow). `FilterResult.OutOfRange` surfaces these distinctly so the
layer above can decide whether to offer a recreate or warn the user. Silently
dropping them would make a dev server on a high port appear simply broken.

### Per-port teardown

`Manager.Reconcile` tracks applied forwards as `(sandboxID, port)` pairs. On
each reconcile the full desired set of pairs is compared against the applied set.
Any pair absent from the desired set triggers `Forwarder.Cancel`, even if the
sandbox itself remains running. This ensures a port that stops listening
immediately loses its forward on the next poll cycle — the forward does not
survive until sandbox stop.
