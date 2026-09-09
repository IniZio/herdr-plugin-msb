# Port-forward toggle and status UX — design

Design only. No production code changes. Extends `portfwd-ux.md` (committed
590a64d); read it first. This document covers two specific operator problems
named in the verdict: toggling a forward on and off, and knowing what is
actually forwarded. Friction points F-n and open questions OQ-n refer to
identifiers in that prior doc.

Where this document departs from `portfwd-ux.md`, the departure is marked
**DEPARTURE** and the reason given.

---

## A. Protocol change

### Why the current schema cannot express what we need

`Request` today (`:38-49`) has three useful fields: `Host`, `RemotePort`,
`LocalPort`. Every append is implicitly "forward this port". Two things cannot
be expressed:

1. "Stop forwarding port 3000" — the queue is append-only and has no operation
   field.
2. "What is actually live right now" — the laptop keeps `applied` in memory
   (`:379-381`); nothing writes it back. `status` reads a queue counter, not
   state.

Both problems share one root: the protocol carries intentions, not state.

### Change 1 — add `Op` and `Sandbox` to `Request`

**DEPARTURE from portfwd-ux.md §3.4**: that section proposes the same two
fields in Phase 4 (last). This doc moves them to the first slice. Reason: they
are the minimum change that makes revoke expressible at all, and revoke is the
operator's stated unmet need. Everything else in this doc depends on them.

New `Request` JSON shape (fields added to the existing struct; backward-compatible
if `op` is treated as `"forward"` when absent):

```json
{
  "id": 12,
  "host": "engine-03",
  "remote_port": 3000,
  "local_port":  3000,
  "op":          "forward",
  "sandbox":     "w8-abc123",
  "ts":          "2026-09-09T10:00:00Z"
}
```

