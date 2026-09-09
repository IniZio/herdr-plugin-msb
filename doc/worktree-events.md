# Worktree lifecycle events and sandbox reclamation

Slice s52. Companion to `doc/herdr-event-contract.md` (measured herdr 0.8.0 facts) and
`doc/worktree-convert.md` (the `space-convert` verb and the binding store).

## 1. The problem

Before this slice the lifecycle ran one way only: `space-convert` created a sandbox and
wrote a binding, and nothing ever ran the reverse. Deleting a herdr worktree left its
sandbox running with no owner. Guest RAM under microsandbox is memfd-backed, therefore
resident and unswappable, so a stranded sandbox holds host RAM until someone notices by
hand.

Two mechanisms close the loop, and both are needed:

- an event hook on `worktree.removed`, which reclaims immediately when herdr drives the
  removal;
- the `space-prune` verb, a backstop that reclaims whatever the hook missed.

The hook alone cannot be the only path. Delivery is non-blocking, one-shot, with **no
retry** (`doc/herdr-event-contract.md` §A3), so a hook that fails loses the work
permanently. And the hook does not fire at all for the removal path in §4 below.

## 2. The hook scripts

`plugins/herdr/bin/on-worktree-created.sh` — logs the event only. It does not create a
sandbox: creation needs an image ref and stays the operator-driven `space-convert` path.

`plugins/herdr/bin/on-worktree-removed.sh` — logs the event, then invokes

    herdr-plugin-msb space-prune --apply --kill-running --workspace "$WS_ID"

**The hook destroys in-flight work inside that guest, without confirmation.**
`--kill-running` means the reclaim does not stop at a RUNNING sandbox: whatever is
executing in the guest — an agent mid-task, an unfinished build, uncommitted files that
live only in the guest — is killed and the sandbox is removed, with no prompt and no
recovery. This is the accepted trade, decided by the operator: `herdr worktree remove`
is an explicit operator action on that worktree, and reclaiming the worktree's sandbox is
the intended consequence of it. Anything inside a worktree-bound sandbox that must
survive has to be pushed out of the guest before the worktree is removed. The blast
radius is bounded by `--workspace "$WS_ID"` — only bindings for the removed worktree's
workspace are considered — and by the two-signal strand rule in §3.1, which still applies
in full: `--kill-running` waives only the running-status refusal, never the strand test.

The `[[events]]` manifest entries pointing at both scripts were written by slice s51 and
were not modified here.

Both scripts append one line per invocation to
`${XDG_STATE_HOME:-$HOME/.local/state}/herdr-plugin-msb/herdr-events.log`, carrying the
extracted values rather than a bare "fired":

    <UTC timestamp> event=<dotted event> workspace=<id> worktree=<path> source=<env|json>

That log is the audit channel. A `-` in the `workspace=` or `worktree=` field means the
envelope parse returned empty, which is the failure this slice exists to avoid.

### 2.1 The envelope, and why the obvious selector returns nothing

`HERDR_PLUGIN_EVENT_JSON` on herdr 0.8.0 is an **envelope**, not the bare event object,
and the event name is spelled two ways at once — dotted in the environment variable,
underscored inside the JSON:

    HERDR_PLUGIN_EVENT=worktree.removed
    HERDR_PLUGIN_EVENT_JSON={"event":"worktree_removed","data":{"type":"worktree_removed",
      "workspace_id":"<id>","worktree":{...},"forced":<bool>,"workspace":<info>|null}}

Correct selectors:

| event | field | selector |
|---|---|---|
| `worktree.created` | workspace id | `.data.workspace.workspace_id` |
| `worktree.created` | checkout path | `.data.worktree.path` |
| `worktree.removed` | workspace id | `.data.workspace_id` |
| `worktree.removed` | checkout path | `.data.worktree.path` |

`.data.workspace` is **nullable** on removal; `.data.workspace_id` is not, which is why
the two events use different selectors.

nexus3's hook scripts select `.workspace.workspace_id` — without the envelope. Against
0.8.0 that returns empty and yields a hook that fires and silently does nothing. Measured
opposite outcomes on the same captured payload:

    jq -r '.workspace.workspace_id // empty'        ->  (empty)
    jq -r '.data.workspace.workspace_id // empty'   ->  ws-abc123

### 2.2 `HERDR_WORKSPACE_ID` is set for `worktree.removed`

