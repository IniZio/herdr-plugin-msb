# herdr 0.8.0 plugin event contract

Reference note for s51 (`convert` verb, owner of `herdr-plugin.toml` and its
`[[events]]` entries) and s52 (`on-worktree-created.sh` / `on-worktree-removed.sh`).

Subject under test: `/home/newman/.local/bin/herdr`, `herdr --version` → `herdr 0.8.0`.
herdr source is **not** available locally and `github.com/miko-misa/herdr` returns HTTP
404 (`curl -o /dev/null -w '%{http_code}'` → `404` for both the web and API URLs), so every
citation below is either the binary's own machine-readable schema
(`herdr api schema --json`, 251 527 bytes, JSON-Pointer paths quoted) or captured output
from the installed binary. No claim here rests on herdr source line numbers.

## A1 — `[[events]]` manifest schema

Struct is `RawPluginManifestEventHook` (binary symbol
`_ZN5herdr3app3api7plugins7runtime…`, and the manifest type name appears verbatim in
`strings herdr`). Its parsed form is published in the API schema at
`/schemas/success_response/$defs/PluginManifestEventHook`:

```json
{"properties":{"command":{"items":{"type":"string"},"type":"array"},
               "on":{"type":"string"},
               "platforms":{"items":{"$ref":".../PluginPlatform"},"type":["array","null"]}},
 "required":["on","command"],"type":"object"}
```

- `on` — string, **required**, dot-separated (`worktree.created`), not underscore.
- `command` — array of argv strings, **required**. Relative paths resolve from the plugin
  root (`HERDR_PLUGIN_ROOT`); confirmed by the live env capture in A2.
- `platforms` — optional array; omit rather than pass `[]`
  (`"platforms must not be an empty array; omit the field to leave platforms undeclared"`).
- There is **no** `id`, `title`, `description` or timeout key on an event hook.

### Accepted `on` values — measured, with both outcomes

The plugin-hook registry is a **22-name subset** of the 27 API subscription names at
`/schemas/request/$defs/Subscription/oneOf/*/properties/type/const`. Measured by linking a
probe manifest declaring all 27 and reading back `warnings`:

```
REJECTED (5): layout.updated, pane.output_matched, pane.scroll_changed,
              pane.updated, workspace.metadata_updated
ACCEPTED (22): pane.agent_detected pane.agent_status_changed pane.closed pane.created
               pane.exited pane.focused pane.moved tab.closed tab.created tab.focused
               tab.moved tab.renamed workspace.closed workspace.created workspace.focused
               workspace.moved workspace.renamed workspace.reordered workspace.updated
               worktree.created worktree.opened worktree.removed
```

`startup` and `plugin.startup` are **rejected** as `on` values — startup commands go in the
separate `[[startup]]` table (`PluginManifestStartup`, `command` only).

### An unknown `on` is a NON-FATAL warning, not an error

Negative control, two runs, opposite outcomes, identical in every other respect:

```
$ herdr plugin link <probeA>   # on = "worktree.created"
{"result":{"plugin":{...,"events":[{"command":["sh","-c","true"],"on":"worktree.created"}],
 "plugin_id":"s50.probe-valid",...}}}          # ← no "warnings" key

$ herdr plugin link <probeB>   # on = "worktree_created"
{"result":{"plugin":{...,"events":[{"command":["sh","-c","true"],"on":"worktree_created"}],
 "plugin_id":"s50.probe-invalid",...,
 "warnings":["unknown event 'worktree_created'"]}}}
```

Both links **succeed**. The schema says so as well
(`/schemas/success_response/$defs/InstalledPluginInfo/properties/warnings/description`):
"Warnings collected at link time or on registry load (e.g. unknown event names, missing
manifest file). Non-fatal — the entry is kept and surfaced by plugin.list."

**Consequence for s51:** a typo in `on` never fails a build, a link, or a run. It produces a
hook that silently never fires. The only detector is `herdr plugin list --json` →
`.plugins[].warnings`. This is the exact defect nexus3 recorded as D-HSH-20. Any s51 test
that asserts the manifest parses proves nothing; assert `warnings` is empty.

## A2 — delivery channel and payload

