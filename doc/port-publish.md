# Port publish — design record

## Verified: microsandbox v0.6.17 has no post-hoc publish surface

Probes run against the installed daemon and SDK:

- `msb port`, `msb expose`, `msb publish` → exit 2, "unrecognized subcommand"
- `-p` flag exists on `msb run` only; `msb start --help` has no port flag
- `msbsdk.ModifyOptions` has no ports field — no SDK call for post-start port mapping
- No FFI function name in the SDK contains "port", "expose", or "publish"

## Choice: publish is a recreate

`(*Runtime).Publish` removes the named sandbox and re-creates it with `Ports` set
in `SandboxSpec`, which `SandboxOptions` maps to `-p HOST:GUEST` via `WithPorts`.
Post-hoc same-process publish is not half-built or deferred — it is structurally
unavailable in this runtime version.

## Costs

State: all in-guest filesystem state is destroyed on Publish. The test asserts this
in P3 by reading `/tmp/marker.txt` in the new guest and requiring a non-zero exit.

Wall time, measured by `TestPublishTwoHostReach` (PROOF P3) across three live runs on
engine-03 with a 512 MiB Alpine guest: 419ms, 393ms, 320ms. Recreate is cheap in wall
time and expensive in state — the guest is a new machine afterwards, so publish must be
decided before a working session starts, not in the middle of one.

## Same-number invariant

Host port == guest port for every entry in `Ports`. The test builds the nc listener
command, the `-L` SSH forward local port, the `-L` remote port, the published host
port, and the curl URL all from the same `port` variable and logs them together in
the PROOF P6 port invariant line. A renumbered success is a failure: Vite HMR
embeds its own address in the websocket URL, and OAuth `redirect_uri` must match the
registered value exactly — a port mismatch breaks both silently from the outside.

## Forward presence

Per-forward presence is read from the local LISTEN socket via `netstat -an -p tcp`
on the laptop, not from `ssh -O check` (which reports only the master connection,
not individual forwards) and not from `ssh -O cancel` exit code (which is 0 even
when the forward never existed). P6c asserts LISTEN present; P6e asserts LISTEN
absent after `pkill`. The t.Cleanup also runs the kill and logs the final socket
state regardless of how the test ends.

`TestLiveTeardownTwoHostAfterPublish` (build tag `live`) re-proves teardown against a
real two-machine SSH master and records the vacuity controls in the same run: `ssh -O
check` printed `Master running (pid=1692622)` exit 0 both before and after
`TeardownSandbox`, and `Cancel` of a never-applied forward returned a nil error. Only
the local LISTEN socket changed state (true → false).

## Two-host proof

`TestPublishTwoHostReach` is gated on `HERDR_MSB_LIVE=1` plus `HERDR_MSB_LIVE_LAPTOP`
(`user@host`). The local end is a different machine (macOS laptop over Tailscale); the
sandbox host is engine-03. The laptop's login shell is fish, so every remote command is
wrapped in `bash -lc '...'`. Guest reach is proven by a per-run unique marker served
from inside the guest and read back by `curl` on the laptop — reachability alone would
not distinguish the guest from a local loop.

## Port-count ceiling sweep — 2026-09-09

Question: how many ports can one sandbox publish before it degrades or fails?
This determines whether we can publish a wide range at boot and skip all operator
port-selection logic, or must bound the range and accept destructive-recreate for
ports outside it.

### Method

Sequential `msb create --name porttest -m 512M alpine` runs with `-p N:N` repeated
for N in a contiguous range starting at 1024 (or 20000 for smaller tests).
Each sandbox removed before the next. Host RAM monitored throughout; abort
threshold was 2 GiB available (never approached — minimum observed: 16.9 GiB).

### Results table

| Count requested | Exit | Boot ms | Listeners actually bound | libkrun RSS |
|----------------|------|---------|--------------------------|-------------|
| 1              | 0    | 356     | 1                        | 70 MiB      |
| 10             | 0    | 369     | 10                       | 69 MiB      |
| 100            | 0    | 354     | 100                      | 69 MiB      |
| 500            | 0    | 378     | 500                      | 70 MiB      |
| 1,000          | 0    | 369     | 1,000                    | 70 MiB      |
| 4,000          | 0    | 391     | 4,000                    | 75 MiB      |
| 8,000          | 0    | 411     | 7,999 (1 host conflict)  | 82 MiB      |
| 16,000         | 0    | 510     | 15,997 (3 host conflicts)| 90 MiB      |
| 32,000         | 0    | 637     | 31,988 (12 conflicts)    | 107 MiB     |
| 45,000         | 0    | 721     | 44,983 (17 conflicts)    | 124 MiB     |
| 55,000         | 0    | 774     | 54,983 (17 conflicts)    | 139 MiB     |
| 64,512 (max)   | 0    | 768     | 64,485 (27 conflicts)    | 162 MiB     |