`doc/herdr-event-contract.md` lists this as an explicit UNKNOWN — the variable was
confirmed only for `workspace.created`, and the workspace is arguably gone by the time a
removal fires. **Now measured: it is set.** Both scripts prefer the environment variable
and fall back to the jq selector, and both live firings logged `source=env`:

    2026-09-08T17:15:28Z event=worktree.created workspace=w8M worktree=/home/newman/magic/herdr-plugin-msb-s52a source=env
    2026-09-08T17:16:17Z event=worktree.removed workspace=w8M worktree=/home/newman/magic/herdr-plugin-msb-s52a source=env

The jq fallback is retained: the env-var guarantee is measured on 0.8.0 only, and the
fallback costs nothing.

## 3. The `space-prune` verb

    herdr-plugin-msb space-prune [--apply] [--all] [--kill-running] [--workspace <herdr-workspace-id>]

Default is a **dry run** that surveys every binding and changes nothing. Reclaiming is
destructive, so it is never the default.

Per-binding output is one line, then a trailer:

    space-prune: keep <handle> workspace=<id> reason=workspace-alive
    space-prune: would-reclaim <handle> workspace=<id> reason=workspace-gone+worktree-gone:<path>
    space-prune: reclaimed <handle> workspace=<id> reason=workspace-gone+worktree-gone:<path>
    space-prune: considered=<N> reclaimable=<M> applied=<K> apply=<true|false>

### 3.1 Strandedness needs two independent signals

A binding records both the herdr workspace id and, since this slice, the worktree
`checkout_path` it was created from. Reclaiming is unrecoverable, so one signal is not
enough to authorise it: any event that invalidates herdr workspace ids would otherwise
mark **every** binding stranded and a single `--apply --all` would reclaim every sandbox
on the host.

1. Probe the live workspace set once, via `herdr workspace list` (there is no `--json`
   flag; the command always emits JSON). Selector: `.result.workspaces[].workspace_id`.
   If that probe fails for any reason — binary absent, non-zero exit, unparseable output —
   the verb reclaims **nothing** and exits 1. A transient herdr outage must never read as
   "every workspace is gone".
2. Signal A, `workspace-gone`: the binding's workspace id is absent from that successfully
   parsed set.
3. Signal B, `worktree-gone`: the binding's recorded `checkout_path` no longer exists on
   disk.
4. Only `workspace-gone+worktree-gone:<path>` — **both** signals — is stranded. One signal
   alone is reported and kept: `workspace-gone-worktree-present:<path>` and
   `worktree-gone-workspace-alive:<path>`.
5. A binding with **no** recorded `checkout_path` is always kept, reason
   `no-checkout-path-recorded`. Corroboration is impossible, so the fail-safe answer is to
   reclaim nothing. This is deliberate and it covers two populations: bindings written
   before the field existed, and `space-create` bindings, whose workspace has no worktree
   at all. Reclaim those by hand (`rm` the sandbox, then drop the binding record) after
   confirming they are dead. The alternative — treating an absent path as corroboration —
   is exactly the one-signal behaviour rule 4 exists to forbid.


### 3.2 `--apply` without `--workspace` requires `--all`

An unscoped destructive sweep is a usage error unless `--all` is passed explicitly:

    $ ./herdr-plugin-msb space-prune --apply
    space-prune: --apply without --workspace requires --all (refusing to reclaim every binding)
    (exit 2)

This is not ceremony. The operator's demo binding (`msb:eyeball` -> `herdr/eyeball`,
workspace `w8E`) was classified `would-reclaim ... reason=workspace-gone` under the
one-signal rule, on a **running** 2 GiB sandbox, so `--apply --workspace w8E` would have
destroyed it and this refusal was the only thing standing in the way. It is no longer the
only thing: §3.1 rule 5 and §3.4 are two further independent refusals on that same
binding. Keep all three. The hook always passes `--workspace`, so the hook path can never
trigger a sweep.

Dry run against the live store after this slice:

    $ ./herdr-plugin-msb space-prune
    space-prune: keep herdr/eyeball workspace=w8E reason=no-checkout-path-recorded
    space-prune: considered=1 reclaimable=0 applied=0 apply=false

### 3.3 A stranded sandbox is usually still running

The first live run of the reclamation path failed, and the failure was the useful part:

    msb: remove "herdr--s52probe": sandbox still running: cannot remove sandbox 'herdr--s52probe': still running
    space-prune: considered=1 reclaimable=1 applied=0 apply=true   (exit 1)

