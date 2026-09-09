# Port forwarding — operator guide

This document is addressed to an **operator**, not an implementer. It describes
what to do, what to click, what to expect, and what to do when things go wrong.
No Go struct names, no internal file paths.

Where this guide references something not yet built, the section is marked:

> **REQUIRES** `sX` — [slice name]

These marks are explicit obligations on the wave-3 implementation slices, not
optional notes. The operator experience described here cannot be delivered until
each named slice ships.

Where the guide relies on a claim that has not been directly observed — only
inferred from source, binary strings, or analogy — the section is marked:

> **INFERRED** — [what is inferred and why it matters]

Inferred claims cannot be cited as test assertions. They are adequate grounds for
design, not for declaring something proved.

---

## What port forwarding does

A dev server running inside a sandbox on `engine-03` listens on a loopback
address inside the guest. Port forwarding makes that port reachable in a browser
on your laptop at `http://127.0.0.1:<port>`.

The tunnel is an SSH forward through the existing ControlMaster connection.
**The sandbox does not need to be recreated to open or close a tunnel.**
Toggling a forward only starts or stops the SSH tunnel; it does not touch the
sandbox.

Exception: if you need a port that was **not pre-published when the sandbox was
created**, adding it *does* require recreation and destroys all in-guest state.
See §6.

---

## 1. One-time setup: install the local agent

The tunnel lives on your **laptop**, not on the engine. A background process
(the local agent) opens and monitors the SSH connection. You install it once; it
starts automatically on every login.

**How to install:**

1. Open herdr's action menu (the `>` or `⌘K`-style menu, depending on your
   herdr keybindings).
2. Select **"Install local agent (launchd)"**.
3. Done. The agent starts within a few seconds and runs on every boot.

No command to type. No flags to remember.

**To uninstall:** action menu → **"Uninstall local agent (launchd)"**.

> **REQUIRES** `sL` — Slice L (launchd service)  
> The `install-launchd` and `uninstall-launchd` actions, the binary subcommand
> they invoke, and the plist generation must be implemented. Until they exist,
> start the agent by hand:
> ```
> herdr-plugin-msb local-agent --target <user@engine>
> ```
> That command occupies a terminal for the session.

---

## 2. Opening a space: choose ports before the sandbox exists

When you run `space-convert` on a worktree, the plugin presents a port checklist
**before** creating the sandbox. Select the ports your project needs now; you
cannot add them without destroying the sandbox later.

**The checklist screen** (example):

```
space-convert — choose ports to pre-publish
────────────────────────────────────────────
Workspace: w8   Repo: my-project

Detected from package.json / vite.config.ts:
  [x]  3000   next dev / express
  [x]  5173   vite

Common (not detected):
  [ ]  4200   angular dev
  [ ]  8080   generic HTTP
  [ ]  8000   python -m http.server
  [ ]  9000   webpack-dev-server

  j/k  select   Space  toggle   Enter  confirm and create sandbox
```

No typing. Navigate with `j`/`k`, toggle with `Space`, confirm with `Enter`.

**Auto-detection source:** the plugin reads `package.json`, `vite.config.ts`,
`Cargo.toml`, and `pyproject.toml` for known port patterns and pre-checks
detected ports. Common ports are shown but unchecked.

**To pin ports for a project permanently:** add a `.herdr-ports` file to your
repo root. One port per line, `#` for comments. Ports listed there are
pre-checked in the checklist and survive re-running `space-convert`.

```
# ports to pre-publish at space-convert
3000
5173
4321
```

> **REQUIRES** `sC` — Slice C (pre-published ports at space-convert)  
> The checklist TUI, `.herdr-ports` parsing, and port-history pre-check must be
> implemented. Until they exist, ports are not pre-published at convert time; you
> would need to pass ports manually via the underlying `msb create` flags.

---

## 3. The Port Forwards pane

The primary operator surface is the **Port Forwards** pane: a live status panel
for all pre-published ports on the current sandbox.

**How to open it** (two paths, no typing):

- Action menu → **"Port forwards"** — opens or focuses the pane.
- `Ctrl+click` any `http://127.0.0.1:<port>` link printed by a dev server in a
  herdr pane — the pane opens automatically and the row for that port is
  highlighted. Proven: `HERDR_PLUGIN_CLICKED_URL` is set to the full URL on a
  real click; `invocation_source` is `"link_click"`. Evidence: RUN B, 2026-09-09,
  `doc/probes/linkenv-probe.md`.

