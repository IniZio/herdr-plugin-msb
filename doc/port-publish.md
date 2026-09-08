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
