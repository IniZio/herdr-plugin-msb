# Port forwarding — operator guide

This document describes the port forwarding system as built and verified. It
covers what to set up once, what to type, what to click, what you will see, and
what to do when things go wrong. No Go struct names; no internal file paths.

---

## What port forwarding does

A dev server inside a sandbox on `engine-03` listens on a loopback address
inside the guest. Port forwarding makes that port reachable in a browser on
your laptop at `http://127.0.0.1:<port>`.

The tunnel is an SSH forward through the existing ControlMaster connection.
**The sandbox does not need to be recreated to open or close a tunnel.**
Toggling a forward only starts or stops the SSH tunnel; it does not touch the
sandbox.

Exception: if you need a port that was **not published when the sandbox was
created**, adding it does require recreation and destroys all in-guest state.
See §5.

---

## 1. One-time setup: install the plugin

Build and install the binary:

```sh
make install          # builds and copies to ~/.local/bin/herdr-plugin-msb
```

Register the plugin with herdr (run once, and after any binary update):

```sh
herdr plugin install --path ~/.local/bin/herdr-plugin-msb
herdr server reload-config
```

`~/.local/bin` must be on your `PATH`. No shell rc edits are required.

To start a session with port forwarding enabled, run:

```sh
herdr-plugin-msb attach <user@engine>
```

This starts the local agent, waits for it to connect, then runs
`herdr --remote <user@engine>` as a child on the inherited terminal.
When the session ends (or is interrupted), the agent and the SSH master
are torn down automatically. No orphaned processes remain.

---

## 2. How ports are published

Each sandbox gets a disjoint 10,000-port block **at create time**. The block
cannot change without destroying and recreating the sandbox.

| Block | Host ports   | Guest ports  |
|-------|--------------|--------------|
| 0     | 5000–14999   | 1024–11023   |
| 1     | 15000–24999  | 1024–11023   |
| 2     | 25000–34999  | 1024–11023   |
| 3     | 35000–44999  | 1024–11023   |
| 4     | 45000–54999  | 1024–11023   |
| 5     | 55000–64999  | 1024–11023   |

Host port `base + i` reaches guest port `1024 + i`. The SSH tunnel maps
the laptop port to the same number the guest uses — so if your dev server
binds to guest port 3000, you access it at `http://localhost:3000` on the
laptop. Up to 6 sandboxes can coexist. See `doc/port-publish.md` for the
port-count ceiling sweep and allocator details.

**Forwardable guest range:** 1024–11023. Ports below 1024 (privileged) are
never forwarded. Ports above 11023 cannot be forwarded without a sandbox
recreate — the pane shows these as `OUT-OF-RANGE`. An operator exclude list
can add additional reserved ports. The Linux ephemeral range (32768–60999)
is **not** excluded by default.

**Port discovery** polls `/proc/net/tcp` and `/proc/net/tcp6` inside the
guest via the SDK's vsock exec channel. It reads
`sh -c "cat /proc/net/tcp /proc/net/tcp6 2>/dev/null"` — never project
files, never host-side socket enumeration. Guest sockets are invisible to
host `ss` or `/proc/net/tcp` because microsandbox uses a private userspace
network stack. See `doc/portfwd-discovery.md`.

---

## 3. The Port Forwards pane

The primary operator surface is the **Port Forwards** pane: a live status
panel for port forwards on the active sandbox.

**How to open it:**
- Action menu → **"Port forwards"** — opens or focuses the pane.
- Ctrl+click any `http://127.0.0.1:<port>` link printed by a dev server —
  the pane opens and the clicked port is set to PENDING. See §4.1.

**Forwarding is session-scoped.** One sandbox is forwarded at a time — the
one attached in the current `herdr --remote` session. This is not arbitrary:
renumbering a forward (to assign each sandbox a distinct local port) breaks
Vite HMR websocket URLs and OAuth `redirect_uri`, which must match the
registered value exactly. Per-sandbox `127.0.0.x` aliases would require
`sudo ifconfig lo0 alias` on macOS. Forwarding one sandbox at a time, with
the same port numbers the guest sees, is the only workable approach.

### 3.1 Screen states

**No state file (agent not running):**

```
Port forwards — (no sandbox)
─────────────────────────────────────────────
  laptop agent not connected
  forwards.state not found

  Run: herdr-plugin-msb attach <target>
  Setup: doc/portfwd-operator-guide.md §1

  q  close pane
```

The agent is started automatically by `herdr-plugin-msb attach <target>`.
If it still does not connect, see §6.

**Agent connected, no ports in state:**

```
Port forwards — none
─────────────────────────────────────────────
  (no ports declared)

  agent connected 10:05:23

  j/k  select   r  add port (recreates)   q  close
```

**Ports present, cursor on an IDLE port:**

```
Port forwards — my-sandbox
─────────────────────────────────────────────
> 3000   IDLE
  5173   IDLE

  j/k  select   Enter  forward selected   r  add port (recreates)   q  close
```

**A port is LIVE:**

