# Worktree-to-sandbox workspace conversion

This file is the rationale for `space-convert` and the worktree event hooks.
Go comment density is capped at 5 per 100 lines repo-wide, so design intent lives here.

## 1. The problem

`space-create` (`internal/cli/cmd_herdrspace.go:88`) always calls `herdrWorkspaceCreate`
(line 37), which runs `herdr workspace create`. An operator who already had a herdr worktree
workspace therefore ended up with two objects — the original worktree workspace and a
newly-created sandbox workspace — rather than one converted workspace.

`space-convert` exists to bind an **existing** workspace id instead of creating a new one.
It receives the operator-supplied workspace id, skips `herderWorkspaceCreate`, and proceeds
directly to `herdrOpenGuestPane`.

## 2. Why split-then-close, not close-then-open

Per `doc/herdr-event-contract.md` §B (measured on workspace `w8J`): closing the **last**
pane of a workspace destroys the workspace immediately:

```
$ herdr workspace get w8J
{"error":{"code":"workspace_not_found","message":"workspace w8J not found"}}
```

There is no window in which the workspace exists with zero panes. Close-then-open is
therefore not available.

The required order for in-place conversion is:

1. Open the guest tty as a **split** off the existing root pane.
   `herdrOpenGuestPane` (`cmd_herdrspace.go:68`) already does this when `rootPaneID != ""`:
   ```
   --placement split --target-pane <rootPaneID> --direction right
   ```
2. Only then close the old root pane with `herdr pane close <oldRootPaneID>`.

What survives: `workspace_id` and `tab_id` are preserved; `pane_id` changes (the new pane
gets a new id). Measured on `w8J`:

```
ws=w8J root=w8J:p1 tab=w8J:t1
new pane=w8J:p2            # herdr pane split w8J:p1 --direction right
close root exit=0          # herdr pane close w8J:p1
--- after closing root ---
pane w8J:p2  tab w8J:t1  ws w8J
```

## 3. The rejected alternative: pane run

`herdr pane run <paneID> "<cmd>"` (`pane.send_input` API) types the command into the pane's
live shell. It preserves `pane_id`, `tab_id`, AND `workspace_id` — full identity. That is
the only path that achieves true in-place replacement.

It was **not chosen** for `space-convert` because it requires the root pane to be sitting
at an interactive shell prompt. A pane running an agent, a build, a blocking process, or
even `vim` would receive the command text as raw input, producing unpredictable behaviour.
Split-then-close works regardless of what the root pane is doing.

The trade-off is deliberate: pane-id continuity is sacrificed for reliability. Callers that
care about pane-id stability should record the new pane id returned by `herdrOpenGuestPane`
immediately after the split.

## 4. The event hooks

`herdr-plugin.toml` carries two `[[events]]` entries:

```toml
[[events]]
on = "worktree.created"
command = ["sh", "plugins/herdr/bin/on-worktree-created.sh"]
platforms = ["linux"]

[[events]]
on = "worktree.removed"
command = ["sh", "plugins/herdr/bin/on-worktree-removed.sh"]
platforms = ["linux"]
```

**The scripts are not yet written.** A later slice implements them. This section documents
the 0.8.0 payload contract they must honour.

### 4.1 Payload contract (herdr 0.8.0)

See `doc/herdr-event-contract.md` §A2 for the full measured delivery record. Summary for
the worktree events:

`HERDR_PLUGIN_EVENT` carries the **dotted** name (`worktree.created`).
`HERDR_PLUGIN_EVENT_JSON` carries an **envelope**:

```json
{"event":"worktree_created",
 "data":{"type":"worktree_created","workspace":{...},"worktree":{...}}}
```

Note the dual spelling: dotted in the env var, underscore inside the JSON. The envelope
wraps `data`; the event object is **not** the top-level value.

Correct jq selectors:

- `worktree.created` → workspace id: `jq -r '.data.workspace.workspace_id'`
- `worktree.created` → checkout path: `jq -r '.data.worktree.path'`
- `worktree.removed` → workspace id: `jq -r '.data.workspace_id'`
  (workspace is nullable on removal; `workspace_id` is always present at top of `data`)
- `worktree.removed` → checkout path: `jq -r '.data.worktree.path'`

`worktree_removed` payload (`/schemas/event/$defs/EventData/oneOf/10`):

```json
{"event":"worktree_removed",
 "data":{"type":"worktree_removed","workspace_id":"<id>","worktree":{...},
         "forced":<bool>,"workspace":<WorkspaceInfo>|null}}
```

`workspace` is **nullable** on removal; `workspace_id` at `data.workspace_id` is not.

Additional env vars delivered unconditionally: `HERDR_WORKSPACE_ID`, `HERDR_TAB_ID`,
`HERDR_PANE_ID`, `HERDR_PLUGIN_ID`, `HERDR_PLUGIN_ROOT`, `HERDR_SOCKET_PATH`,
`HERDR_BIN_PATH`, `HERDR_PLUGIN_CONTEXT_JSON`. Prefer `HERDR_WORKSPACE_ID` over
`jq .data.workspace.workspace_id` when the variable suffices — it avoids the envelope parse
entirely.