microsandbox refuses to remove a running sandbox. But a stranded sandbox is running by
definition — that is precisely why it holds unswappable guest RAM — so a plain remove
reclaims nothing in the only case that matters. Reclamation is now remove, then on failure
stop, then remove again (`pruneReclaim`). If the stop also fails, both errors surface and
the binding is **kept**, so a sandbox that could not be reclaimed does not lose its record.

Read `pruneReclaim` (`internal/cli/cmd_herdrspace_prune.go:76-86`) for the exact order; it
is remove-first, and the stop is a fallback that runs only on a failed remove:

    err := pruneRemoveSandbox(ctx, project, name)   // attempted unconditionally, first
    if err == nil { return nil }                    // a stopped sandbox removes on the first try
    if stopErr := pruneStopSandbox(...); stopErr != nil { return ...both errors... }
    return pruneRemoveSandbox(ctx, project, name)   // retry, after the stop

Commit ef65343's message says reclaim "stops a running sandbox before removing it". That
is **wrong** and the commit message cannot be edited, so the correction is recorded here:
nothing is stopped before the first remove attempt, and a sandbox that is not running is
removed without ever being stopped. The distinction is observable — a stop is issued only
on the failure path, so a successful first remove leaves no stop in the msb logs.

The status probe in §3.4 is a separate, earlier step at the call site
(`cmd_herdrspace_prune.go:148-159`), not part of `pruneReclaim`. It decides whether the
reclaim is attempted at all; it does not stop anything.

Re-run after the fix, same binary, two opposite outcomes:

    $ ./herdr-plugin-msb space-prune --apply --workspace w8
    space-prune: keep herdr/s52probe workspace=w8 reason=workspace-alive
    space-prune: considered=1 reclaimable=0 applied=0 apply=true      (exit 0, sandbox still running)

    $ ./herdr-plugin-msb space-prune --apply --workspace w8DEAD
    space-prune: reclaimed herdr/s52probe workspace=w8DEAD reason=workspace-gone
    space-prune: considered=1 reclaimable=1 applied=1 apply=true      (exit 0, sandbox gone from msb list)

A `--workspace` id with no binding is not an error — it reports `considered=0` and exits 0.
Most removed worktrees were never converted to a sandbox.

### 3.4 Reclaiming a RUNNING sandbox needs `--kill-running`

A stranded sandbox is running by definition (§3.3), so the reclaim path is the difference
between clearing stale leftovers and killing whatever is running inside a live microVM.
Before removing, `space-prune` resolves the sandbox's status:

- status `running` without `--kill-running`: refuse, print
  `space-prune: refusing to reclaim RUNNING sandbox <handle> ...` to stderr, keep the
  binding, exit 1.
- status probe fails for any reason other than "no such sandbox": refuse the same way. An
  unknown status is never read as "not running".
- sandbox absent from msb entirely: not running; the binding is a leftover record and is
  reclaimed normally.

`--kill-running` is the operator's explicit statement that destroying in-flight work is
acceptable. Two runs against a real running sandbox, same binary:

    $ space-prune --apply --workspace w56DEAD
    space-prune: refusing to reclaim RUNNING sandbox herdr/s56probe workspace=w56DEAD (pass --kill-running to destroy it and any in-flight work)
    space-prune: considered=1 reclaimable=1 applied=0 apply=true      (exit 1, sandbox still in msb list)

    $ space-prune --apply --workspace w56DEAD --kill-running
    space-prune: reclaimed herdr/s56probe workspace=w56DEAD reason=workspace-gone+worktree-gone:/home/newman/magic/does-not-exist-s56
    space-prune: considered=1 reclaimable=1 applied=1 apply=true      (exit 0, sandbox gone from msb list)

Consequence for the hook in §2, and the operator decision that settled it:

Between s56 and this slice, `on-worktree-removed.sh` passed `--apply --workspace` only.
A stranded sandbox is running by definition (§3.3), so the hook **refused** every sandbox
it was written to reclaim, logged `space-prune=done rc=1`, and reclaimed nothing. That is
a safe failure, but it is a failure: it left exactly the stranded, RAM-holding sandbox the
hook exists to remove.