```
Port forwards — my-sandbox
─────────────────────────────────────────────
> 3000   LIVE    since 10:00:30   ctrl+click → http://127.0.0.1:3000
  5173   IDLE

  j/k  select   Enter  enqueue   r  add port (recreates)   q  close
```

**A port is PENDING (request in flight):**

```
Port forwards — my-sandbox
─────────────────────────────────────────────
  3000   LIVE    since 10:00:30
> 5173   PENDING request enqueued, waiting for laptop agent...

  j/k  select   q  close
```

PENDING is normally brief (under 30 seconds). If it persists, the agent is
not running — see §6.

**A port is DEAD:**

```
Port forwards — my-sandbox
─────────────────────────────────────────────
  3000   LIVE    since 10:00:30
> 5173   DEAD    lost 10:03:41   error: master keepalive expired

  j/k  select   Enter  retry   r  add port (recreates)   q  close
```

See Known Limitations §1 — DEAD detection is provisional.

**A port is in ERROR:**

```
Port forwards — my-sandbox
─────────────────────────────────────────────
> 3000   ERROR   host mismatch: declared for "engine-04", agent is "engine-03"

  j/k  select   q  close
```

**A port is EXPIRED:**

```
Port forwards — my-sandbox
─────────────────────────────────────────────
> 3000   EXPIRED request made 12m ago; expired before agent acked (TTL 10m)

  q  close
```

**A port is OUT-OF-RANGE (guest port above 11023):**

```
Port forwards — my-sandbox
─────────────────────────────────────────────
> 40000   OUT-OF-RANGE  recreate required (outside 1024-11023)

  j/k  select   r  add port (recreates)   q  close
```

**Stale state (5 minutes without an update):**

```
Port forwards — my-sandbox
─────────────────────────────────────────────
  3000   LIVE    since 10:00:30
  WARNING: state is 7m old — laptop agent may be down

  j/k  select   Enter  enqueue   r  add port (recreates)   q  close
```

---

## 4. Forwarding a port

### 4.1 Path A — ctrl+click the URL your dev server printed (recommended)

When a dev server prints `http://127.0.0.1:3000` in a herdr pane:

1. **Ctrl+click** the URL.
2. The Port Forwards pane opens (or comes to focus).
3. The clicked port is immediately set to PENDING and a forward request is
   enqueued.
4. The laptop agent picks up the request and applies the forward. The row
   transitions to LIVE.
5. The LIVE row shows `ctrl+click → http://127.0.0.1:3000`. Click it to
   open the page.

The link handler fires on URLs matching `http://127.x.x.x:PORT` (loopback
range) and `http://localhost:PORT`. Non-loopback URLs are rejected with an
error. The port is parsed from the full URL — clicking a URL with no port
number is rejected.

If the clicked port is outside the guest range (1024–11023), the row is set
to OUT-OF-RANGE instead of PENDING. Use `r` to add the port via recreation
(see §5).

**Verified:** `HERDR_PLUGIN_CLICKED_URL` is set to the full URL and
`invocation_source` is `"link_click"` in `HERDR_PLUGIN_CONTEXT_JSON` on a
real ctrl+click. Evidence: RUN B, 2026-09-09, `doc/probes/linkenv-probe.md`.

### 4.2 Path B — open the pane and press Enter

1. Action menu → **"Port forwards"** (or the pane is already open).
2. Navigate with `j`/`k` to the port you want.
3. **Press Enter.**
4. PENDING briefly, then LIVE once the agent confirms.

### Key dispatch reference

| Key     | Cursor is on…       | What happens                                      |
|---------|---------------------|---------------------------------------------------|
| `Enter` | IDLE                | Enqueue forward request → PENDING                 |
| `Enter` | LIVE                | Enqueue forward request (agent decides forward/revoke) |
| `Enter` | DEAD                | Enqueue retry → PENDING                           |
| `Enter` | PENDING             | No-op (request already in flight)                 |
| `Enter` | ERROR or EXPIRED    | No-op                                             |
| `Enter` | OUT-OF-RANGE        | Enqueue request (agent will reject; use `r`)      |
| `r`     | any                 | Show recreate warning (§5)                        |
| `q`     | any                 | Close pane                                        |
| `j`     | any                 | Move cursor down one row                          |
| `k`     | any                 | Move cursor up one row                            |

Arrow keys are not handled; use `j`/`k` only.

---

## 5. Adding a port that was not pre-published

**This destroys all in-guest state.** Running processes, open files, and
anything in the guest's memory are lost. A new sandbox is created (~350ms,
but nothing survives the recreate).

Press `r` in the pane. The pane shows:

```
! Adding a port requires recreating this sandbox.

  ALL IN-GUEST STATE WILL BE DESTROYED.
  Running processes, open files, and in-memory state will be lost.
  Your current session ends. (~350ms downtime.)

  Press Enter to confirm recreation, or q to cancel.
```

Press **Enter** to confirm or any other key to return to the pane without
changes.

**Note: the recreate action is not yet implemented.** After pressing Enter,
the pane prints `recreation not yet implemented (Slice C)` and exits. The
warning and confirmation prompt are wired; the actual recreate path is not.