"Conflicts" = host-side EADDRINUSE from other processes already listening on that port;
microsandbox exited 0 in all cases and silently skipped the conflicted ports.

### No ceiling found

Microsandbox v0.6.17 does not fail at any tested count. The practical ceiling is
the 16-bit TCP port space: 64,512 non-privileged ports (1024–65535) were published
in one sandbox in 768 ms with 162 MiB RSS overhead on a 512 MiB guest.

Boot time scales sub-linearly: ~360 ms at 1–1,000 ports, ~770 ms at 64,512 ports.
RSS scales with count but modestly: 70 MiB at 1 port, 162 MiB at 64,512 ports
(~1.4 KiB per port beyond the 70 MiB base).

### Functional verification

Tested at 45,000-port scale (ports 20000–64999):
- Port 20010 (low, listener active in guest): `PONG-20010` received on host. Exit 0.
- Port 64990 (high, listener active in guest): `PONG-64990` received on host. Exit 0.
- Port 19999 (NOT published): `nc -z` exit 1. Negative control passed.
- Port 65001 (NOT published): `nc -z` exit 1. Negative control passed.

Tested at 64,512-port scale (ports 1024–65535):
- Port 5050 (low, listener active in guest): `PONG-5050` received on host. Exit 0.
- Port 65100 (high, listener active in guest): `PONG-65100` received on host. Exit 0.
- Port 1000 (NOT published): `nc -z` exit 1. Negative control passed.
- Port 80 (NOT published): `nc -z` exit 1. Negative control passed.

NOTE on nc -z to a published-but-idle port: `nc -z` to a published port with no
guest listener returns exit 0 because libkrun completes the host-side TCP handshake
before discovering the guest has no listener. `nc -z` exit 1 is the reliable negative
control only for unpublished ports. The meaningful positive control is actual data
transfer (guest sends bytes, host receives them).

### Design implication

Publish a wide default range at boot — no operator port-selection required. A range
of 20000–29999 (10,000 ports) covers practically any dev workload with 90 MiB RSS
overhead, a 400–450 ms boot contribution, and zero destructive-recreate events for
ports in that range. The recreate path (for a port outside the range) still exists
but should be rare enough to be acceptable.

## Range allocator schema coupling

`internal/runtime/msb/rangealloc.go` derives block occupancy by querying the daemon's private SQLite database directly via `sqlite3`. This is an explicit coupling to daemon internals that is NOT covered by the SDK's public API.

### Schema elements depended on

- File: `$MSB_HOME/db/msb.db` (default `$HOME/.microsandbox/db/msb.db`)
- Table: `sandbox`
- Column: `config` — JSON text; port assignments at `$.network.ports[*].host_port`
- Column: `status` — string; values `removed` and `removing` are excluded from the live set; all other values are treated as live
- JSON path: `config->'$.network.ports'` accessed via sqlite3's `json_each()` and `->>` operator

### Why ConfigJSON() per-handle iteration was rejected

`msbsdk.GetSandbox(name)` → `h.ConfigJSON()` is the SDK-sanctioned path for reading a sandbox's stored config. It was rejected because `LookupSandbox` (the FFI backing both `GetSandbox` and `ListSandboxes`) uses a fixed 1 MiB output buffer (`defaultBufSize = 1<<20` in `internal/ffi/ffi.go`). A 10,000-port sandbox config is approximately 1.8 MiB; any `GetSandbox` or `ListSandboxes` call on such a sandbox overflows the buffer and returns `KindBufferTooSmall`. There is no configurable override in v0.6.17. Verified: `output buffer too small: need 1821601, have 1048576`.

### Failure mode on schema change

`daemonOccupiedBlocks` runs a corroboration check: it counts live sandboxes (`SELECT COUNT(*) FROM sandbox WHERE status NOT IN ('removed','removing')`), then queries ports. If the sandbox count is positive but the ports query returns zero rows, it returns an error naming schema drift as the likely cause. This ensures a schema change surfaces as a loud `rangealloc: N live sandbox(es) in DB but no ports visible` error on the next `CreateAndBoot`, rather than a silent total collision where every sandbox is handed block 0.

### Failure mode when sqlite3 is absent

Both `Allocate` and `CheckCollision` fail closed if `sqlite3` is not on PATH: `exec.Command("sqlite3", ...)` returns a non-zero exit and the error propagates as `rangealloc: sqlite3 sandbox count: exec: "sqlite3": executable file not found in $PATH` (or similar). `CreateAndBoot` aborts before any sandbox is created.

### Stability note

microsandbox ships schema changes without notice. Any rename of the `sandbox` table, relocation of `$.network.ports`, or new status vocabulary values will surface via the corroboration check at worst, or as a sqlite3 error at best.