`HERDR_PLUGIN_EVENT_JSON` is still the channel — and its shape is **an envelope**, not the
bare event object. Captured from a real fired hook (probe plugin on `workspace.created`,
hook `sh -c 'env > …'`, fired by `herdr workspace create --no-focus --label s50-probe`):

```
HERDR_PLUGIN_EVENT=workspace.created
HERDR_PLUGIN_EVENT_JSON={"event":"workspace_created","data":{"type":"workspace_created",
  "workspace":{"workspace_id":"w8G","number":7,"label":"s50-probe","focused":false,
  "pane_count":1,"tab_count":1,"active_tab_id":"w8G:t1","agent_status":"unknown"}}}
HERDR_WORKSPACE_ID=w8G
HERDR_TAB_ID=w8G:t1
HERDR_PANE_ID=w8G:p1
HERDR_PLUGIN_ID=s50.probe-capture
HERDR_PLUGIN_ROOT=/…/probeE
HERDR_PLUGIN_CONFIG_DIR=/home/newman/.config/herdr/plugins/config/s50.probe-capture
HERDR_PLUGIN_STATE_DIR=/home/newman/.local/state/herdr/plugins/s50.probe-capture
HERDR_SOCKET_PATH=/home/newman/.config/herdr/herdr.sock
HERDR_BIN_PATH=/home/newman/.local/bin/herdr (deleted)
HERDR_ENV=1
HERDR_PLUGIN_CONTEXT_JSON={"workspace_id":"w8G","workspace_label":"s50-probe",
  "workspace_cwd":"…","tab_id":"w8G:t1","tab_label":"1","focused_pane_id":"w8G:p1",
  "focused_pane_cwd":"…","focused_pane_status":"unknown","invocation_source":"api",
  "correlation_id":"workspace.created"}
```

Envelope: `{"event":"<underscore_name>","data":<EventData>}`.
`HERDR_PLUGIN_EVENT` carries the **dotted** name; the envelope's `event` and the payload's
`data.type` carry the **underscore** name. Both spellings are live at once — that dual
spelling is what produced D-HSH-20.

`data` matches the API schema's `EventData` variant exactly (verified for
`workspace_created` against `/schemas/event/$defs/EventData/oneOf/*`). Reading the worktree
variants off the same schema:

`worktree_created` (`/schemas/event/$defs/EventData/oneOf/8`) — required
`type`, `workspace`, `worktree`:

```json
{"event":"worktree_created",
 "data":{"type":"worktree_created","workspace":<WorkspaceInfo>,"worktree":<WorktreeInfo>}}
```

`worktree_removed` (`/schemas/event/$defs/EventData/oneOf/10`) — required
`type`, `workspace_id`, `worktree`, `forced`; `workspace` is present but **nullable**:

```json
{"event":"worktree_removed",
 "data":{"type":"worktree_removed","workspace_id":"<id>","worktree":<WorktreeInfo>,
         "forced":<bool>,"workspace":<WorkspaceInfo>|null}}
```

`WorktreeInfo` (`/schemas/event/$defs/WorktreeInfo`) — required `path`, `is_bare`,
`is_detached`, `is_prunable`, `is_linked_worktree`, `label`; optional `branch`,
`open_workspace_id`.
`WorkspaceInfo` (`/schemas/event/$defs/WorkspaceInfo`) — required `workspace_id`, `number`,
`label`, `focused`, `pane_count`, `tab_count`, `active_tab_id`, `agent_status`; optional
`worktree`, `tokens`.

**Trap for s52.** nexus3's hook scripts document the payload without the envelope —
`/home/newman/magic/nexus3/plugins/herdr/bin/on-worktree-removed.sh:10` says
`{"type":"worktree_removed","workspace_id":"<id>",…}` at top level, and
`on-worktree-created.sh:21` extracts with `jq -r '.workspace.workspace_id // empty'`.
Against herdr 0.8.0 that jq returns empty. The correct selectors are:

- created: `jq -r '.data.workspace.workspace_id'`
- removed: `jq -r '.data.workspace_id'`, path via `jq -r '.data.worktree.path'`

Do **not** port those nexus3 comments. Prefer `HERDR_WORKSPACE_ID` where it suffices; it is
set on every hook invocation (captured above). Whether `HERDR_WORKSPACE_ID` is set for
`worktree.removed` specifically was **not** captured — s52 must confirm it or use the
envelope path.