The structural reason recreation is destructive: microsandbox v0.6.17 has
no post-hoc port publish surface. `WithPorts` is accepted only at
`CreateSandbox`. Adding a port after boot requires Kill → Remove →
CreateAndBoot with the full new port set. Evidence: `doc/port-publish.md`.

---

## 6. Failure states and what to do

### Agent not running — PENDING stays, or Screen 1 (no state file)

The laptop agent is not running or has not connected. Start it:

```
herdr-plugin-msb local-agent --target <user@engine>
```

This occupies the terminal for the session. Once it connects, the pane
updates and pending requests are processed.

### DEAD — forward was live, now it is not

**Provisional — see Known Limitations §1.** Press `Enter` on the DEAD row
to retry. The agent re-establishes the ControlMaster and reapplies the
forward. If the row stays DEAD, the agent may not be running (check for
the PENDING-stays symptom above).

Common agent-reported cause: SSH ControlMaster disconnected after laptop
sleep (`master keepalive expired`).

### ERROR — host mismatch

The forward was declared for a different engine than the one the agent is
watching. Re-enqueue by ctrl+clicking the URL again, which writes a fresh
request for the current engine.

### EXPIRED — agent was not running when the request was made

The request aged out (TTL 10 minutes) before the agent processed it.
Re-enqueue by ctrl+clicking the URL or pressing Enter on the row (it will
stay in ERROR or EXPIRED until the state is refreshed by the agent on next
connect).

### Stale state (WARNING in pane)

`forwards.state` has not been updated in more than 5 minutes. The agent
may be down. Check as above.

---

## 7. Security posture

This section describes the standing containment conditions. Read it before
enabling forwarding on a machine other accounts can reach.

**Egress deny is applied to every sandbox.** The plugin sets
`DefaultEgress: deny` in the microsandbox network config. Only DNS and
`api.anthropic.com:443` are permitted outbound. The real Claude credential
is mounted into every guest (read-write), so the egress policy is the
principal containment layer. This was measured, not inferred: an
ephemeral run with no policy reached `https://example.com/` with real
response HTML before the fix was applied. A regression test
(`internal/runtime/msb/loopback_invariant_live_test.go`) and
`doc/egress-profile.md` record the measured behaviour and guard the
invariant on every create path.

**Ingress is not filtered.** `networkConfig()` sets `DefaultEgress` and
nothing else; the microsandbox ingress default is `allow`. Every published
port accepts inbound connections from the engine without restriction. A
guest service that echoes data to any inbound connection therefore bypasses
`default_egress=deny` entirely: the connection arrives inbound, which the
policy does not cover, and secret material can leave through the response.
The practical exposure: any unauthenticated service an agent starts in the
guest on ports 1024–11023 is reachable by anything that can reach those
host ports. `DefaultIngress: deny` would close this gap but has not been
shipped.

**Published ports bind the engine's `127.0.0.1`.** A loopback bind is not
a privilege boundary on Linux: any account able to log into the engine can
reach every port a sandbox publishes. On a single-operator engine this is
acceptable. If the machine becomes genuinely shared, the combination of
ingress-allow and per-account loopback reachability stops being acceptable
and needs revisiting.

---

## 8. Known limitations

### 1. DEAD detection is provisional (OQ-3 open)

The `dead` status exists in the state type and is rendered by the pane, but
no production code currently transitions a forward to `dead`. The transition
requires confirming that a post-sleep SSH ControlMaster that answers
`ssh -O check` with exit 0 is actually passing traffic — this depends on
open question OQ-3 (unresolved; requires the operator to sleep and wake
their laptop to reproduce).

Until OQ-3 is resolved, the pane may show a forward as `LIVE` when the
underlying tunnel is dead. The 5-minute stale-state warning catches the case
where the agent has stopped writing state entirely, but it does not catch a
tunnel that looks alive to the agent and is not.

**What to do:** if a LIVE port is not reachable, press `Enter` to
re-enqueue. The agent will attempt to re-establish the forward.

### 2. Cross-sandbox reach is partially proven

A connection from one sandbox to another sandbox's published port travels:
guest-A → engine loopback → published host port → guest-B. The engine
loopback hop (guest reaches a published host port) is proven. The full
path — guest-A reaching guest-B's service through the engine's loopback
stack — is unproven. This requires input on engine-side routing rules and
has not been measured.

### 4. Adding a port requires recreating the sandbox (not yet implemented)

See §5 for the recreate stub: the confirmation prompt is wired but the actual
recreate path is not (prints `recreation not yet implemented (Slice C)` and exits).

### 3. Alias and IP forms of the same engine yield two SSH masters

`--target engine-03` and `--target 100.64.0.156` each create a separate
ControlMaster because the ControlPath is keyed on the operator-supplied target
string. This is deliberate — alias and IP are not guaranteed to resolve to the
same host — but it means forwarding via one form after connecting via another
leaves a second master open. Use one form consistently within a session to avoid
accumulating idle masters.