Once open, the pane stays open and updates every second. You do not need to
reopen it.

### 3.1 What you see — screen states

**No local agent running:**

```
Port forwards — w8-abc123
─────────────────────────────────────────────
  laptop agent not connected
  forwards.state not found

  If the launchd service is installed it will connect within 30s.
  To install: open action menu → "Install local agent"

  q  close pane
```

**Agent connected, ports idle (nothing forwarded yet):**

```
Port forwards — w8-abc123
─────────────────────────────────────────────
> 3000   IDLE
  5173   IDLE
  8080   IDLE

  agent connected 10:05:23

  j/k  select   Enter  forward selected   r  add port (recreates)   q  close
```

**Port 3000 is live:**

```
Port forwards — w8-abc123
─────────────────────────────────────────────
  3000   LIVE    since 10:00:30   ctrl+click → http://127.0.0.1:3000
> 5173   IDLE
  8080   IDLE

  j/k  select   Enter  toggle   r  add port (recreates)   q  close
```

The LIVE row shows the `ctrl+click` URL. Click it to open the page; click it
again to toggle the forward off (via §4.2 below).

**Port 5173 dead:**

```
Port forwards — w8-abc123
─────────────────────────────────────────────
  3000   LIVE    since 10:00:30
> 5173   DEAD    lost 10:03:41   error: master keepalive expired
  8080   IDLE

  j/k  select   Enter  retry   r  add port (recreates)   q  close
```

**After pressing Enter (brief transient):**

```
Port forwards — w8-abc123
─────────────────────────────────────────────
  3000   LIVE    since 10:00:30
> 5173   PENDING request enqueued, waiting for laptop agent...
  8080   IDLE

  j/k  select   q  close
```

PENDING is brief (normally under 10 seconds). If it persists, the agent is not
connected — see §7.

**State file stale (agent may be down):**

```
Port forwards — w8-abc123
─────────────────────────────────────────────
  3000   LIVE    since 10:00:30
                 WARNING: state is 5m old — laptop agent may be down

  j/k  select   Enter  toggle   r  add port (recreates)   q  close
```

> **REQUIRES** `sP` — Slice P (ports-pane TUI binary) and `s3` — Slice 3
> (forwards.state writeback)  
> The TUI process, the state file it polls, and the state file the laptop agent
> writes must all be implemented. Until they exist, this pane does not exist.
> The only status today is `herdr-plugin-msb status`, which reports queue
> counters — not reachability.

> **REQUIRES** `sP` for the `ports-open` action  
> The action menu entry **"Port forwards"** and the `[[panes]]` manifest entry
> must be added in the same slice.

---

## 4. Forwarding a port

### 4.1 Path A — ctrl+click the URL your dev server printed (recommended)

When the dev server prints `http://127.0.0.1:3000` (or any `http://127.0.0.1:<port>`
URL) in a herdr pane:

1. **Ctrl+click** the URL.
2. The Port Forwards pane opens (or comes to focus) with the cursor on port 3000.
3. **Press Enter** to forward it.
4. The row shows PENDING briefly, then transitions to LIVE.
5. The LIVE row shows the `ctrl+click` URL. Click it to open the page.

Every state change is visible. The pane opening is your confirmation that the
click was received. PENDING is your confirmation that the request was enqueued.
LIVE is your confirmation that the tunnel is up and verified.

Proven mechanism: `HERDR_PLUGIN_CLICKED_URL` set to the full URL, `invocation_source`
set to `"link_click"` in `HERDR_PLUGIN_CONTEXT_JSON` — RUN B, 2026-09-09.

> **REQUIRES** `s7` — Slice 7 (port-hint from link-handler)  
> The `port-hint` binary subcommand, the `[[link_handlers]]` manifest entry, and
> the pane's port-hint polling loop (which moves the cursor to the hinted row and
> deletes the hint) must be implemented.

### 4.2 Path B — open the pane and press Enter

1. Action menu → **"Port forwards"** (or the pane is already open).
2. Navigate with `j`/`k` to the port you want.
3. **Press Enter.**
4. PENDING briefly, then LIVE.