## A3 — delivery semantics: non-blocking, one-shot, exit code logged

Measured with a probe hook `["sh","-c","sleep 3; exit 7"]` on `workspace.created`:

```
workspace.create returned after 5 ms         # hook still sleeping
created ws: w8H                              # operation completed regardless
$ herdr plugin log list --plugin s50.probe-fail
{'log_id':'plugin-log-39','event':'workspace.created','status':'failed','exit_code':7,
 'started_unix_ms':1788885455674,'finished_unix_ms':1788885458676,'error':None}
```

- **Asynchronous / non-blocking.** The API call returned in 5 ms against a 3 002 ms hook.
- **A failing hook does not abort or roll back the herdr operation.** The workspace was
  created and stayed.
- **One-shot: no retry.** Exactly one log row for a hook that exited 7. (Caveat: proven for
  exit 7; herdr shows no retry field anywhere, but only this one failure mode was run.)
- **Fan-out is concurrent across plugins.** Historic rows from a real worktree event show
  two plugins with the same `started_unix_ms` (`plugin-log-5` `persiyanov.reviewr` and
  `plugin-log-6` `nexus3`, both `"event":"worktree.created"`, both `started_unix_ms`
  1788760977340).
- **Observability.** `herdr plugin log list [--plugin ID] [--limit N]` records
  `log_id, plugin_id, command, event, status (running|succeeded|failed), exit_code,
  started_unix_ms, finished_unix_ms, stdout, stderr, error`
  (`/schemas/success_response/$defs/PluginCommandLogInfo`). stdout and stderr are captured
  in full — this is the s52 debugging surface.
- **Concurrency cap exists**: the binary carries
  `"maximum concurrent plugin commands reached ("` and the error code
  `plugin_command_limit_reached`. The numeric limit was not measured.
- **A hung hook: UNKNOWN.** No timeout string was found and no hang was run. A hook that
  never exits stays `running` and holds a concurrency slot. s52 hooks must impose their own
  timeout and must not block on anything unbounded.

## A4 — `worktree.removed` fires only when herdr drives the removal

**Confirmed as inherited, not re-proven in this slice.** nexus3 recorded the answer in
`/home/newman/magic/nexus3/plugins/herdr/bin/on-worktree-removed.sh:15-18`:
"OQ-1 (answered in session): worktree.removed fires ONLY when herdr drives the removal
(`herdr worktree remove`). A plain `git worktree remove` outside herdr does NOT fire this
hook." Corroborating, non-conclusive: the binary's only worktree-removal path strings are
herdr's own verb (`"worktree.remove is handled asynchronously by the app runtime"`,
`worktree_remove_failed`, `dirty_worktree_requires_force`) and there is no filesystem-watch
or `git worktree prune` reconciliation string anywhere in it.

This slice did **not** re-run the negative control (it requires creating and destroying a
real git worktree). Treat as MEDIUM confidence. **Design consequence stands either way:** a
prune/reap backstop is required; the hook alone cannot be the only reclamation path.

## B — in-place root pane replacement: NOT possible

There is **no** herdr command, CLI flag, or API method that replaces a workspace's root pane
in place. The API exposes 90 methods (`herdr api schema --json`, every
`properties.method.const`); the complete pane surface is:

```
pane.list get current layout process_info neighbor edges focus focus_direction resize zoom
pane.rename read split swap move close send_input send_keys send_text wait_for_output
pane.report_agent report_agent_session release_agent report_metadata clear_agent_authority
pane.graphics.set/info/clear
```

There is no `pane.create`, no `pane.replace`, no `pane.respawn`, and no workspace- or
tab-level "reopen root" verb. `herdr pane` CLI help lists the same set.

### Closest achievable behaviours, both measured

**1. Run the new command *in* the existing root pane — full identity preserved.**
`herdr pane run <pane_id> <command>` (API `pane.send_input`) types the command into the
pane's live shell:

```
$ herdr pane run w8G:p1 "echo S50-MARKER"
$ herdr pane read w8G:p1 --source visible --lines 20
newman@engine-03:~/magic/agentic-artifacts$ echo S50-IN-PLACE-MARKER
S50-IN-PLACE-MARKER
newman@engine-03:~/magic/agentic-artifacts$ echo S50-MARKER
S50-MARKER
```