`op` is `"forward"` (today's only behaviour) or `"revoke"`. When `op` is
absent in an existing file entry, the agent treats it as `"forward"` — no
migration needed. This is backward-compatible at the file level; a new agent
reading an old file is safe. An old agent reading a new file will also be safe
because the old agent ignores unknown fields and the ack cursor still advances
correctly. If a revoke entry appears in a file being read by an old agent, the
old agent will attempt to forward port 3000 again (it has no `op` branch); that
is wrong but recoverable — the new agent can supersede.

`sandbox` carries the sandbox ID (as returned by `Runtime.List`). A zero value
(`""`) means "host-level, not sandbox-scoped" — this preserves compatibility
with existing requests that have no sandbox context.

### Change 2 — `forwards.state` writeback file

**DEPARTURE from portfwd-ux.md §3.5**: that section defers the writeback
design to "after OQ-3 resolves" (can the laptop reach the operator's screen
directly). This doc makes the writeback unconditional. Reason: OQ-3 determines
whether the laptop can push a pane *directly*, but the operator can always pull
truth by running `status` on the engine — and `status` is what they are asking
about. The pull path (operator types `status`, engine reads the writeback file)
is independent of OQ-3 and cheaper to ship.

The laptop agent writes `~/.local/state/herdr-plugin-msb/forwards.state` on
the engine via the existing SSH master, using `ExecArgv` (`:104-106`) — the
same transport `RemoteMarkCommand` uses. Write is atomic: write to a temp file,
rename over the target. This avoids EBUSY issues documented in project memory
(Claude Code writes credentials atomically).

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
    }
  ]
}
```

`status` values:

- `live` — `Forwarder.Present(port)` returned true on the laptop.
- `pending` — request acked but `Present` not yet confirmed (brief transient).
- `dead` — previously live, `Present` now returns false.
- `error` — apply failed or `Present` returned false immediately after apply.

The agent rewrites this file on every tick that changes any port's state. The
engine-side `status` verb reads it and renders it. If the file does not exist
(agent not yet connected, or never ran), `status` says so explicitly.

No new transport is needed: `ExecArgv` already runs arbitrary commands over the
existing master (`:104-106`). The write is one `cat > <tmpfile> && mv` command.

### Is this a breaking change?

No. Old request files without `op` are still valid (agent defaults to
`"forward"`). The `forwards.state` file is additive — nothing today reads it,
so adding it changes no existing behaviour. Old agents that do not know how to
write it leave it absent; `status` handles absence with an explicit message.

---

## B. Operator surface

### The toggle

The operator needs two gestures:

1. **Forward a port** — already reachable via `declare` and the
   `ports-declare` action. Phase 1a in portfwd-ux.md fixes the link handler to
   actually work (OQ-1 gated). This doc adds nothing new to the forward path.

2. **Stop forwarding a specific port** — currently impossible without killing
   the master and dropping every forward (F-15). The new verb is `unforward`.

#### New verb: `unforward`

```
herdr-plugin-msb unforward -port 3000
```

`runUnforward` appends a `Request{Op: "revoke", RemotePort: 3000}` to
`requests.json`, defaulting `Host` from `os.Hostname()`. The agent, on next
tick, matches the port in its `applied` map, runs `CancelArgv` (`:112-114`),
calls `Present` to confirm the port is gone, and removes the entry from its
state. It then rewrites `forwards.state`.

An `unforward` for a port not in `applied` is reported to the operator (via the
pane or the status pane, whichever exists) as "port 3000 was not forwarded by
this agent".

#### Action menu entry

Add to `herdr-plugin.toml`:

```toml
[[actions]]
id        = "ports-unforward"
name      = "Stop port forward"
command   = ["herdr-plugin-msb", "unforward", "-port", ""]
```

> **RETRACTED — this mechanism does not exist.** The claim that the operator "is
> prompted for the port number (herdr's standard action argument prompt)" was never
> verified and is false. `herdr plugin action invoke` accepts only `--plugin` and a
> positional `<ACTION_ID>`; the `[[actions]]` schema carries only `id`, `title`,
> `command`, `contexts`, `platforms` — no parameter, prompt, or substitution field.
> An action fires its fixed `command` array verbatim and solicits no operator input.
> Actions are also strictly static: there is no runtime registration, so a per-port
> "Unforward 3000" menu entry is not expressible either.
>
> The operator has additionally ruled out typed CLI gestures entirely. Both the verb
> above and this menu entry are therefore superseded — see the revised design for the
> pane-TUI approach, which is the only surface that carries a per-port toggle without
> a herdr capability that does not exist.

#### Link-handler toggle (OQ-1 gated)

Once OQ-1 confirms `HERDR_PLUGIN_CLICKED_URL`, the link handler can be
extended: if the clicked port appears in `forwards.state` as `live`, the action
calls `unforward`; otherwise it calls `declare`. This turns the URL click into
a true toggle. The action reads `forwards.state` (a local file read, no remote
call needed) to decide which branch to take.

This is additive over Phase 1a of portfwd-ux.md; do not implement the toggle
path until the basic declare path (Phase 1a) is confirmed working.

### Status display

#### Engine-side `status` verb

`runStatus` currently prints `pending=N acked=M` (`:540-554`). Rewrite it to:

1. Read `forwards.state`. If absent, print the honest message and exit.
2. Print one row per forward.
3. Print queue bookkeeping beneath, clearly separated and labelled as "queue
   counters, not reachability".

Literal example output when `forwards.state` exists and is fresh:

```
port-forward status  (as of 10:05:23, from newman@mba)

  3000  LIVE    sandbox w8-abc123  since 10:00:30
  8080  DEAD    sandbox w8-abc123  lost 09:58:41  error: master keepalive expired

queue counters (not reachability):  pending=0  acked=12
```

When `forwards.state` does not exist:

```
port-forward status

  no forwards.state found — laptop agent has not connected or has not polled yet
  run:  herdr-plugin-msb local-agent --target newman@engine-03

queue counters (not reachability):  pending=1  acked=11
```

When `forwards.state` is stale (written_at more than 30 s ago):

```
port-forward status  (as of 10:05:23, from newman@mba  — STALE, 4m ago)

  3000  LIVE    sandbox w8-abc123  since 10:00:30
  ...