No typing. No host flags. No second command.

### Key dispatch reference

| Key     | When cursor is on… | What happens                                  |
|---------|---------------------|-----------------------------------------------|
| `Enter` | IDLE                | Enqueue forward request                       |
| `Enter` | LIVE                | Enqueue revoke (stop forwarding)              |
| `Enter` | DEAD                | Enqueue retry                                 |
| `Enter` | PENDING             | No-op (request already in flight)             |
| `Enter` | ERROR or EXPIRED    | No-op (use `d` to re-declare)                 |
| `d`     | ERROR or EXPIRED    | Clear status, re-enqueue forward              |
| `r`     | any                 | Add a port that was not pre-published (§6)    |
| `q`     | any                 | Close pane                                    |
| `j`/`↓` | any                | Move cursor down                              |
| `k`/`↑` | any                | Move cursor up                                |

---

## 5. Stopping a forward

Navigate to the LIVE row, press **Enter**. The row transitions to PENDING, then
to IDLE once the laptop agent confirms the listener is gone.

The ctrl+click path also stops forwards: if you ctrl+click a URL whose port is
currently LIVE, the pane opens with that row selected. Press Enter to toggle it
off.

---

## 6. Adding a port that was not pre-published

**This destroys all in-guest state.** Running processes, open files, and anything
in the guest's memory are gone. Your current session ends. A new sandbox is
created (~350ms, but nothing survives the recreate).

Press `r` in the Port Forwards pane. You will see:

```
Port forwards — w8-abc123
─────────────────────────────────────────────
! Adding a port requires recreating this sandbox.

  ALL IN-GUEST STATE WILL BE DESTROYED.
  Running processes, open files, and in-memory state will be lost.
  Your current session ends. (~350ms downtime.)

  Select ports for the new sandbox:
  [x]  3000   currently live (carried over)
  [ ]  4321   requested
  [x]  5173   currently dead (carried over)
  [ ]  8080   currently idle (deselect to skip)

  j/k  select   Space  toggle   Enter  confirm recreation   q  cancel
```

The warning is not skippable. Currently-live and currently-dead ports are
pre-checked for continuity. Press `q` to cancel without any change.

**This path never fires silently.** Proven constraint: `ModifyOptions` in the
microsandbox SDK v0.6.17 has no `Ports` field. `WithPorts` is accepted only at
`CreateSandbox`. Adding a port after boot requires Kill → Remove → CreateAndBoot
with the new port set. Evidence: `doc/port-publish.md`.

> **REQUIRES** `sC` (Slice C) for the recreation path triggered from the pane  
> Screen 8 in the pane delegates to the `space-convert` recreate path. Both
> must exist before this gesture works end-to-end.

---

## 7. Failure states and what to do

### DEAD — forward was live, now it is not

The pane shows `DEAD` with an error message. Common causes:

- **`master keepalive expired`** — the SSH ControlMaster disconnected (usually
  after laptop sleep). Press `Enter` to retry. The agent re-establishes the
  master and reapplies the forward.
- The guest process stopped. The forward itself is still valid; there is just
  nothing to reach. The port stays DEAD until the guest process restarts.

> **INFERRED** (OQ-3) — Post-sleep ControlMaster reliability  
> Whether a post-sleep master reliably answers `ssh -O check` with exit 0, making
> a DEAD forward *look* alive, is not yet measured on this laptop/engine pair.
> Until OQ-3 is measured with two outcomes (sleep/wake with and without
> `ServerAliveInterval`), the DEAD state must be treated as **provisional**: it
> is possible that DEAD detection is delayed or missed after laptop sleep. OQ-3
> affects Slice N (push-on-DEAD) only; the pane's pull path is OQ-3-independent.

**What to do:** press `Enter` to retry. If the port goes PENDING and stays there,
the agent is not connected — check the pane header or Screen 1.

### ERROR — host mismatch

```
Port forwards — w8-abc123
─────────────────────────────────────────────
> 3000   ERROR   host mismatch: declared for "engine-04", agent is "engine-03"
                 request left pending — check -host flag or re-declare
```

The forward was declared for a different engine than the one the agent is
watching. Press `d` to re-declare the port for the correct engine.

