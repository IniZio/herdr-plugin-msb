# Port-forward UX — design

Design only. No implementation code is proposed as written; every mechanism
below carries a PROVEN / UNVERIFIED mark and a citation. Nothing here has been
built or run against a live sandbox.

Subject: the operator starts a dev server inside a sandboxed worktree on
`engine-03` and wants it in the browser on their laptop, on the same port
number.

Marks used throughout:

- **PROVEN** — measured on this host, with a citation to the file, line, or doc
  section that records the measurement.
- **UNVERIFIED** — read from source, from `strings` on a binary, or inferred.
  Not measured. Must not be built on without a measurement first.

---

## 1. The current flow, step by step

Written as the operator actually experiences it. Friction points are named
`F-n` and carried into the phase plan.

### Step 0 — before anything (on the engine)

The sandbox must have been created with the port already published.

- `Runtime.Publish` **removes and re-creates** the sandbox to add a port
  (`internal/runtime/msb/publish.go:26-34`). PROVEN: microsandbox v0.6.17 has
  no post-hoc publish surface — `msb port` / `msb expose` / `msb publish` all
  exit 2, `msbsdk.ModifyOptions` has no ports field, and no FFI symbol contains
  "port", "expose", or "publish" (`doc/port-publish.md`, "Verified: microsandbox
  v0.6.17 has no post-hoc publish surface").

**F-1 — publish is destructive.** Adding a port to a running sandbox destroys
all in-guest state. If the operator did not think of port 3000 at create time,
the only supported route costs them the sandbox.

**Important scoping consequence, and it is good news:** the SSH forward does
**not** need a published port. `ssh -O forward -L P:127.0.0.1:P` binds on the
*engine's* loopback and reaches the guest over the sandbox's own path, and the
mechanism that reads guest listeners is `msb exec netstat` inside the guest, not
a host socket table. The forward path and the publish path are independent. The
design below therefore never triggers a recreate. F-1 is real but out of scope
for this document beyond "do not put a recreate on the happy path".

### Step 1 — the dev server starts inside the guest

Nothing happens. No component watches for it.

**F-2 — no detection.** `portfwd.Discoverer` exists and works
(`internal/core/portfwd/discovery.go:54-91`, `msb exec netstat -ltn` inside the
guest), but **it has no production caller**. PROVEN by grep: the only
references to `DiscoverOne`, `DiscoverAll`, `NewManager`, `Reconcile`, and
`TeardownSandbox` outside `_test.go` files are their own definitions. The whole
of `internal/core/portfwd/manager.go` and the discovery entry points are dead
code today.

### Step 2 — the operator declares the port, by hand, on the engine

```
herdr-plugin-msb declare -port 3000 -host engine-03 -notify
```

This appends a `Request` to `requests.json` under the state dir
(`internal/cli/cmd_herdr_plugin.go:481-538`, `Declarer.Declare` at `:211-249`).

**F-3 — the operator must retype a port the machine already printed.** The dev
server wrote `http://127.0.0.1:3000` to the pane one second earlier.

**F-4 — the operator must know and type their own hostname.** `-host` is a free
string. It is not defaulted from `os.Hostname()`.

**F-5 — `-host` is a silent-drop trap, not just a nuisance.** The agent's
filter is `IsOurs(r) = r.Host == "" || r.Host == a.HostName`
(`:309-311`). If the operator declares `-host engine-03` and starts the agent
without `--host` (`HostName` is then `""`), `IsOurs` is false — and the request
is not merely skipped, the loop advances `cursor = r.ID` (`:372-374`) and the
cursor is written back as the remote ack (`:394-397`). The request is
**consumed and never applied**, permanently. Nothing tells anyone. This is the
single most dangerous behaviour in the current flow.

**F-6 — the queue silently expires.** `Prune` (`:187-200`) and the agent's own
cutoff (`:363`, `:372`) discard any request older than `DefaultTTL = 10 minutes`
(`:31`). Declare a port, get distracted, start the agent fifteen minutes later:
the request is gone, with no message.

### Step 3 — the operator walks to the other machine and starts the agent

```
herdr-plugin-msb local-agent --target newman@engine-03 \
  --control-path /run/user/$(id -u)/msb-portfwd.sock --poll 5s
```

**F-7 — a second long-lived command on a second machine is the whole cost.**
This is the dominant friction. Everything else is a paper cut by comparison.

**F-8 — two of the three flags in the documented invocation are unnecessary.**
`--control-path` already defaults to `ControlPathFor(StateDir, host)`
(`:586-593`, `:81-83`) and `--poll` already defaults to 5s (`:598-603`). The
runbook teaches four flags for a command that needs one. Note also that
`/run/user/<uid>` does not exist on macOS, and the laptop in the MSP-R-010
evidence is macOS — so the documented control path is wrong for the machine it
is documented for. The code's default (`$HOME/.local/state/herdr-plugin-msb/
<host>.ctl`, ~55 bytes) is correct on both platforms and stays under the
103-byte `sun_path` cap enforced at `:85-90`.

**F-9 — there is no laptop-side binary.** `make build` builds for the host only
(`Makefile:29-30`); there is no `GOOS=darwin` target anywhere in the Makefile,
and `herdr-plugin.toml` declares `platforms = ["linux"]`. How the operator's
macOS laptop obtains a runnable `herdr-plugin-msb` is undocumented. UNVERIFIED
whether the tree even compiles for darwin (`terminal_other.go` exists as a
non-linux stub, which is encouraging but not a measurement).

### Step 4 — the forward is applied

`Agent.Tick` reads the remote queue over the master, runs `ssh -O forward`, and
marks the ack (`:336-399`).

**F-10 — apply is never verified.** `Tick` treats exit 0 from
`ssh -O forward` as success and records `applied[r.ID]`. It never checks that a
local listener appeared. `portfwd.Forwarder.Present` (`forward.go:109-127`) is
exactly that far-side check — `ss -ltn` with a macOS `netstat -an -p tcp`
fallback and correct prefix/suffix port guards — and PROVEN as the authoritative
signal (`doc/portfwd-teardown.md`, "Why `ss -ltn` is the per-forward presence
signal": `ssh -O check` is master-level only, and `ssh -O cancel` on a
never-applied port **exits 0**, so neither exit code can be used). `Present` is
in the dead half of the codebase and the live agent does not call it.

**F-11 — two divergent master-opening implementations.** `MasterArgv`
(`cmd_herdr_plugin.go:92-102`) sets `ControlPersist=yes` with no
`ConnectTimeout`, no `ExitOnForwardFailure`, and no `ServerAliveInterval`.
`Forwarder.EnsureMaster` (`forward.go:53-79`) sets `ControlPersist=60`,
`ConnectTimeout=10`, `ExitOnForwardFailure=yes`, `StrictHostKeyChecking=no`.
They will behave differently under exactly the conditions that matter.

### Step 5 — the operator finds out whether it worked

```
herdr-plugin-msb status     # → pending=0 acked=7
```

**F-12 — `status` is a queue counter, not a forward status.** It runs on the
engine and reads only `requests.json` and the ack file (`:540-554`). `acked=7`
means "the agent's cursor reached 7". Under F-5 it also means "seven requests
were thrown away". There is no field in this output that is derived from the
laptop, and therefore nothing in it can tell the operator whether a browser tab
will load. This is the repo's own documented failure mode restated: an
engine-side signal that proves nothing (`doc/portfwd-teardown.md`; project
memory `verify-at-the-endpoint-that-exercises-the-mechanism`).

**F-13 — nothing announces a forward.** The only operator-visible channel is
`herdr plugin pane open` (`:258-266`), fired only by `declare -notify`, only for
ports the operator typed themselves, and only at declare time — i.e. **before**
the forward exists. The pane says "here is a URL I intend to forward", which the
operator will read as "it is up".

**F-14 — nothing announces a dead forward.** After laptop sleep the master's TCP
connection is gone but the control socket can linger; `ssh -O check` may still
exit 0 against a master that forwards nothing. No `ServerAliveInterval` is set
on the agent's master (F-11), so nothing prunes it, and no periodic `Present`
check runs (F-10). The tab stops loading and the operator debugs their
application.

### Step 6 — teardown

**F-15 — there is no revoke.** No verb removes one request. The queue is
append-only; entries leave only by ack or by TTL. To stop forwarding port 3000
the operator kills the master (`ssh -S <sock> -O exit`), which drops **every**
forward on that host.

**F-16 — forwards outlive their agent.** `CancelApplied` runs only from
`Serve`'s deferred call (`:404-426`). The master runs `ControlPersist=yes`, so a
SIGKILLed agent leaks every forward it applied — the comment at `:401-403` says
so. The port stays bound on the laptop, and MSP-R-006 names precisely this as
worse than no forward at all.

**F-17 — sandbox death is invisible to the laptop.** `Manager.TeardownSandbox`
and `Manager.Reconcile` implement sandbox-scoped teardown
(`manager.go:23-72`) — and are dead code (F-2). The live `Request` type carries
`Host`, `RemotePort`, `LocalPort` (`:38-49`) and **no sandbox identity**, so the
laptop half cannot express "this sandbox is gone" even if something told it.

### The click path that looks like it already works

`herdr-plugin.toml` declares `[[link_handlers]] pattern = 'http://127\.0\.0\.1:\d+'`
→ action `ports-declare` → `["herdr-plugin-msb", "declare", "--notify"]`.

**F-18 — the shipped link handler is a no-op.** The action passes no `-port`.
`runDeclare` with an empty `-port` takes the branch at `:495-504`: it prints the
current queue JSON and returns 0. It declares nothing. Action stdout goes only
to `herdr plugin log list`, so the operator sees a success and no pane. MSP-R-010
records this action as verified with `exit_code 0` — which it is, and which
proves nothing about the port being declared. This is a fourth instance of the
repo's "check written to pass" pattern.

---

## 2. The target flow, in the operator's words

> I start `npm run dev` in a guest pane. A moment later a small pane says
> **"port 3000 is live on your laptop — http://127.0.0.1:3000"**. I Cmd-click it
> and the page loads. When I stop the dev server, or the sandbox goes away, the
> pane tells me the forward is gone. If it ever breaks while I am using it, the
> pane tells me that too, and tells me it is retrying.
>
> I never typed a port, a hostname, or a socket path. I never started a second
> command. If I want it gone early I run `msb unforward 3000` — or I just stop
> the server.

Three properties define "done":

1. **Zero operator input on the happy path.** No port, no host, no path, no
   flags, no second command.
2. **Every status the operator reads is derived from the laptop.** An
   engine-side statement about reachability is either omitted or labelled as an
   intention, never as a state.
3. **Both transitions are announced.** UP and DOWN. A forward that dies quietly
   is the failure this project has already shipped once.

---

## 3. Mechanism, step by step — proven vs open

### 3.1 Detection inside the guest

**Mechanism:** poll `Discoverer.DiscoverAll` (`discovery.go:74-91`) on the
engine, every N seconds, and feed new listeners to `Declarer.Declare`, which
already takes `[]portfwd.Listener` and dedupes by port (`:211-249`).

- PROVEN: enumeration inside the sandbox's own network context works.
  `msb exec netstat -ltn` reads the guest kernel's sockets
  (`doc/portfwd-discovery.md`, "Why the exec/vsock path works").
- PROVEN, and not to be re-proposed: host-side enumeration is impossible.
  Host `ss -ltnp` and `/proc/net/tcp` show only host sockets — a live guest
  listener on 45455 was invisible; `ip netns list` and `/var/run/netns/` are
  both empty and `nsenter -t <pid> -n ss` returns "Operation not permitted";
  `msb inspect --format json` reported `"ports": []` while that listener was
  live. Three ways, all measured (`doc/portfwd-discovery.md`). Process-ancestry
  attribution is separately falsified (F5, `msp-r-006` "Open dependency").
- PROVEN: identity comes from `Runtime.List`, not from a pid — `ExecResult`
  carries no pid by design (`doc/portfwd-discovery.md`, "Why identity comes from
  our records").
- OPEN: what a poll of `msb exec` costs. Each `DiscoverAll` is one exec per
  running sandbox. Cadence, and whether exec contends with a busy guest, is
  unmeasured. Start at 3–5 s and measure.
- OPEN: noise filtering. `netstat -ltn` in a guest running an agent will show
  more than the dev server. A bind-address and port-range policy is needed;
  `Listener.BindAddr` is already captured (`discovery.go:18-22`) but
  `Declarer.Declare` currently hardcodes `RemoteBind: "127.0.0.1"` (`:238`) and
  ignores it.

### 3.2 The link-handler path — how far it gets us

Further than the current manifest suggests, and it is cheap.

- UNVERIFIED, from `strings` on `/home/newman/.local/bin/herdr` (herdr 0.8.0):
  the binary contains the literals **`HERDR_PLUGIN_CLICKED_URL`** and
  **`HERDR_PLUGIN_LINK_HANDLER_ID`** adjacent to `HERDR_PLUGIN_ACTION_ID`,
  `HERDR_PLUGIN_EVENT`, `HERDR_PLUGIN_EVENT_JSON`, `HERDR_WORKSPACE_ID`; and the
  plugin-context struct field list contains `clicked_url`, `link_handler_id`,
  `invocation_source`, `correlation_id`, `selected_text`, `workspace_cwd`.
  This strongly suggests herdr passes the clicked URL to the action. **It is a
  string in a binary, not a measurement.** Measure it before building on it
  (OQ-1).
- If OQ-1 confirms: `declare` reads `HERDR_PLUGIN_CLICKED_URL`, parses the port
  out of it, defaults `-host` from `os.Hostname()`, and enqueues. That converts
  F-18 from a no-op into a working one-gesture path and kills F-3 and F-4, in
  roughly twenty lines.
- **Ceiling of this path:** a link handler fires on a *click*. It is not
  detection. It gets the operator from "type a port and a hostname" to "click
  the URL your dev server printed" — a large win, and still one gesture short of
  the target. It also does nothing about F-7 (the laptop-side agent), which is
  the dominant cost. Treat it as the cheap first cut, not the answer.
- PROVEN: actions are reachable under `--remote` with no keybinding, and
  `[[link_handlers]]` is one of the two reaching paths (MSP-R-010, verified
  2026-09-08). Carry that node's own confidence caveat: its ABI facts are
  MEDIUM-confidence and second-hand.
- PROVEN: action stdout and stderr are pipes; `herdr plugin pane open` is the
  only operator-visible channel (`:251-253`, MSP-R-010 fit criterion).

### 3.3 The laptop side — self-configuring, and what starts it

Where herdr runs is the crux. Under `herdr --remote newman@engine-03`, the
herdr **server** is on the engine, so `[[actions]]`, `[[events]]`, and
`[[startup]]` all execute on the engine (MSP-R-010: "plugin commands execute
where the herdr **server** runs"). **herdr cannot start anything on the
laptop.** `[[startup]]` exists in the 0.8.0 manifest schema
(`PluginManifestStartup`, `doc/herdr-event-contract.md:50-51`) and is useless
for this purpose.

So the laptop-side agent must be started by the laptop. Two candidates:

**(a) Ride the attach command.** The operator already types
`herdr --remote newman@engine-03 --session default`. That string *is* the
`--target`. A thin laptop-side wrapper (`msb-attach`, a shell function, or a
`herdr` shim) extracts the argument after `--remote` and starts
`herdr-plugin-msb local-agent --target <that> --host <hostname of that>` before
exec'ing the real herdr.

- Discovers `--target` with zero new operator knowledge — it is a substring of
  what they already type.
- Discovers `--control-path` with no operator knowledge: `ControlPathFor`
  already derives it (`:81-83`), and its default is correct on macOS where the
  documented `/run/user/...` path is not (F-8).
- Lifetime is exactly the attach session, which is the right scope: forwards
  should not outlive the session that created them (F-16).
- OPEN: whether a wrapper is acceptable to the operator, versus a real service.

**(b) A launchd user agent (macOS) / systemd user unit (Linux laptop).**
Persistent, survives across attaches, `KeepAlive` restarts it after sleep.

- Needs `--target` from somewhere the wrapper had for free — a config file, or
  the most recent attach recorded by (a).
- OPEN (OQ-5): the operator's laptop is macOS (MSP-R-010 evidence: macOS,
  100.64.0.35). Nothing about launchd has been tested here.

**Recommendation: build (a) first, then (b) on top of it** — (a) needs no new
configuration surface at all, and its output (the last-used target) is exactly
the input (b) is missing.

**Sleep and reconnect** is where the design must be explicit, because F-14 is
the failure this project has already shipped:

- The agent must open its master with `ServerAliveInterval` and
  `ServerAliveCountMax` set, so a master whose connection died during sleep
  **exits** instead of lingering as a socket that answers `ssh -O check` with 0.
  Neither current master-opening path sets these (F-11). UNVERIFIED: the exact
  values, and whether a lingering post-sleep master really does answer
  `-O check` with 0 on this pairing (OQ-4 — this must be measured with both
  outcomes, not assumed).
- On reconnect the agent re-runs `EnsureMaster` and **re-applies from
  `Present`**, not from its in-memory `applied` map — after a master restart the
  map is a record of intentions, not of state.
- Every reconnect that changes a port's state emits a pane (see 3.5).

### 3.4 Revoke and teardown — the smallest honest model

The append-only queue is the wrong shape for a thing with a lifecycle. The
smallest honest change is **one field and one message type**, not a rewrite:

1. Add `Sandbox` (the `runtime.SandboxRef` ID) to `Request` (`:38-49`). Without
   it the laptop half cannot express sandbox-scoped teardown at all (F-17), and
   the sandbox-scoped teardown logic already exists, keyed on sandbox ID
   (`manager.go:62-71`).
2. Add `Op` to `Request`: `forward` (today's only behaviour) or `revoke`. A
   revoke is an ordinary append — the file stays append-only, the ack cursor
   still works unchanged, and the agent gains one branch: on `revoke`, run
   `CancelArgv` (`:112-114`) for the matching spec and drop it from `applied`.
   This buys `unforward 3000` and per-sandbox drop with no new transport.
3. `stop` and `rm` enqueue a revoke for every port of that sandbox before the
   guest goes away. `worktree.removed` already has a hook
   (`herdr-plugin.toml`); PROVEN caveat: it fires only when herdr drives the
   removal, not on a plain `git worktree remove`
   (`doc/herdr-event-contract.md`, A4).
4. Discovery closes the loop for the case nobody signalled: a port that has
   disappeared from `DiscoverAll` for two consecutive polls gets an automatic
   revoke. This is `Manager.Reconcile`'s existing shape (`manager.go:23-59`),
   which already cancels forwards for sandboxes no longer running.
5. Raise or remove the 10-minute TTL for un-acked requests (F-6). A TTL that
   silently deletes work the operator asked for is not a safety feature. If a
   TTL is kept, its expiry must produce a pane.
6. Delete the silent-drop path (F-5): a request for another host must be
   **left pending**, never cursor-advanced. Better: default `-host` from
   `os.Hostname()` at declare time and `--host` from the target at agent start,
   so the mismatch stops occurring.

PROVEN constraints this model respects: `ssh -O cancel` exits 0 whether or not
the forward existed, so cancel's exit code is never the signal — `Present` is
(`doc/portfwd-teardown.md`). And there is no renumbering path: the same-port
contract is non-negotiable (MSP-R-006), so a port collision on the laptop is an
error to report, never a fallback to another number.

### 3.5 Feedback — UP, and the harder case, DEAD

**The rule, from this repo's history:** any status shown to the operator is
computed on the laptop, or it is labelled as an intention. An engine-side curl
once "proved" a dead forward (project memory,
`verify-at-the-endpoint-that-exercises-the-mechanism`).

- **UP** is `Forwarder.Present(port)` returning true **on the laptop**, after
  the apply. PROVEN as the authoritative per-forward signal, with the negative
  case measured — after cancel, `ss -ltn | grep :45456` is empty and curl is
  refused (`doc/portfwd-ssh-mechanism.md`, "Forward presence check";
  `doc/portfwd-teardown.md`). `Present` already falls back to
  `netstat -an -p tcp` with a `.PORT` matcher (`forward.go:118-127`,
  `:157-172`), which is the macOS form — so the laptop check is already written,
  just unused. Only after `Present` is true does the operator see
  "port 3000 is live".
- **DEAD** is the same check, run on a timer, transitioning true→false. The
  agent must keep a per-port last-known state and emit a pane on **every**
  transition in both directions. This is the single most important behaviour in
  the design and the one with no current implementation at all.
- The channel is `herdr plugin pane open` (`:258-266`) — the only
  operator-visible one. UNVERIFIED (OQ-3): the agent runs on the **laptop**,
  where under `--remote` there is no local herdr server socket to open a pane
  against. The laptop agent may have no way to reach the operator's screen at
  all. If it does not, the honest fallback is: the laptop agent writes its
  per-port state (derived on the laptop) back to the engine over the existing
  master, and the engine-side notifier renders it — still laptop-derived, merely
  laptop-derived-and-relayed. `Agent` already runs arbitrary remote commands
  over that master (`ExecArgv`, `:104-106`; `RemoteMarkCommand`, `:65-68`), so
  the transport exists. **This open question decides the shape of the whole
  feedback layer** and must be measured first.
- `status` (`:540-554`) is rewritten to print one row per port with a
  laptop-derived state, or — until OQ-3 is answered — to say plainly that its
  numbers are queue bookkeeping and not reachability. Either is honest.
  `pending=N acked=M` presented as port-forward status is not.

---

## 4. Phased build order

### Phase 1 — the smallest change that removes the most friction

Two items, deliberately kept apart from everything structural. Neither needs a
sandbox recreate, a new daemon, or a schema change.

**1a. Make the link handler actually declare (kills F-18, F-3, F-4).**
`runDeclare` reads `HERDR_PLUGIN_CLICKED_URL` when `-port` is empty, parses the
port, and defaults `-host` to `os.Hostname()`. **Gated on OQ-1** — measure that
herdr 0.8.0 sets that variable before writing the code. Roughly twenty lines.
The operator goes from "know your hostname, retype the port, run a command" to
"click the URL that is already on your screen".

**1b. Make apply prove itself, and make failure loud (kills F-10, halves F-13
and F-12).** After `ssh -O forward` returns 0, the agent calls
`Forwarder.Present(port)` — on the laptop — and only then reports the port as
live. `Present` already exists and is already the proven signal
(`forward.go:109-127`); this is wiring, not new logic. If `Present` is false, do
not ack: report the failure. Add `ServerAliveInterval`/`ServerAliveCountMax` to
`MasterArgv` and converge the two master-opening paths (F-11).

Also in Phase 1, because they are one-line honesty fixes with outsized effect:

- Stop the silent drop (F-5): a foreign-host request stays pending; never
  advance the cursor past a request that was not applied.
- Fix the runbook (F-8): document `local-agent --target <user@host>` and nothing
  else. The other flags already default correctly, and the documented
  `/run/user/...` control path is wrong on the operator's macOS laptop.

**Why this is phase 1:** 1a removes the most keystrokes per unit of work, 1b
removes the failure mode that costs the most *trust* — and the repo has already
been burned by exactly that failure mode once. Neither depends on the laptop
service question (OQ-5), the pane-reachability question (OQ-3), or any SDK
capability. 1b depends on nothing unverified at all.

### Phase 2 — auto-detection (kills F-2)

Wire `Discoverer.DiscoverAll` into a polling loop on the engine that feeds
`Declarer.Declare`. Add bind-address and port-range filtering. Add `Sandbox` to
`Request`. This is the step that removes the last operator gesture on the happy
path. It is deliberately second: it is worthless while the laptop half still has
to be started by hand, and its cost (OQ-2, exec poll cost) is unmeasured.

### Phase 3 — the laptop agent stops being a command (kills F-7, F-9)

Ship 3.3(a), the attach wrapper. Ship a `GOOS=darwin` build target and a
documented install path for the laptop binary. Add reconnect-after-sleep
handling that re-derives state from `Present` rather than from the in-memory
map.

### Phase 4 — revoke, teardown, and the DEAD signal (kills F-14, F-15, F-16, F-17)

Add `Op: revoke`. Wire `stop` / `rm` / `worktree.removed` to enqueue revokes.
Add the disappeared-listener auto-revoke. Add the periodic laptop-side `Present`
sweep with transition-triggered panes in both directions. Rewrite `status` to
print laptop-derived per-port state.

Phase 4 is last only because it depends on OQ-3 (can the laptop reach the
operator's screen). If OQ-3 resolves early and cheaply, the DEAD-signal half of
Phase 4 should be pulled forward ahead of Phase 3 — a forward that dies silently
is worse than a forward that takes one command to start.

### Phase 5 — launchd/systemd service (3.3b)

Only after Phase 3 has established where the target comes from.

---

## 5. OPEN QUESTIONS — measure before building

Ordered by how much of the design collapses if the answer is unfavourable.

**OQ-1 — Does herdr 0.8.0 pass the clicked URL to a link-handler action?**
`HERDR_PLUGIN_CLICKED_URL` and `HERDR_PLUGIN_LINK_HANDLER_ID` appear as
literals in the binary alongside the other plugin env names, and `clicked_url` /
`link_handler_id` appear in the plugin-context struct field list. UNVERIFIED —
this is `strings` output, not behaviour. *Measure:* point a link handler at an
action that dumps its environment to a file, Ctrl-click a
`http://127.0.0.1:3000` in a pane, read the file. Require **both** outcomes:
the variable present with the right value on a match, and absent (or the action
not fired) with no match. Blocks Phase 1a. If the answer is no, Phase 1a becomes
"parse the port out of `selected_text`" or, failing that, is dropped and Phase 2
carries the whole detection burden.

**OQ-2 — Can the laptop-side agent reach the operator's screen?**
Under `--remote` the herdr server is on the engine. `herdr plugin pane open`
from the laptop has no local server to talk to. UNVERIFIED. *Measure:* run
`herdr plugin pane open` on the laptop during a live `--remote` attach; observe
whether a pane appears in the operator's TUI, errors, or opens against a
different session. Determines whether the feedback layer is direct or relayed
back over the ssh master. Blocks the whole of 3.5 and most of Phase 4.

**OQ-3 — Does a post-sleep ControlMaster answer `ssh -O check` with 0?**
The design's DEAD detection assumes it can, and therefore refuses to trust
`-O check`. UNVERIFIED on this laptop/engine pairing. *Measure:* open a master
with a live forward, sleep the laptop, wake it, and record `ssh -O check` exit
code, `Present(port)`, and an actual HTTP fetch — all three, together. Then
repeat with `ServerAliveInterval` set and require a **different** outcome. A
single run cannot distinguish a working keepalive from an ignored option.

**OQ-4 — What does a `msb exec netstat` poll cost, and at what cadence?**
Phase 2 runs one exec per running sandbox per tick. Unmeasured. *Measure:*
wall-clock per `DiscoverAll` at 1, 3, and 5 sandboxes; guest CPU impact under a
running build. Sets the poll interval, and may force event-driven detection
instead.

**OQ-5 — What starts and supervises the laptop agent on macOS?**
The laptop is macOS. launchd behaviour across sleep, across reboot, and its
`KeepAlive` semantics are untested here. UNVERIFIED. *Measure:* only after
Phase 3(a) makes the target discoverable. Blocks Phase 5 only.

**OQ-6 — Does this tree build for darwin?**
`terminal_other.go` exists as a non-linux stub, which is encouraging.
`make build` has no cross target and the manifest says `platforms = ["linux"]`.
UNVERIFIED. *Measure:* `GOOS=darwin GOARCH=arm64 go build ./cmd/herdr-plugin-msb`
— through a make target, per this repo's build rules. Cheap; do it early, since
a negative answer changes Phase 3 substantially.

**OQ-7 — Is `ssh -O forward` idempotent for an already-applied port?**
Recorded as unverified in `doc/portfwd-ssh-mechanism.md` ("Is forward apply
idempotent?"). Phase 4's reconcile-from-`Present` loop will re-apply after a
master restart and needs the answer. *Measure:* apply twice, record both exit
codes and `Present` after each.

**Not open, and not to be re-litigated:** host-side port enumeration
(falsified three ways, `doc/portfwd-discovery.md`); process-ancestry attribution
across a netns (falsified, F5); post-hoc port publish in microsandbox v0.6.17
(falsified four ways, `doc/port-publish.md`); port renumbering (excluded by
MSP-R-006 — Vite HMR and OAuth `redirect_uri`).