```

The staleness threshold is a constant in the status renderer. A stale file most
likely means the laptop agent died or the master dropped.

#### Persistent status pane (action menu)

Add a `ports-status` action that calls `herdr-plugin-msb status` and opens
the output in a pane. The operator can rerun it any time.

**DEPARTURE from portfwd-ux.md §3.5**: that section suggests the agent pushes
pane updates in both directions (UP/DOWN transitions). This is correct as the
eventual target, but it depends on OQ-3 resolving favourably. In the meantime,
a pull-based model (operator triggers `ports-status`, engine reads the file the
laptop wrote) is independently useful and has no open dependencies.

The push model (agent-initiated pane on transition) is additive later once
OQ-3 is answered. Do not block the pull model on it.

### Handling the known failure modes

#### F-5 / Known Trap 9: IsOurs silent-drop

Two changes, both required:

**At declare time**: default `-host` from `os.Hostname()` when the flag is
absent. This means a host mismatch stops occurring on the happy path — the
operator never has to type their hostname.

**At agent time**: when `r.Host != "" && r.Host != a.HostName`, do NOT advance
the cursor. Instead:

- Log `[warn] request id=%d port=%d was declared for host %q but this agent is
  %q — request left pending` to stderr.
- Rewrite `forwards.state` with an entry `{port: N, status: "error", error:
  "host mismatch: declared for <theirs>, agent is <ours>"}`.

This surfaces in `status` output immediately. The operator sees the mismatch
and can re-declare with the correct host.

**Do not silently consume.** The cursor advance is the silent drop; it must be
removed. A pending request the operator can see is recoverable; a permanently
consumed one is not.

#### F-6 / Known Trap 10: TTL silent expiry

When `Prune` discards a request (`:187-200`) or the agent's cutoff
(`:363, :372`) passes over an expired one, write an entry to `forwards.state`:

```json
{
  "port":   3000,
  "status": "error",
  "error":  "request expired after TTL (10m) without being acked — agent may
             not be running or has a host mismatch"
}
```

This surfaces in `status` output. The operator knows the declaration was lost,
not merely pending.

Also: consider whether 10 minutes is the right TTL for an interactive workflow.
A developer who restarts their laptop agent after an interruption should not
silently lose their declarations. The TTL serves as a prune guard, not a
safety feature; a 60-minute TTL with a logged expiry is less hostile. This doc
does not mandate a value — it mandates that expiry is never silent.

---

## C. Trade-offs and recommendation

Three meaningfully distinct surfaces:

### Option 1 — Action menu + link handler, no persistent pane

Toggle is via action menu entries (`ports-declare`, `ports-unforward`). Status
is on-demand via `ports-status` action (opens a pane, closes when done). No
persistent pane.

**Cost**: status requires the operator to trigger it each time. A dead forward
surfaces only when the operator next checks. The operator cannot glance and know
the current state. Satisfies the verdict only partially: they can now toggle and
check status, but status is not "at a glance".

**Benefit**: minimal new surface, OQ-1 not required for `unforward`. Fastest
to ship.

### Option 2 — Persistent status pane + explicit `unforward` verb (recommended)

A `ports-status` action opens a persistent side pane that the operator can
leave open. The engine-side `status` verb reads `forwards.state` (written by
the laptop agent every tick). `unforward -port N` is a direct CLI verb, also
reachable from the action menu with a port prompt.

**Cost**: requires implementing the `forwards.state` writeback (Slice 3 below).
Requires the laptop agent to be running for status to be live. If the agent is
not running, the pane clearly says so (not silently wrong).

**Benefit**: status is readable at a glance from the pane, no trigger needed.
Toggle is explicit and typed (accidental unforward is ruled out). Does not
depend on OQ-1 or OQ-3 — the laptop writes a file, the engine reads it, the
pane renders it. This is the minimum loop that is honestly correct.

**Disqualifiers for the other options**: Option 1 fails "at a glance" because
it is on-demand. Option 3 (below) relies on OQ-3 (laptop pushing pane
notifications directly), which is unverified.

### Option 3 — Agent-pushed live pane (UP/DOWN transitions)

The laptop agent pushes a pane update on every state transition — port goes
live, port dies, port retries. The pane updates in real time without any
operator trigger.

**Cost**: blocked on OQ-3. If `herdr plugin pane open` from the laptop under
`--remote` cannot reach the engine's TUI, this entire option is dead code. OQ-3
is currently unverified (portfwd-ux.md §5). Shipping Option 3 before OQ-3 is
measured is building on an assumption the project has already been burned by
once.

**Benefit**: the UX ceiling — the operator sees a live state without touching
anything.

**Recommendation**: ship Option 2. It is honestly correct, has no open
dependencies, and satisfies the stated need ("toggle and know the status"). Add
Option 3's push behaviour later, on top of Option 2, once OQ-3 is answered.
Option 3 is not a replacement for Option 2's pull path — it is an enhancement.

---

## D. Slice breakdown

Slices are ordered by dependency. Each slice is independently shippable.

### Slice 1 — Protocol: add `Op` and `Sandbox` to `Request`

Files: `internal/cli/cmd_herdr_plugin.go` (Request struct, serialization),
tests.

What changes: `Request` gains `Op string` and `Sandbox string`. Agent tick
gains one branch: on `Op == "revoke"`, call `CancelApplied` for the port and
remove from the applied set. On absent `Op`, treat as `"forward"` (backward
compatibility). Existing unit tests must still pass; add a revoke-branch test.

No live machines needed. No OQ dependency.

### Slice 2 — New verb: `unforward`

Files: `internal/cli/cmd_herdr_plugin.go` (add `runUnforward`, register verb),
`run.go` (add `unforward` to verb list), `herdr-plugin.toml` (add
`ports-unforward` action).

What changes: `unforward -port N` appends `Request{Op: "revoke", RemotePort:
N}`. Action menu entry calls it with a port prompt. Unit test: verify the
correct Request is appended.

Depends on Slice 1. No live machines needed.

### Slice 3 — `forwards.state` writeback

Files: `internal/cli/cmd_herdr_plugin.go` (agent tick, new `writeForwardState`
helper).

What changes: after every tick that changes port state (apply, cancel, Present
check), the agent writes `forwards.state` over the SSH master via `ExecArgv`.
Write is atomic (tmp+rename on the engine). Includes the `present` check after
apply (kills F-10 from portfwd-ux.md).

**Requires a live two-machine test**: verify the file appears on the engine,
verify its contents reflect actual port state (not just what was requested).
Verify atomic write does not EBUSY.

Depends on Slice 1.

### Slice 4 — Rewrite `status` verb

Files: `internal/cli/cmd_herdr_plugin.go` (`runStatus`).

What changes: read `forwards.state`, render per-port rows (LIVE/DEAD/PENDING/
error), print queue counters separately, handle absent/stale file with clear
messages. Unit test: golden-file rendering tests for each state.

Depends on Slice 3 (to have a file to read). Can be built with a fixture file
before Slice 3 is tested live.

### Slice 5 — Failure visibility (IsOurs trap, TTL trap)

Files: `internal/cli/cmd_herdr_plugin.go` (agent tick IsOurs branch, Prune).

What changes:
- `IsOurs` false → do NOT advance cursor; write an error entry to
  `forwards.state`; log to stderr.
- TTL expiry → write an error entry to `forwards.state`.
- `Host` default at `declare` time → `os.Hostname()` when flag absent.

Unit tests: verify cursor is NOT advanced on host mismatch; verify an error
entry appears in the state struct; verify `os.Hostname()` default.

Depends on Slice 3 (to have the state struct to write into). The cursor-fix
half is independent of Slice 3 and should be done in Slice 1 if possible —
the silent drop is the most dangerous behaviour in the current codebase.

**Flag**: the cursor-advance fix is the most critical correctness change in
this whole design. It is small (one condition removed) and should not be
delayed for Slice 5 scheduling reasons. Pull it into Slice 1 or Slice 2.

### Slice 6 — Status pane in action menu

Files: `herdr-plugin.toml`.

What changes: add `ports-status` action that calls `herdr-plugin-msb status`
and opens the output in a pane (using `-notify` or equivalent pane mechanism).

Depends on Slice 4. No live machines needed beyond verifying pane opens.

### Slice 7 — Link-handler toggle (OQ-1 gated)

Files: `internal/cli/cmd_herdr_plugin.go` (`runDeclare`, or a new shared
handler).

What changes: if `HERDR_PLUGIN_CLICKED_URL` is set and the port is already in
`forwards.state` as `live`, call `unforward` instead of `declare`.

**Gated on OQ-1** from portfwd-ux.md — measure that herdr 0.8.0 sets the
variable before writing any of this. Depends on Slices 2, 4, and OQ-1.

### Dependency graph

```
Slice 1 (Op + Sandbox, cursor fix)
  └── Slice 2 (unforward verb)
  └── Slice 3 (forwards.state writeback)  ← LIVE TEST required
        └── Slice 4 (status rewrite)
              └── Slice 6 (status pane)
        └── Slice 5 (failure visibility)
  └── Slice 7 (link-handler toggle)       ← OQ-1 gated
```

Slices 2 and 3 can proceed in parallel after Slice 1. Slice 6 is the last
user-visible piece and has the full dependency chain beneath it.

---

## Single biggest open question

**OQ-3 from portfwd-ux.md: can `herdr plugin pane open` reach the engine's
TUI when run on the laptop under `--remote`?**

This doc's recommendation (Option 2, pull-based) does not block on OQ-3 —
`status` works regardless. But OQ-3 determines whether the agent can ever push
a "port 3000 is now DEAD" notification without the operator asking. That push
path (Option 3) is the difference between "status you can check" and "status
that tells you without asking". The operator's verdict ("expect to easily
toggle and know the status") could be read as wanting the push path. Resolve
OQ-3 before deciding whether to build Slice 3's writeback as the final answer
or as a stepping stone toward agent-push notifications.

Measure: during a live `--remote` session, run `herdr plugin pane open --text
"test"` from the laptop. Observe whether a pane appears in the engine-side TUI.
Require both outcomes (appears / does not appear) with and without the flag.