This error cannot occur on ports pre-published at `space-convert` time (the
plugin derives the host automatically). It can appear on old requests that
pre-date the fix described in portfwd-toggle-and-status.md §A-2.

### EXPIRED — agent was not running when the request was made

```
Port forwards — w8-abc123
─────────────────────────────────────────────
> 3000   EXPIRED request made 12m ago; expired before agent acked (TTL 10m)
                 agent may not be running — check launchd or start manually
```

The request was enqueued but the agent did not process it within 10 minutes.
Press `d` to re-enqueue. If the agent is not installed, see §1.

### Pane shows Screen 1 (agent not connected)

The local agent is not running or has not connected yet.

- If launchd is installed, wait up to 30 seconds for reconnect.
- If launchd is not installed, start the agent manually: action menu → **"Install
  local agent (launchd)"** — then wait 30 seconds.
- If the pane stays on Screen 1 after 60 seconds, open a terminal and run
  `herdr-plugin-msb local-agent --target <user@engine>` to see the error output.

### Stale state (Screen 9)

The state file has not been updated in more than 5 minutes. The agent may be
down or the master may have staled. Check as above.

---

## 8. How the pane is pushed to you (DEAD transitions)

When a port goes DEAD, the agent can raise the Port Forwards pane on the engine
automatically, without you having to open it. You would see the pane appear
mid-session showing the DEAD row.

This is **Option 3 in the design** (push-on-DEAD).

> **REQUIRES** `sN` — Slice N (agent-push on DEAD transitions)  
> **GATED on OQ-3.** This slice must not ship until OQ-3 is measured with two
> outcomes (sleep/wake with and without `ServerAliveInterval`). A push that fires
> against a stale master is invisible noise. The pull path (open the pane, see
> the status) works regardless of OQ-3.

Until Slice N ships, DEAD detection is pull-only: you open the pane and check.
Screen 9 (stale state warning) tells you the agent may be down before you
conclude the forward is healthy.

---

## 9. Implementation obligations on wave-3 slices

This section is addressed to implementers. Each entry is a hard requirement
derived from the operator narrative above; the operator experience cannot be
delivered without it.

### `s3` — Slice 3: forwards.state writeback

The laptop agent must write `~/.local/state/herdr-plugin-msb/forwards.state`
on the engine after every tick that changes port state (apply, revoke, Present
check, host mismatch, TTL expiry). Write must be atomic (temp-file + rename).

`status` values the pane depends on: `live`, `pending`, `dead`, `error`,
`expired`. The pane polls this file on a one-second tick; if the file is absent,
the pane shows Screen 1.

The `Present` check after apply (immediately after `ssh -O forward` returns 0)
must be included — this is what converts a queue-counter ack into a
laptop-derived reachability signal. `Forwarder.Present` already exists
(`forward.go:109-127`); wiring it is what this slice adds.

### `sP` — Slice P: ports-pane TUI binary

The `ports-pane` verb must implement all nine screens from §3.1 (this guide),
keyed on `forwards.state` content. Key dispatch must follow §4 above exactly.
`Notifier.Notify` must assert `type == "plugin_pane_opened"` in the JSON
response body in addition to exit-code check.

Three things must be passed explicitly to `herdr plugin pane open` wherever it
is called:
- `--placement tab` (the overlay default targets the active pane; an SSH exec
  channel has none — omitting this crashes the open).
