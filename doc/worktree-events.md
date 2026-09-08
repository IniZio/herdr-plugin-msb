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

    herdr-plugin-msb space-prune --apply --workspace "$WS_ID"

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

    herdr-plugin-msb space-prune [--apply] [--all] [--workspace <herdr-workspace-id>]

Default is a **dry run** that surveys every binding and changes nothing. Reclaiming is
destructive, so it is never the default.

Per-binding output is one line, then a trailer:

    space-prune: keep <handle> workspace=<id> reason=workspace-alive
    space-prune: would-reclaim <handle> workspace=<id> reason=workspace-gone
    space-prune: reclaimed <handle> workspace=<id> reason=worktree-gone:<path>
    space-prune: considered=<N> reclaimable=<M> applied=<K> apply=<true|false>

### 3.1 The strandedness rule is fail-safe

1. Probe the live workspace set once, via `herdr workspace list` (there is no `--json`
   flag; the command always emits JSON). Selectors: `.result.workspaces[].workspace_id`
   and `.result.workspaces[].worktree.checkout_path`. If that probe fails for any reason —
   binary absent, non-zero exit, unparseable output — the verb reclaims **nothing** and
   exits 1. A transient herdr outage must never read as "every workspace is gone".
2. `workspace-gone`: the binding's workspace id is absent from that successfully parsed
   set.
3. `worktree-gone:<path>`: the workspace is live but its checkout path no longer exists.
4. A live workspace with an empty checkout path is **kept**. Missing data never reclaims.

### 3.2 `--apply` without `--workspace` requires `--all`

An unscoped destructive sweep is a usage error unless `--all` is passed explicitly:

    $ ./herdr-plugin-msb space-prune --apply
    space-prune: --apply without --workspace requires --all (refusing to reclaim every binding)
    (exit 2)

This is not ceremony. At the time of writing, the operator's demo binding
(`msb:eyeball` -> `herdr/eyeball`, workspace `w8E`) is genuinely stranded by rule 2: the
sandbox is running and its workspace is gone. An unscoped `--apply` would reclaim it. The
refusal, not luck, is what keeps a live sandbox alive; the hook always passes
`--workspace`, so the hook path can never trigger a sweep.

### 3.3 A stranded sandbox is usually still running

The first live run of the reclamation path failed, and the failure was the useful part:

    msb: remove "herdr--s52probe": sandbox still running: cannot remove sandbox 'herdr--s52probe': still running
    space-prune: considered=1 reclaimable=1 applied=0 apply=true   (exit 1)

microsandbox refuses to remove a running sandbox. But a stranded sandbox is running by
definition — that is precisely why it holds unswappable guest RAM — so a plain remove
reclaims nothing in the only case that matters. Reclamation is now remove, then on failure
stop, then remove again (`pruneReclaim`). If the stop also fails, both errors surface and
the binding is **kept**, so a sandbox that could not be reclaimed does not lose its record.

Re-run after the fix, same binary, two opposite outcomes:

    $ ./herdr-plugin-msb space-prune --apply --workspace w8
    space-prune: keep herdr/s52probe workspace=w8 reason=workspace-alive
    space-prune: considered=1 reclaimable=0 applied=0 apply=true      (exit 0, sandbox still running)

    $ ./herdr-plugin-msb space-prune --apply --workspace w8DEAD
    space-prune: reclaimed herdr/s52probe workspace=w8DEAD reason=workspace-gone
    space-prune: considered=1 reclaimable=1 applied=1 apply=true      (exit 0, sandbox gone from msb list)

A `--workspace` id with no binding is not an error — it reports `considered=0` and exits 0.
Most removed worktrees were never converted to a sandbox.

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