`herdr pane get w8G:p1` before and after both report `pane_id w8G:p1`,
`tab_id w8G:t1`, `workspace_id w8G`, `terminal_id term_65afb56e3521663` — pane, tab,
workspace and terminal identity all survive. This is the only true in-place path, and it
requires the root pane to be sitting at an interactive shell prompt. It does not replace the
pane's process; it runs a child under the existing shell.

**2. Split then close the old pane — tab and workspace survive, pane id does not.**

```
ws=w8J root=w8J:p1 tab=w8J:t1
new pane=w8J:p2            # herdr pane split w8J:p1 --direction right
close root exit=0          # herdr pane close w8J:p1
--- after closing root ---
pane w8J:p2 tab w8J:t1 ws w8J
tab w8J:t1 '1'
```

Tab id `w8J:t1` and workspace id `w8J` are unchanged; only `pane_id` changes. **Tab identity
survives.**

**3. Closing the last pane destroys the workspace.** Continuing the same run:

```
--- now close the LAST pane ---
$ herdr workspace get w8J
{"error":{"code":"workspace_not_found","message":"workspace w8J not found"}}
```

So "close first, then open" is not available: there is no window in which the workspace
exists with zero panes.

### What this means for `space-create` / the s51 convert verb

`internal/cli/cmd_herdrspace.go:109` (`herdrWorkspaceCreate`) always calls
`workspace.create`, which is why the operator saw two workspaces. To convert an **existing**
worktree workspace in place, the ordering is forced:

1. `herdr pane split <root> --direction right` (or `--direction down`) to get the new pane,
   then `herdr pane close <root>` — tab and workspace ids survive, root pane id changes; or
2. `herdr pane run <root> "<attach command>"` — everything survives, but only if the root
   pane is at a shell prompt and you accept running under that shell.

Never `pane close` first. Never `workspace.create` for a convert.

## Evidence ledger

| Claim | Evidence |
|---|---|
| herdr version | `herdr --version` → `herdr 0.8.0`; binary `/home/newman/.local/bin/herdr` |
| herdr source unavailable | `github.com/miko-misa/herdr` and its API URL both HTTP 404 |
| `[[events]]` keys `on`/`command`/`platforms` | `herdr api schema --json` `/schemas/success_response/$defs/PluginManifestEventHook`; binary type name `RawPluginManifestEventHook` |
| 22 accepted / 5 rejected `on` values | probe manifest declaring all 27 subscription names; `warnings` read back from `herdr plugin link` |
| unknown `on` is non-fatal | two links, opposite outcomes (no `warnings` vs `["unknown event 'worktree_created'"]`), both exit 0 |
| envelope shape of `HERDR_PLUGIN_EVENT_JSON` | env dumped by a real hook fired on `workspace.created` |
| worktree payload fields | `/schemas/event/$defs/EventData/oneOf/8` and `/10`, plus `WorktreeInfo`, `WorkspaceInfo` |
| non-blocking, no retry, exit code logged | `sleep 3; exit 7` hook: create returned in 5 ms; one `plugin-log-39` row, `status failed`, `exit_code 7` |
| concurrent fan-out across plugins | historic `herdr plugin log list` rows 5 and 6, same `started_unix_ms` |
| no pane replace API | 90 `properties.method.const` values in the request schema; no `pane.create`/`pane.replace` |
| split+close keeps tab and workspace | live run on scratch workspace `w8J` |
| closing last pane destroys workspace | `workspace_not_found` for `w8J` immediately after |
| `worktree.removed` only on herdr-driven removal | nexus3 `plugins/herdr/bin/on-worktree-removed.sh:15-18` (inherited, MEDIUM) |

All probe plugins (`s50.probe-*`) were unlinked and all probe workspaces closed; `herdr
plugin list` and `herdr workspace list` were re-read afterwards to confirm no residue.

## Explicit UNKNOWNs

- Behaviour of a hook that hangs (kill? timeout? slot leak?) — not measured.
- The numeric value of the plugin command concurrency limit.
- Whether `HERDR_WORKSPACE_ID` is set for `worktree.removed` (the workspace is gone by then);
  only `workspace.created` was captured.
- Whether `worktree.removed` fires for a plain `git worktree remove` — inherited from nexus3,
  not re-proven here.