- `--workspace <ID>` (without it the pane lands in whatever herdr thinks is
  active — this has already opened a pane in the operator's face).
- The herdr binary path must not be read naively from `/proc/<pid>/exe` — a
  running daemon holds the pre-update inode with a literal ` (deleted)` suffix.

All three are proven constraints. Evidence: portfwd-toggle-and-status.md §Basis
(Finding 2, RE-RUN THROUGH A REAL SSH EXEC CHANNEL).

On Enter: append a `Request` with `op: "forward"` or `op: "revoke"` to
`requests.json`. The `op` and `sandbox` fields are added by Slice 1.

### `sC` — Slice C: pre-published ports at space-convert

The port checklist TUI (§2 above) must run during `space-convert` before sandbox
creation. Auto-detection must read `package.json`, `vite.config.ts`,
`Cargo.toml`, `pyproject.toml`. `.herdr-ports` must be read from the repo root
when present. Port-history pre-check must read from
`~/.local/state/herdr-plugin-msb/port-history.json`.

Confirmed ports must be passed as `WithPorts` to `CreateSandbox`. No ports may
be published silently; the checklist must always appear.

Screen 8 (add port, warn and confirm recreation) delegates to this path. The
recreate path must warn, require explicit Enter to confirm, and never proceed
silently.

### `s7` — Slice 7: port-hint from link-handler

The `port-hint` binary subcommand must:

1. Read `HERDR_PLUGIN_CLICKED_URL` from the environment (the full URL — proven
   delivery channel, RUN B 2026-09-09).
2. Parse the port out of the URL (`net/url` or split on `:`; the value is
   `http://127.0.0.1:3000`, not `3000`).
3. Branch on `invocation_source` in `HERDR_PLUGIN_CONTEXT_JSON` — `"link_click"`
   vs. `"cli"` — so one action can serve both paths without a second verb.
4. Atomically write `<state_dir>/port-hint` containing the port number.
5. Call `herdr plugin pane open --plugin herdr-plugin-msb --pane-id ports`
   (with `--placement tab` and `--workspace <ID>` — see Slice P above).

The `ports-pane` TUI must poll `port-hint` on each tick: if the file is present
and newer than the last render, move the cursor to that port's row and delete the
file. The operator then presses Enter.

The `[[link_handlers]]` manifest entry must be added to `herdr-plugin.toml`.

Step 5 (the pane open) is **not optional**. Omitting it means the click fires
silently and the operator reports it "did nothing" — this happened during the
probe (linkenv-probe.md; portfwd-toggle-and-status.md §B-4).

---

## 10. Inferred items — must not be cited as test assertions

Five things in this guide rely on inference, not direct measurement. Every place
this guide leans on them is marked above. Consolidated here for visibility:

**INFERRED-1 — PTY stdin read-blocks because herdr holds the PTY master.**  
The pane-TUI design depends on the `ports-pane` process being able to block on
stdin keypresses. The probe showed `/dev/pts/5` as stdin and `read` blocking in
the pane shell (`portfwd-toggle-and-status.md §Basis, Finding 1`). This is
adequate for design viability. It is NOT a test assertion that a ratatui TUI
receiving keypresses will behave identically — that requires a live test of the
actual `ports-pane` binary in a pane.

**INFERRED-2 — `[[actions]]` field list is complete.**  
The `[[actions]]` fields (`id`, `title`, `command`, `contexts`, `platforms`) are
observed from this repo's own manifest and from `herdr plugin action list`
output. Absence of a `prompt`/`parameter` field from a manifest that doesn't use
one does not establish that the field is absent from the parser. herdr's source
is not on this machine. The pane-TUI surface does not depend on this being
resolved; it routes all per-port interaction through the TUI, not through action
parameters.

**INFERRED-3 — OQ-3: post-sleep ControlMaster reliability (OPEN, DEFERRED).**  
Whether `ssh -O check` returns 0 on a post-sleep master is not yet measured on
this laptop/engine pair. Affects Slice N (push-on-DEAD) only. The DEAD state in
the pane and the stale-state warning (Screen 9) are provisionally correct but
may be delayed after laptop sleep. Any phrasing in this guide about DEAD
detection timeliness is provisional and tied to OQ-3.

**INFERRED-4 — macOS ssh client behaviour.**  
The exec proof ran engine-to-engine over localhost. The operator's laptop is
macOS. `Forwarder.Present` already has a `netstat -an -p tcp` macOS fallback
(`forward.go:118-127`), which is encouraging. macOS-specific ssh behaviour under
sleep and `ServerAliveInterval` must be measured on the actual laptop/engine
pair, not inferred from the Linux-side test.

**INFERRED-5 — setsid TTY control.**  
`setsid sh -c 'herdr plugin pane open ...'` was used in one step of the pane-open
proof. The load-bearing evidence is the re-run through a real SSH exec channel.
`setsid` is NOT cited as proof that `pane open` needs no TTY; that argument is
invalid (`portfwd-toggle-and-status.md §Basis, CONTROL REPAIRED, WITH A LIMIT`).
Do not resurrect it.