Delivery is non-blocking and one-shot (no retry). Exit codes are logged but do not fail the
triggering operation. See `doc/herdr-event-contract.md` §A3.

### 4.2 Do not copy nexus3 hook scripts

`/home/newman/magic/nexus3/plugins/herdr/bin/on-worktree-created.sh` extracts the workspace
id with `jq -r '.workspace.workspace_id // empty'` — without the envelope. Against herdr
0.8.0 that returns empty. The nexus3 scripts document a pre-0.8.0 payload shape and are
**wrong** for 0.8.0. Do not port their jq selectors.

### 4.3 Silent-failure trap: typo in `on`

An unknown `on` value never fails a link, build, or run. It produces a hook that silently
never fires. The only detector is `herdr plugin list --json` read through the paths-walk
needed because the top-level envelope is `{"id":...,"result":...}` (a naive `.plugins[]`
returns null):

```sh
herdr plugin list --json | jq -c \
  '[paths(objects) as $p | getpath($p) | select(.plugin_id=="herdr-plugin-msb")] \
   | .[0] | {plugin_id, warnings, events}'
```

Current output (herdr 0.8.0, 2026-09-08):

```json
{"plugin_id":"herdr-plugin-msb","warnings":null,"events":[{"command":["sh","plugins/herdr/bin/on-worktree-created.sh"],"on":"worktree.created","platforms":["linux"]},{"command":["sh","plugins/herdr/bin/on-worktree-removed.sh"],"on":"worktree.removed","platforms":["linux"]}]}
```

`warnings: null` confirms both `on` values are recognised. A non-null `warnings` array
after any manifest change is the signal that a hook has been silently disabled.

## Verification commands

```
$ herdr --version
herdr 0.8.0

$ herdr plugin list --json | jq -c '[paths(objects) as $p | getpath($p) | select(.plugin_id=="herdr-plugin-msb")] | .[0] | {plugin_id, warnings, events}'
{"plugin_id":"herdr-plugin-msb","warnings":null,"events":[{"command":["sh","plugins/herdr/bin/on-worktree-created.sh"],"on":"worktree.created","platforms":["linux"]},{"command":["sh","plugins/herdr/bin/on-worktree-removed.sh"],"on":"worktree.removed","platforms":["linux"]}]}

$ herdr workspace get w8 | jq -c .
{"id":"cli:workspace:get","result":{"type":"workspace_info","workspace":{"active_tab_id":"w8:t1N","agent_status":"working","focused":true,"label":"herdr-plugin-msb","number":2,"pane_count":6,"tab_count":6,"workspace_id":"w8","worktree":{"checkout_path":"/home/newman/magic/herdr-plugin-msb","is_linked_worktree":false,"repo_key":"/home/newman/magic/herdr-plugin-msb/.git","repo_name":"herdr-plugin-msb","repo_root":"/home/newman/magic/herdr-plugin-msb"}}}}
```

The `worktree` field at `.result.workspace.worktree.checkout_path` confirms the workspace
object shape delivered in worktree event payloads.

## The primary checkout is never sandboxed

`space-convert` refuses any workspace whose `worktree.is_linked_worktree` is not `true`.
The check lives in `refuseNonLinkedWorktree` and fires in `runSpaceConvert` immediately
after `herdrWorkspaceGet`, before `herdrRootPaneID`, before `convertCreateSandbox`, and
before `herdrspace.Put` — a refusal that fires after a microVM has booted is not a
refusal. The refusal writes no binding and creates no sandbox.

The test is structural, not a path prefix. A linked worktree can live anywhere, so
`/home/newman/.herdr/worktrees/` is not the signal; `is_linked_worktree` is. The field
absent from the payload is treated as "not a linked worktree" and refused: fail closed.

Rationale. Converting the primary checkout binds the tree the operator and their agents
work in, replaces its root pane with a guest shell, and tears its panes down when the
sandbox is removed. This happened twice. Both times the workspace was `w8`, the operator's
main checkout, and both times it ended with 64 `Microsandbox guest shell` panes in it.

The second occurrence identified the mechanism, which is not a hand-typed command.
`TestShippedVerbRegistry` at `internal/cli/cmd_sandbox_test.go:181` calls
`Run(ctx, []string{verb})` for every shipped verb, `space-convert` included, with
`convertCreateSandbox` unstubbed. Once `--workspace` gained its `$HERDR_WORKSPACE_ID`
default, running `make test` from inside a workspace became a real conversion of that
workspace: a real microVM, a real binding, real panes. Any agent running the gate from
inside `w8` reproduced the incident. The `is_linked_worktree` refusal closes that path,
but the unstubbed verb sweep remains a live hazard for every other mutating verb.

## `make test` can report ok over a red package

`internal/cli` reports `ok` while tests fail. `TestShippedVerbRegistry` runs the
`default-shell` verb, `runDefaultShell` calls `syscall.Exec` at
`internal/cli/cmd_herdrspace_default_shell.go:77`, and the test binary is replaced by
`$SHELL`, which exits 0. Every test declared after it never runs, and the buffered failure
report of every test before it is discarded. Read `-v` output, not the package summary,
when a result matters.