The hook now passes `--kill-running` (§2). The operator's reasoning: `herdr worktree
remove` is a deliberate, hand-driven act on that worktree, and destroying the worktree's
sandbox is its intended consequence — so the guest's in-flight work is forfeit, without
confirmation. Read §2 before relying on this.

`--kill-running` on the hook waives the running-status refusal in this section only. It
does not touch the other two refusals: the two-signal strand rule (§3.1) and the
`no-checkout-path-recorded` keep (§3.1 rule 5) both still hold, and the hook still always
scopes to one `--workspace`, so it can never sweep (§3.2). The operator's demo binding
(`msb:eyeball` -> `herdr/eyeball`, workspace `w8E`) records no checkout path and is
therefore kept whatever flags are passed.

### 3.5 The lifecycle, proven end to end

Until this slice every part of the reclaim path had been proven separately and the whole
had never run. The one real hook firing on record logged
`space-prune=done rc=2 output=... unknown command "space-prune"`: the binary the hook
resolves predated the verb, so the lifecycle had never once completed. `make install`
fixes that, and the plugin is registered with `plugin_root` = this repo
(`~/.config/herdr/plugins.json`), so the hook runs the repo's script and the repo's binary
— `make build` and `make install` both matter.

Two full runs on throwaway worktrees, differing only in whether the hook passed
`--kill-running`, each with a real 1 GiB `alpine` guest that was RUNNING at removal time:

| run | hook argv | events-log line | `msb list` after |
|---|---|---|---|
| negative | `space-prune --apply --workspace w8S` | `rc=1 output=space-prune: refusing to reclaim RUNNING sandbox herdr/s57probe3 workspace=w8S (pass --kill-running ...)` / `considered=1 reclaimable=1 applied=0` | `herdr--s57probe3` still running |
| positive | `space-prune --apply --kill-running --workspace w8R` | `rc=0 output=space-prune: reclaimed herdr/s57probe2 workspace=w8R reason=workspace-gone+worktree-gone:<path>` / `considered=1 reclaimable=1 applied=1` | gone |

In the positive run nothing removed the sandbox by hand: the only commands issued were
`herdr worktree create`, `space-convert`, and `herdr worktree remove --force`. herdr's own
`herdr plugin log list` records that invocation of `plugins/herdr/bin/on-worktree-removed.sh`
as `exit_code: 0, status: succeeded`, and the binding disappeared from
`herdr-space-bindings.json`. The operator's `msb:eyeball` binding was `keep
reason=no-checkout-path-recorded` in the dry run before and after.

### 3.6 A replaced herdr binary broke the hook, and how

The first end-to-end attempt failed a second way, and only the live run could have found
it:

    space-prune=done rc=1 output=herdr workspace list: fork/exec /home/newman/.local/bin/herdr (deleted): no such file or directory

`herdr` had been upgraded while the server process kept running, so the server's
`/proc/self/exe` reads `/home/newman/.local/bin/herdr (deleted)` — the kernel's suffix for
an unlinked inode — and that literal string reaches the hook as `HERDR_BIN_PATH`.
`space-prune` needs `herdr workspace list` for signal A (§3.1), so the workspace probe
failed, and by rule the verb then reclaims **nothing** and exits 1. The failure was safe
and completely opaque.

`herdrBin` now strips a trailing ` (deleted)` and uses the stripped path **only if that
path exists**; otherwise the value is returned untouched, so a genuinely wrong
`HERDR_BIN_PATH` still fails loudly instead of silently resolving elsewhere. Confirmed by
observation, not inference: `readlink /proc/<herdr-server-pid>/exe` printed the `(deleted)`
suffix while a freshly started `herdr` process printed the clean path.

## 4. Limitation: only herdr-driven removals fire the hook

`worktree.removed` fires when herdr drives the removal. A plain `git worktree remove`
outside herdr does **not** fire it.

`doc/herdr-event-contract.md` §A4 rated this MEDIUM confidence, inherited from nexus3 and
not re-proven. **It is now proven here**, with two runs and opposite outcomes against the
real linked plugin:

| run | command | events log | plugin log |
|---|---|---|---|
| negative | `git -C <repo> worktree remove <path>` | no new line | no new invocation |
| positive | `herdr worktree remove --workspace w8M` | `event=worktree.removed workspace=w8M worktree=<path> source=env` | exit 0, succeeded |

Both probe worktrees were created identically with `herdr worktree create`, so the only
variable between the runs was the removal path.

This gap is the reason `space-prune` exists as a verb rather than only as hook-internal
logic. Reclamation after a plain `git worktree remove` requires running the verb — from
the operator's hand, or from a periodic sweep.

## 5. Rejected: reclaiming an unbound sandbox

`space-prune` only ever considers sandboxes that appear in the binding store. A sandbox
with no binding is left alone, however idle it looks: this plugin is not the only thing
that may create sandboxes in the `herdr` project, and inferring ownership from a name
prefix would make the destructive path guess. If it was never bound, it was never ours to
reclaim.
