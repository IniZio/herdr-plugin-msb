# Port-forward toggle and status UX — design

Design only. No production code changes. Extends `portfwd-ux.md` (committed
590a64d); read it first. This document covers two operator problems named in the
verdict: toggling a forward on and off, and knowing what is actually forwarded.
Friction points F-n and open questions OQ-n refer to that prior doc.

Where this document departs from `portfwd-ux.md`, the departure is marked
**DEPARTURE** and the reason given.

---

## Basis for this revision

Four findings established after the initial draft force material changes to the
operator surface, the pre-publish story, and the option ranking.

**Finding 1 — herdr 0.8.0 capability gaps. SOURCING CORRECTED:** an earlier
revision of this section claimed these were "VERIFIED from help text and manifest
schema". There is no `herdr manifest schema` command — `herdr manifest` returns
"unknown command" — so that citation was false. Each claim below now carries its
actual source and confidence. This matters because the same doc series already
attributed a non-existent "action argument prompt" to herdr (retracted in
c9d0d03); a second unsourced capability claim is the same failure repeating.

- `herdr plugin action invoke` accepts only `--plugin` and a positional
  `<ACTION_ID>`. **VERIFIED** — `herdr plugin action invoke --help` was run.
- `[[actions]]` is *observed* to carry `id`, `title`, `command`, `contexts`,
  `platforms` (from this repo's own manifest and from `herdr plugin action list`
  output, which is herdr's own serialization). **INFERRED, NOT VERIFIED**, that no
  parameter/prompt/substitution field *exists*: absence of a field from a manifest
  that does not use one cannot establish absence from the parser, and herdr's
  source is not on this machine. The design does not depend on this being airtight
  — the pane-TUI surface needs no action parameter either way — but if a future
  need turns on it, read herdr's source or docs rather than re-deriving it here.
- Actions cannot be registered at runtime. `herdr plugin action` has only `list`
  and `invoke`. Per-port "Unforward 3000" menu entries are not expressible.
- Panes CANNOT route keypresses back to the plugin via herdr protocol. `herdr
  plugin pane` has only `open`, `focus`, `close`.
- **BUT**: a `[[panes]]` entry launches an arbitrary command in a real PTY. That
  process owns its terminal and reads stdin freely. A plugin-owned TUI running
  inside a pane renders status and accepts keypresses with zero herdr protocol
  involvement. **This is the unlock — it becomes the primary operator surface.**

  **DEMONSTRATED**, after an advisor gate found this had been asserted rather than
  shown. A probe `[[panes]]` entrypoint running `tty; ls -la /proc/self/fd/0; read`
  produced, verbatim:

  ```
  /dev/pts/5
  lrwx------ 1 newman newman 64 Sep  9 08:48 /proc/self/fd/0 -> /dev/pts/5
  --- reading (blocks if real PTY)
  ```

  The output ends there: `read` blocked rather than returning on EOF. A real PTY,
  stdin bound to it, and a blocking read — the three things the TUI surface needs.
  Not tested: keypress delivery from a human, and whether input can be injected
  programmatically through the herdr API. Neither affects the surface being viable.
- herdr has no plugin autostart hook (no `startup/session.started` event, no
  `daemon/service` subcommand). The laptop `local-agent` must be started by
  launchd on macOS. That is an OS-layer deliverable; design it explicitly (§B-5
  and Slice L).

**Finding 2 — pane relay PROVEN (closes OQ-2 from portfwd-ux.md §5):**

`herdr plugin pane open` requires no TTY. Proven with:

```
setsid sh -c 'herdr plugin pane open ... < /dev/null'
→ {"type":"plugin_pane_opened","pane_id":...}
```

Negative control: nonexistent plugin → `{"error":{"code":"plugin_not_found"}}`,
exit 1. So the laptop agent can raise a pane on the engine by calling
`PaneOpenArgv` (cmd_herdr_plugin.go:261-268) over its existing SSH ControlMaster
via the existing `ExecArgv` helper (:104-106). No local herdr socket needed.
Agent-push IS available; Option 3 is no longer blocked on OQ-3 for availability.

**CONTROL REPAIRED, WITH A LIMIT — read this before citing it.** The `setsid`
control above varied the *plugin name*, not the TTY condition, so it could not
distinguish "tolerates no TTY" from "never checks". Bare `herdr` through the
identical wrapper panics — `failed to initialize terminal: Os { code: 6, ...
"No such device or address" }`, exit 101 — which establishes that the wrapper
strips the controlling terminal in a way the OS enforces.

It does NOT establish that `herdr plugin pane open` is TTY-independent. The TUI
calls into ratatui and needs a terminal; `pane open` writes JSON to a Unix socket
and exits. One failing without a TTY licenses nothing about the other tolerating
its absence — they are different classes of program. **Never cite this control
standalone as proof that `pane open` needs no TTY; that argument is invalid.**
The load-bearing evidence is the ssh-exec re-run below, which exercises the real
command in the real environment.

**RE-RUN THROUGH A REAL SSH EXEC CHANNEL**, since `setsid` only simulates one.
Opened pane `w83:pF` with `"type":"plugin_pane_opened"`, confirmed in
`herdr pane list`, then closed; invalid plugin id gave `plugin_not_found`/exit 1.
Three requirements the implementation MUST supply explicitly:

- **`--placement tab`** (or `split`). The default is an overlay targeting *the
  active pane*, and an ssh exec channel has none. Omitting it fails.
- **`--workspace <ID>`**. Without it the pane lands in whatever workspace herdr
  considers active. This is why an earlier probe opened a pane in the operator's
  own w8 — a missing flag, not a one-off slip, so it will recur without this.
- **`undeletedHerdrBin`** for any path read from `/proc/<pid>/exe`. Confirmed live:
  `/proc/8365/exe -> /home/newman/.local/bin/herdr (deleted)`. The daemon holds the
  pre-update inode and the literal ` (deleted)` suffix is part of the string.

`HERDR_SOCKET_PATH` is *not* forwarded by sshd, but herdr's default socket path
resolved correctly, so it need not be supplied. `herdr` does resolve on PATH in a
non-login ssh exec shell (`/home/newman/.local/bin/herdr`).

Carry-forward from this proof: `Notifier.Notify` currently checks only exit code
(:281). It must additionally assert `type == "plugin_pane_opened"` in the JSON
response body. Fold into Slice P.

OQ-3 (does a post-sleep ControlMaster answer `ssh -O check` with 0?) is still
open — it affects reliability of DEAD detection, not availability. The pane-TUI
pull path is OQ-3-independent. See §F.

**Finding 3 — sandbox ports are CREATE-TIME ONLY (VERIFIED at the SDK):**

SDK v0.6.17 `ModifyOptions` has no `Ports` and no `Network` field. `WithPorts`
is a `SandboxOption` accepted only by `CreateSandbox`. `msb modify --help` has no
`--port`. `internal/runtime/msb/publish.go` implements publishing as Kill →
Remove → CreateAndBoot. A marker-file test (`doc/port-publish.md`) proves all
in-guest state is destroyed (~350ms, but the wall time is irrelevant — nothing
survives).

Consequence: the toggle gesture controls ONLY the SSH tunnel. Sandbox port
publication must be decided at `space-convert` time. Ports not pre-published at
create time require recreation, with an explicit warning and confirmation, never
silent. See §C.

Rejected approach — guest-initiated reverse tunnel: rejected because it requires
engine-03 in the sandbox egress allowlist. CLAUDE.md states containment rests
entirely on the network profile on the machine holding the real credential mount.
Adding the engine as an egress target opens a permanent out-of-band channel from
the credential-holding guest. Record as closed; do not revisit without a
containment model change.

**Finding 4 — clicked-URL delivery unproven (RUN B not yet run):**

RUN A (negative control) showed `HERDR_PLUGIN_CLICKED_URL` absent and argv empty
on a direct `herdr plugin action invoke`. RUN B (a real ctrl+click in the herdr
TUI) has not been run. Both outcomes are designed below (§B-4). Do not implement
Slice 7 until RUN B's outcome is known.

---

## A. Protocol change

### A-1. Why the current schema cannot express what we need

`Request` today (`:38-49`) has three useful fields: `Host`, `RemotePort`,
`LocalPort`. Two things are inexpressible:

1. "Stop forwarding port 3000" — the queue is append-only and has no operation
   field.
2. "What is actually live right now" — `applied` is in-memory (`:379-381`);
   nothing writes it back. `status` reads a queue counter, not state.

Both share one root: the protocol carries intentions, not state.

### A-2. Add `Op` and `Sandbox` to `Request`

**DEPARTURE from portfwd-ux.md §3.4**: that section proposes the same two fields
in Phase 4 (last). Moving them to Slice 1: they are the minimum change that makes
revoke expressible, and revoke is the operator's stated unmet need.

New `Request` JSON shape (fields added to the existing struct; backward-compatible
when `op` is absent):

```json
{
  "id":          12,
  "host":        "engine-03",
  "remote_port": 3000,
  "local_port":  3000,
  "op":          "forward",
  "sandbox":     "w8-abc123",
  "ts":          "2026-09-09T10:00:00Z"
}
```

`op` is `"forward"` (today's only behaviour) or `"revoke"`. When `op` is absent
in an existing entry, the agent treats it as `"forward"` — no migration needed.
New agents reading old files are safe. Old agents reading new files will attempt
to forward a revoke entry — wrong but recoverable once the new agent is installed.

`sandbox` carries the sandbox ID (as returned by `Runtime.List`). A zero value
(`""`) means "host-level, not sandbox-scoped" — preserves compatibility with
existing entries.

### A-3. `forwards.state` writeback file

**DEPARTURE from portfwd-ux.md §3.5**: that section defers the writeback design
until OQ-3 resolves. Making it unconditional here: OQ-3 governs whether the
laptop can push a pane directly, but the pane-TUI (§B-1) polls the file on its
own tick. The pull path is OQ-3-independent and is the foundation of everything
in §B.

The laptop agent writes `~/.local/state/herdr-plugin-msb/forwards.state` on the
engine via the existing SSH master, using `ExecArgv` (`:104-106`) — the same
transport `RemoteMarkCommand` uses. Write is atomic: write to a temp file, rename
over the target (avoids EBUSY; see project memory "Claude Code writes credentials
atomically").

`forwards.state` shape:

```json
{
  "written_by":  "newman@mba",
  "updated_at":  "2026-09-09T10:05:23Z",
  "forwards": [
    {
      "port":         3000,
      "sandbox":      "w8-abc123",
      "status":       "live",
      "confirmed_at": "2026-09-09T10:00:30Z",
      "error":        ""
    },
    {
      "port":         8080,
      "sandbox":      "w8-abc123",
      "status":       "dead",
      "confirmed_at": "2026-09-09T09:58:41Z",
      "error":        "master keepalive expired"
    },
    {
      "port":         3001,
      "sandbox":      "w8-abc123",
      "status":       "error",
      "confirmed_at": "",
      "error":        "host mismatch: declared for \"engine-04\", agent is \"engine-03\""
    },
    {
      "port":         9000,
      "sandbox":      "w8-abc123",
      "status":       "expired",
      "confirmed_at": "",
      "error":        "request made 12m ago, expired before agent acked (TTL 10m)"
    }
  ]
}
```

`status` values:

- `live` — `Forwarder.Present(port)` returned true on the laptop.
- `pending` — request acked but `Present` not yet confirmed (brief transient).
- `dead` — previously live; `Present` now returns false.
- `error` — apply failed, host mismatch, or `Present` false immediately after
  apply.
- `expired` — TTL elapsed before the laptop agent acked the request.

The agent rewrites this file on every tick that changes any port's state. The
pane-TUI reads it on each polling interval. If the file does not exist, the pane
says so explicitly (Screen 1 in §B-2).

No new transport needed: `ExecArgv` already runs arbitrary commands over the
existing master. The write is one `cat > <tmpfile> && mv` invocation.

### A-4. Wire existing dead code before adding new code

The following have no non-test caller. Wire them before building new features:

- `CancelApplied` — wired to the revoke branch in Slice 1.
- `Cancel` — wired to the forward-apply failure path in Slice 1.
- `Manager.Reconcile` — wired to the agent's ticker in Slice W.
- `Manager.TeardownSandbox` — wired to sandbox lifecycle events (the
  `worktree.removed` hook path) in Slice W. Caveat: that hook fires only when
  herdr drives removal, not on a plain `git worktree remove`
  (doc/herdr-event-contract.md, A4).
- `DiscoverAll` — called inside `Manager.Reconcile` in Slice W.

### A-5. Is this a breaking change?

No. Old request files without `op` are still valid. `forwards.state` is
additive — nothing today reads it. Old agents reading new revoke entries will
attempt to forward the port; that is wrong but recoverable once upgraded.

---

## B. Operator surface

### B-1. Primary surface: plugin-owned TUI pane

The action menu mechanism is rejected for per-port interaction. Historical record
preserved below; it must stay in this document:

> **RETRACTED — action menu prompts do not exist.** The claim that the operator
> "is prompted for the port number (herdr's standard action argument prompt)" was
> never verified and is false. `herdr plugin action invoke` accepts only
> `--plugin` and a positional `<ACTION_ID>`; the `[[actions]]` schema carries
> only `id`, `title`, `command`, `contexts`, `platforms` — no parameter, prompt,
> or substitution field. An action fires its fixed `command` array verbatim and
> solicits no operator input. Actions are also strictly static: there is no
> runtime registration, so a per-port "Unforward 3000" menu entry is not
> expressible either.
>
> The operator has additionally ruled out typed CLI gestures entirely ("expect no
> external cli call typing needed for users"). Both the `unforward -port N` verb
> and a prompt-based action entry are therefore superseded — see the pane-TUI
> approach, which is the only surface that carries per-port interaction without a
> herdr capability that does not exist.

The replacement: a `[[panes]]` entry in `herdr-plugin.toml`:

```toml
[[panes]]
id      = "ports"
title   = "Port forwards"
command = ["herdr-plugin-msb", "ports-pane"]
```

`ports-pane` is a TUI loop: it reads `forwards.state` on each polling tick (every
second), renders the pre-published port list with current status, and dispatches
on keypresses. The process owns its PTY and stdin. Zero herdr protocol involvement
in any per-port interaction.

The operator opens it from herdr's pane menu (no typing). It stays open; state
updates live. A static action also opens it:

```toml
[[actions]]
id      = "ports-open"
title   = "Port forwards"
command = ["herdr", "plugin", "pane", "open",
           "--plugin", "herdr-plugin-msb", "--pane-id", "ports"]
```

### B-2. Pane screen states — literal renders

All screens assume sandbox `w8-abc123`, pre-published ports `3000 5173 8080`.

**Screen 1 — laptop agent not connected:**
```
Port forwards — w8-abc123
─────────────────────────────────────────────
  laptop agent not connected
  forwards.state not found

  If the launchd service is installed it will connect within 30s.
  To install: open action menu → "Install local agent"

  q  close pane
```

**Screen 2 — agent connected, all ports idle:**
```
Port forwards — w8-abc123
─────────────────────────────────────────────
> 3000   IDLE
  5173   IDLE
  8080   IDLE

  agent connected 10:05:23

  j/k  select   Enter  forward selected   r  add port (recreates)   q  close
```

**Screen 3 — port 3000 live:**
```
Port forwards — w8-abc123
─────────────────────────────────────────────
  3000   LIVE    since 10:00:30   ctrl+click → http://127.0.0.1:3000
> 5173   IDLE
  8080   IDLE

  j/k  select   Enter  toggle   r  add port (recreates)   q  close
```

**Screen 4 — port 5173 dead, cursor on it:**
```
Port forwards — w8-abc123
─────────────────────────────────────────────
  3000   LIVE    since 10:00:30
> 5173   DEAD    lost 10:03:41   error: master keepalive expired
  8080   IDLE

  j/k  select   Enter  retry   r  add port (recreates)   q  close
```

**Screen 5 — pending (brief transient after Enter):**
```
Port forwards — w8-abc123
─────────────────────────────────────────────
  3000   LIVE    since 10:00:30
> 5173   PENDING request enqueued, waiting for laptop agent...
  8080   IDLE

  j/k  select   q  close
```

**Screen 6 — host mismatch error:**
```
Port forwards — w8-abc123
─────────────────────────────────────────────
> 3000   ERROR   host mismatch: declared for "engine-04", agent is "engine-03"
                 request left pending — check -host flag or re-declare

  j/k  select   d  re-declare this port   q  close
```

**Screen 7 — TTL expired:**
```
Port forwards — w8-abc123
─────────────────────────────────────────────
> 3000   EXPIRED request made 12m ago; expired before agent acked (TTL 10m)
                 agent may not be running — check launchd or start manually

  d  re-declare this port   q  close
```

**Screen 8 — port not pre-published (operator pressed `r`):**
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

**Screen 9 — stale state file (agent may be down):**
```
Port forwards — w8-abc123
─────────────────────────────────────────────
  3000   LIVE    since 10:00:30
                 WARNING: state is 5m old — laptop agent may be down

  j/k  select   Enter  toggle   r  add port (recreates)   q  close
```

### B-3. Key dispatch

| Key         | Action                                                                     |
|-------------|----------------------------------------------------------------------------|
| j / ↓       | Move cursor down                                                           |
| k / ↑       | Move cursor up                                                             |
| Enter       | IDLE → enqueue forward; LIVE → enqueue revoke; DEAD → enqueue retry; no-op on PENDING/ERROR/EXPIRED |
| d           | Re-declare selected port (clears error/expired status, re-enqueues forward) |
| r           | Show Screen 8 (add port — warns, confirms, then recreates sandbox)         |
| q           | Close pane                                                                 |

Toggle is symmetrical: Enter on LIVE enqueues a revoke. The pane transitions to
PENDING immediately (optimistic), then to IDLE once the agent confirms
`Present(port)` is false. A revoke for a port not in the applied set surfaces an
error entry in `forwards.state` and Screen 6.

### B-4. Link-handler branch — both outcomes of RUN B

RUN A proved `HERDR_PLUGIN_CLICKED_URL` is absent on direct `herdr plugin action
invoke`. RUN B (ctrl+click in a live herdr session) has not been run. Both
branches are designed here; Slice 7 is gated on the outcome.

**Branch A — URL IS delivered (RUN B confirms the env var is set):**

The link-handler action runs `herdr-plugin-msb port-hint`. The binary:

1. Reads `HERDR_PLUGIN_CLICKED_URL` from its environment.
2. Parses the port from the URL (`strconv.Atoi` on the host:port component).
3. Atomically writes `<state_dir>/port-hint` containing the port number.
4. Calls `herdr plugin pane open --plugin herdr-plugin-msb --pane-id ports`
   to bring the pane to focus or open it if closed.

The `ports-pane` TUI: on each polling tick, reads `port-hint` if present. If
newer than the last render, moves the cursor to that port's row and deletes the
hint file. Operator sees the relevant row highlighted; Enter toggles it.

Gesture: ctrl+click `http://127.0.0.1:3000` → pane focuses → row 3000 selected →
Enter.

Manifest addition (gated on RUN B):

```toml
[[link_handlers]]
id      = "ports-link"
command = ["herdr-plugin-msb", "port-hint"]
```

Also measure: whether `{url}` command substitution exists in argv (the binary
strings suggest it may). Require both outcomes of that measurement before
committing to env-var-only or argv-substitution.

**Branch B — URL is NOT delivered (RUN B shows env var absent on a real click):**

The link-handler can still open the pane. It calls `herdr plugin pane open` but
cannot pre-select a port. The pane opens in its current state; the operator
navigates with j/k.

Manifest (Branch B fallback):

```toml
[[link_handlers]]
id      = "ports-link"
command = ["herdr-plugin-msb", "open-ports-pane"]
```

This is usable: the operator clicks a URL and the pane opens showing all port
statuses. The ctrl+click is not a toggle in Branch B — it is a "show me the
status panel" gesture.

### B-5. Laptop local-agent: launchd design (OS-layer deliverable)

herdr has no plugin autostart hook. On macOS, launchd is the correct mechanism.
This ships as a binary subcommand, not as a herdr config item.

`herdr-plugin-msb install-launchd` reads `--target` from a local config file
(`~/.config/herdr-plugin-msb/config.toml`, set interactively at first run), then
generates and loads this plist:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
    "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.herdr-plugin-msb.local-agent</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/local/bin/herdr-plugin-msb</string>
    <string>local-agent</string>
    <string>--target</string>
    <string>newman@engine-03</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardErrorPath</key>
  <string>/Users/newman/Library/Logs/herdr-plugin-msb/local-agent.err</string>
</dict>
</plist>
```

After install, the service runs on every login. No further operator gestures.

A static action invokes install; the command reads target from local config, no
operator typing:

```toml
[[actions]]
id      = "install-launchd"
title   = "Install local agent (launchd)"
command = ["herdr-plugin-msb", "install-launchd"]

[[actions]]
id      = "uninstall-launchd"
title   = "Uninstall local agent (launchd)"
command = ["herdr-plugin-msb", "uninstall-launchd"]
```

First-run config setup is one-time and interactive (the operator types
`newman@engine-03` exactly once in a terminal when installing the binary). After
that, no typing.

### B-6. Agent-push notifications (OQ-2 closed; OQ-3 still open)

OQ-2 is closed (Finding 2). The laptop agent can call `herdr plugin pane open`
over ControlMaster+ExecArgv to raise the pane on the engine. This enables push:
when a port goes DEAD, the agent brings the pane to focus automatically.

OQ-3 remains open: if the ControlMaster stales after laptop sleep, the agent
cannot detect DEAD until the master reconnects. While OQ-3 is open, Screen 9
(stale state warning) is the fallback — the operator opens the pane and sees the
staleness warning.

The push path (Slice N) is additive over the pull path. Ship Slice P first.
Add Slice N after OQ-3 is measured with two outcomes on this laptop/engine pair.

---

## C. Pre-published ports at space-convert

### C-1. The constraint

Finding 3 establishes that sandbox ports are create-time only. The SSH tunnel
toggle never triggers a recreate. If the operator needs a port not pre-published
at `space-convert` time, the sandbox must be recreated — destroying all in-guest
state. Therefore the operator must choose ports before the sandbox exists, without
typing, with a sensible default, and with explicit warning if recreation is later
needed.

### C-2. Port selection at space-convert

During `space-convert`, before the sandbox is created, a checklist TUI is
presented. Pre-population order:

1. Read `.herdr-ports` from the repo root if present (operator-controlled,
   checked-in file).
2. Detect from repo: scan `package.json` (`scripts`, `devDependencies`),
   `vite.config.ts`, `Cargo.toml`, `pyproject.toml` for known port patterns;
   mark detected ports.
3. Fall back to default list: `3000 4200 5173 8080 8000 9000`.
4. Pre-check any port from the previous sandbox on the same workspace
   (`port-history.json` in state dir).

Literal screen at space-convert:

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

No typing. If the operator needs a port not in the list, they add it to
`.herdr-ports` in the repo root and re-run `space-convert` (a one-time file edit,
not a runtime gesture).

`.herdr-ports` format (optional, repo-committed):
```
# ports to pre-publish at space-convert
3000
5173
4321
```

### C-3. Not-pre-published case

When the operator presses `r` in the pane-TUI (Screen 8), the pane shows the
recreation warning with the full port selection checklist. The operator must:

1. Read the warning explicitly.
2. Select which ports to include in the new sandbox. Currently-live ports are
   pre-checked for continuity.
3. Press Enter to confirm, or `q` to cancel.

Nothing proceeds silently. No in-guest state survives. The SSH tunnel to any
currently-live port drops the moment the sandbox is removed; the new tunnel
applies only after `CreateAndBoot` completes.

The pane optionally writes the confirmed list back to `.herdr-ports` (controlled
by `update_herdr_ports = true` in `~/.config/herdr-plugin-msb/config.toml`;
default off — the operator controls what goes into the repo).

### C-4. Rejected approaches

Silent publish on first use — rejected: in-guest state loss is permanent and
unrecoverable. Any path that destroys state must warn explicitly.

Guest-initiated reverse tunnel — rejected: requires engine-03 in the sandbox
egress allowlist. CLAUDE.md states containment rests entirely on the network
profile on the machine holding the real credential mount. Record closed; do not
revisit without a containment model change.

---

## D. Trade-offs and recommendation

### Option 1 — Status-only pane, CLI toggle

A one-shot `ports-status` action opens a status pane. Toggle is via typed CLI
verbs (`declare`, `unforward -port N`).

**Cost**: toggle requires typing. Rejected by operator constraint ("expect no
external cli call typing needed").

**Benefit**: minimal new code. Not viable as specified.

### Option 2 — Pane-TUI, pull-based (recommended baseline)

The `ports-pane` TUI is the primary surface. Toggle is Enter on the selected row.
Status updates every second from `forwards.state`. The laptop agent writes the
state file over ControlMaster on every tick.

**Cost**: requires `ports-pane` TUI binary (Slice P), `forwards.state` writeback
(Slice 3), space-convert checklist (Slice C), and launchd service (Slice L).

**Benefit**: satisfies the operator constraint in full. No typing in the normal
flow. Status is always visible. OQ-3-independent for the pull path. If the agent
is not running, Screen 1 says so explicitly — never silently wrong.

**Honestly costs to disclose to the operator**: if the laptop is asleep and the
ControlMaster stales, the pane shows Screen 9 (STALE) until the agent reconnects.
The operator may not notice a DEAD port until they open the pane. Option 3
improves this.

### Option 3 — Pane-TUI + agent-push on DEAD (full target)

Builds on Option 2. When a port goes DEAD, the agent calls `herdr plugin pane
open` (proven available, Finding 2) to raise the pane automatically. The operator
is notified without polling.

**Cost**: Slice N depends on OQ-3 resolution. If the ControlMaster stales after
laptop sleep and `ssh -O check` lies, push notifications are unreliable. Until
OQ-3 is measured, this slice carries an unknown failure mode.

**Benefit**: UX ceiling. DEAD ports surface without any operator action. This is
what "expect to easily toggle and know the status" reads as the full target.

**Recommendation**: ship Option 2 (Slices 1, W, 3, P, C, 4, 5, L). Add Option 3
(Slice N) after OQ-3 is measured with two outcomes on this hardware pair. Option
3 is an enhancement on top of Option 2, not a replacement — the pull path remains
correct regardless of OQ-3.

---

## E. Slice breakdown

Slices are ordered by dependency. Each is independently shippable unless noted.

### Slice 1 — Protocol: Op + Sandbox to Request; cursor fix

Files: `internal/cli/cmd_herdr_plugin.go` (Request struct, serialization).

Changes: `Request` gains `Op string` and `Sandbox string`. Agent tick gains one
branch: `Op == "revoke"` → call `CancelApplied` (first wiring of dead code),
remove from applied set. Absent `Op` → treat as `"forward"`. **Also pull the
cursor-advance fix from the former Slice 5**: when `IsOurs` is false, do NOT
advance the cursor. This is the single most critical correctness fix in the
design and must not be delayed.

Unit tests: revoke branch; backward-compatible read of old entries; cursor NOT
advanced on host mismatch. No live machines. No OQ dependency.

### Slice W — Wire remaining dead code

Files: `internal/cli/cmd_herdr_plugin.go`, `manager.go`.

Changes: wire `Manager.Reconcile` to the agent's ticker. Wire
`Manager.TeardownSandbox` to the `worktree.removed` hook path. Wire `DiscoverAll`
inside `Reconcile`. Wire `Cancel` to forward-apply failure. The stated principle:
wire what exists before adding new code.

Unit tests: verify Reconcile is called on ticker; TeardownSandbox called on
sandbox stop; DiscoverAll called by Reconcile. A live two-machine test verifies
end-to-end reconcile removes stale forwards.

Depends on Slice 1.

### Slice 3 — `forwards.state` writeback

Files: `internal/cli/cmd_herdr_plugin.go` (agent tick, new `writeForwardState`
helper).

Changes: after every tick that changes port state (apply, revoke, Present check,
host mismatch, TTL expiry), the agent atomically writes `forwards.state` on the
engine via `ExecArgv`. Includes the `Present` check after apply (kills F-10
from portfwd-ux.md).

**Requires live two-machine test**: verify file appears on the engine; contents
reflect actual port state (not just what was requested); atomic write does not
EBUSY.

Depends on Slice 1.

### Slice P — `ports-pane` TUI binary

Files: new `internal/cli/cmd_ports_pane.go`, registered verb `ports-pane`.
`herdr-plugin.toml`: add `[[panes]]` entry and `ports-open` action.

Changes: implement TUI loop — stdin reading, ANSI rendering, key dispatch (§B-3),
`forwards.state` polling every second. Renders Screens 1–9 from §B-2. On Enter:
appends correct `Request` to `requests.json`. On `r`: renders Screen 8 and calls
the space-convert recreate path (coordinates with Slice C). Also fix
`Notifier.Notify` to assert `type == "plugin_pane_opened"` in addition to exit
code check.

**Requires live two-machine test**: verify pane opens in the engine TUI; verify
keypress → Request → agent tick → `forwards.state` update → pane re-render cycle.

Depends on Slices 1 and 3.

### Slice C — Pre-published ports at space-convert

Files: `internal/cli/cmd_space_convert.go` (or equivalent verb), new
`internal/cli/ports_checklist.go`.

Changes: during `space-convert`, before sandbox creation, run the checklist TUI
(§C-2). Auto-detect ports from repo, fall back to defaults, read `.herdr-ports`
if present, pre-check from port-history. Write confirmed ports to sandbox config
via `WithPorts`. Screen 8 in Slice P delegates recreation to this path.

**Requires live test**: verify selected ports appear as published on the created
sandbox; verify the not-pre-published recreation path warns and confirms.

Does not depend on Slice 3 or Slice P for the create-time path. The Screen 8
integration depends on Slice P.

### Slice 4 — Rewrite `status` verb

Files: `internal/cli/cmd_herdr_plugin.go` (`runStatus`).

Changes: read `forwards.state`, render per-port rows matching Screens 1/2/3/6/7/9
render modes (absent file, stale file, each status value), print queue counters
separately with an explicit "not reachability" label.

Unit tests: golden-file rendering for each state using fixture files. No live
machines needed.

Depends on Slice 3 (can be developed against a fixture file before Slice 3 goes
live).

### Slice 5 — Failure visibility: host mismatch and TTL

Files: `internal/cli/cmd_herdr_plugin.go` (Prune, host-mismatch path).

Note: the cursor-advance fix has already been pulled into Slice 1. This slice
covers surfacing via `forwards.state`:

- `IsOurs` false → write `{status: "error", error: "host mismatch: declared for
  \"X\", agent is \"Y\""}` entry to `forwards.state`; log to stderr.
- TTL expiry in `Prune` → write `{status: "expired", error: "..."}` entry.
- `-host` default at `declare` time → `os.Hostname()` when flag absent.

Unit tests: verify error entry appears in state struct on mismatch; verify
`os.Hostname()` default; verify Prune writes expired entry.

Depends on Slice 3.

### Slice L — launchd service

Files: new `internal/cli/cmd_install_launchd.go`, embedded plist template.
`herdr-plugin.toml`: add `install-launchd` and `uninstall-launchd` actions
(§B-5).

Changes: `install-launchd` reads target from local config, generates plist, calls
`launchctl load -w`. `uninstall-launchd` calls `launchctl unload` and removes
plist.

No live-machine dependency beyond a smoke test on the operator's Mac. Runs
standalone; no dependency on other slices. Should ship before Slice P is
end-to-end tested, because the pane-TUI requires the agent to be running.

### Slice N — Agent-push on DEAD transitions (OQ-3 gated)

Files: `internal/cli/cmd_herdr_plugin.go` (agent tick, DEAD transition handler).

Changes: when any port transitions to `dead`, call `herdr plugin pane open` via
`ExecArgv` over ControlMaster to raise the engine-side pane.

**Gated on OQ-3**: measure whether a post-sleep ControlMaster reliably answers
`ssh -O check` with 0 before shipping this. Require two outcomes (sleep/wake with
and without `ServerAliveInterval`). A single pass cannot distinguish a working
keepalive from one that passes vacuously.

**Requires live two-machine test**: verify pane raises on DEAD transition; verify
no spurious raises when master is healthy.

Depends on Slice P.

### Slice 7 — Port-hint from link-handler (RUN B gated)

Files: new `internal/cli/cmd_port_hint.go` or `cmd_open_ports_pane.go`.
`herdr-plugin.toml`: add `[[link_handlers]]` entry.

**Do not write this slice until RUN B's outcome is known.** Then:

- Branch A (URL delivered): implement §B-4 Branch A — parse URL, write hint file,
  open pane. The `ports-pane` TUI polls hint file and pre-selects row.
- Branch B (URL not delivered): implement §B-4 Branch B — open pane only, no
  pre-selection.

Also measure whether `{url}` argv substitution exists; require two outcomes of
that measurement.

Depends on Slice P.

### Dependency graph

```
Slice 1 (Op + Sandbox + cursor fix)
  ├── Slice W  (wire dead code)
  ├── Slice 3  (forwards.state writeback)         ← LIVE TEST required
  │     ├── Slice P  (ports-pane TUI)             ← LIVE TEST required
  │     │     ├── Slice C  (space-convert)        ← LIVE TEST required
  │     │     ├── Slice N  (push on DEAD)         ← LIVE TEST + OQ-3 gated
  │     │     └── Slice 7  (port-hint)            ← RUN B gated
  │     ├── Slice 4  (status verb rewrite)
  │     └── Slice 5  (failure visibility)
  └── Slice L  (launchd service)
```

Slices 1, W, 4, 5, L have no live-machine dependency and can proceed in parallel.
Slices 3, P, C, N require live two-machine tests. Slice N adds an OQ-3 gate on
top. Slice 7 adds a RUN B gate on top of Slice P.

---

## F. Open questions remaining

**OQ-3 — post-sleep ControlMaster reliability (STILL OPEN):**

Does `ssh -O check` return 0 after laptop sleep and wake? Affects Slice N
(push-on-DEAD reliability). Measure: open a master with a live forward, sleep the
laptop, wake it, record `ssh -O check` exit code, `Present(port)`, and an actual
HTTP fetch — all three together. Repeat with `ServerAliveInterval` set and require
a **different** outcome. A single run cannot distinguish a working keepalive from
one that succeeds vacuously.

**RUN B — clicked URL in a real herdr session (NOT YET RUN):**

RUN A (negative control) showed `HERDR_PLUGIN_CLICKED_URL` absent on direct
action invoke. RUN B requires a ctrl+click on a URL in a live herdr TUI session.
Measure: observe whether `HERDR_PLUGIN_CLICKED_URL` is set in the action
process's environment; separately observe whether `{url}` argv substitution
appears. Require both outcomes: URL delivered vs. not delivered. OQ-1 from
portfwd-ux.md is subsumed by this measurement — they are the same probe. Governs
Slice 7.

**OQ-2 is CLOSED** (Finding 2, §Basis): the laptop agent can raise a pane on the
engine via ControlMaster. The "single biggest open question" from the prior draft
is resolved.

**OQ-3 (formerly "single biggest open question" of this doc):** now governs only
Slice N (push-on-DEAD). The Option 2 baseline (Slice P, pull-based) is
OQ-3-independent.
