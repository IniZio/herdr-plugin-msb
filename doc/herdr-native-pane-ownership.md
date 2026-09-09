# herdr native-pane ownership

How this plugin ensures that panes created through herdr's *native* paths
(tab-bar button, prefix+c, splits, initial workspace pane) land inside the
guest when the workspace is sandbox-bound, not on the host.

---

## 1. The defect

`herdr-plugin.toml` declares one route for guest pane creation:

```toml
[[actions]]
id = "new-tab"
title = "New Tab (guest pane in msb spaces, host tab elsewhere)"
command = ["herdr-plugin-msb", "new-tab"]
contexts = ["workspace"]
```

Citation: `/home/newman/magic/herdr-plugin-msb/herdr-plugin.toml`, the sole
`[[actions]]` entry with `id = "new-tab"`.

An `[[actions]]` entry is reachable only through herdr's plugin action menu.
Herdr's *native* new-tab path — the tab-bar `+` button, the default `prefix+c`
binding, pane splits, and the initial pane opened when a workspace is created —
does not consult the plugin at all.  Each of those paths starts a process using
herdr's configured `default_shell` (or `$SHELL` if none is set), which is
always a host process.

---

## 2. What herdr 0.8.0 offers

All commands below were run against the installed binary.

```
$ herdr --version
herdr 0.8.0
```

### 2a. No `--command` flag on new-tab or split

```
$ herdr tab create --help
Create a tab

Usage: herdr tab create [OPTIONS]

Options:
      --workspace <WORKSPACE_ID>
      --cwd <PATH>
      --label <TEXT>
      --env <KEY=VALUE>     Set an environment variable for the launched process
      --focus
      --no-focus
```

```
$ herdr pane split --help
Split a pane

Usage: herdr pane split [OPTIONS] [PANE_ID]

Options:
      --pane <ID>
      --current
      --direction <DIRECTION>   [possible values: right, down]
      --ratio <FLOAT>
      --cwd <PATH>
      --env <KEY=VALUE>     Set an environment variable for the launched process
      --focus
      --no-focus
```

Neither `herdr tab create` nor `herdr pane split` accepts a `--command` flag.
The process started in any new pane is always `default_shell` or `$SHELL`.
There is no per-workspace override.

### 2b. No per-workspace default-shell field

`herdr api schema --json` lists `WorkspaceInfo` in
`/schemas/event/$defs/WorkspaceInfo` with required fields `workspace_id`,
`number`, `label`, `focused`, `pane_count`, `tab_count`, `active_tab_id`,
`agent_status` and optional `worktree`, `tokens`.  No `default_shell` or
`default_command` field exists at workspace, tab, or pane level in any schema
variant.

### 2c. `tab.created` and `pane.created` fire after the host shell starts

Both event names are accepted by the plugin-hook registry (measured, both
outcomes verified):

```
ACCEPTED (22): pane.agent_detected pane.agent_status_changed pane.closed
               pane.created pane.exited pane.focused pane.moved tab.closed
               tab.created tab.focused tab.moved tab.renamed workspace.closed
               workspace.created workspace.focused workspace.moved
               workspace.renamed workspace.reordered workspace.updated
               worktree.created worktree.opened worktree.removed
```

Citation: `/home/newman/magic/herdr-plugin-msb/doc/herdr-event-contract.md`,
section "A1 — Accepted `on` values — measured, with both outcomes".

However, both events fire *after* herdr has already started a host-shell
process in the new pane.  The API exposes no `pane.replace` or `pane.respawn`
verb — see the complete pane surface in
`/home/newman/magic/herdr-plugin-msb/doc/herdr-event-contract.md` section
"B — in-place root pane replacement: NOT possible".  A post-hoc event handler
would need to kill the host shell and exec a guest shell into the same pane
identity, which is not possible.  Any workaround (close old pane, split a new
one) produces a visible flash and a changed pane ID.

**Ruled out.** Post-hoc `tab.created` / `pane.created` handlers cannot satisfy
"every pane-creation path lands in the guest" without visible artefacts.

---

## 3. The two mechanisms that do work

Both are needed to cover all pane-creation paths.

### Mechanism A — keybinding override

`[keys] new_tab = ""` suppresses herdr's built-in new-tab action, and a
`[[keys.command]]` entry with `type = "plugin_action"` delegates to the
plugin's `new-tab` action instead.  This covers only the keybinding path
(`prefix+c` by default).

### Mechanism B — `[terminal] default_shell` wrapper

Setting herdr's global `default_shell` to a workspace-aware wrapper binary
covers *every* pane-creation path: keybinding, tab-bar `+` button, splits,
and the initial pane of a new workspace.  The wrapper checks
`$HERDR_WORKSPACE_ID` against the plugin's binding store (the same store
`herdr-plugin-msb` already maintains for `new-tab`).  If the workspace is
sandbox-bound, the wrapper exec-replaces itself with the guest shell command;
otherwise it exec-replaces itself with the host `$SHELL`.

This is the mechanism that satisfies the requirement: every pane-creation path
lands in the guest for sandbox-bound workspaces, with no plugin-menu
detour required.

### Operator wiring — copy-pasteable config.toml stanzas

Apply these stanzas to `~/.config/herdr/config.toml` after installing the
wrapper binary (verb: `herdr-plugin-msb default-shell --install`, which writes
the binary to `~/.local/bin/herdr-msb-default-shell`):

```toml
[terminal]
default_shell = "/home/newman/.local/bin/herdr-msb-default-shell"

[keys]
new_tab = ""

[[keys.command]]
key = "prefix+c"
type = "plugin_action"
command = "herdr-plugin-msb.new-tab"
description = "New tab (guest pane in msb spaces, host tab elsewhere)"
```

`herdr-plugin-msb.new-tab` is the `<plugin_id>.<action_id>` form required by
herdr's `[[keys.command]]` with `type = "plugin_action"` — plugin id
`herdr-plugin-msb` from `herdr-plugin.toml:1`, action id `new-tab` from the
`[[actions]]` entry cited in section 1.

Mechanism A alone is insufficient: it does not cover the tab-bar `+` button,
splits, or new workspaces.  Mechanism B alone is sufficient for coverage but
leaves the keybinding going through `default_shell` rather than the plugin's
richer `new-tab` dispatch (which falls back gracefully for unbound workspaces).
Using both is the correct posture.

---

## 4. The safety property

Because `default_shell` is herdr's global fallback, the wrapper becomes the
process launched for *every* new pane on the machine.  Any code path that exits
non-zero before exec-ing a shell freezes that pane permanently.

Two invariants the wrapper must satisfy:

**Fallback on all errors.** If the binding-store lookup fails, if
`$HERDR_WORKSPACE_ID` is absent, or if any other precondition is not met, the
wrapper must exec `$SHELL` immediately.  A non-zero exit is never acceptable
— it must always produce an interactive shell.

**Sentinel to prevent infinite recursion.** If the guest-entry path itself
invokes `default_shell` (e.g. through a nested herdr session or an exec call
that resolves back to the wrapper), the wrapper will re-enter.  Guard with an
env var — analogous to `NEXUS3_HOST_SHELL=1` in the predecessor (see section 5)
— that the wrapper checks on startup and, if set, skips the binding lookup and
exec-replaces with `$SHELL` immediately.  The variable for this plugin is
`HERDR_MSB_HOST_SHELL`.

---

## 5. Prior art in nexus3

`/home/newman/magic/nexus3` is read-only reference material.  It shipped both
mechanisms:

- **Mechanism A** (keybinding): `new_tab = ""` plus a `[[keys.command]]` block
  with `command = "nexus3.new-tab"` — visible in the host config (section 6).
- **Mechanism B** (default-shell): `nexus3 herdr install-default-shell`
  hard-linked the nexus3 binary to
  `~/.local/bin/nexus3-guest-shell` and emitted the
  `[terminal] default_shell = "/home/newman/.local/bin/nexus3-guest-shell"`
  stanza.  Implementation:
  `/home/newman/magic/nexus3/internal/cli/cmd_herdr_default_shell.go`
  (function `runHerdrInstallDefaultShell`; the sentinel var is
  `NEXUS3_HOST_SHELL`).

nexus3 declared **10 `[[panes]]`** and **13 `[[actions]]`** in its plugin
manifest at
`/home/newman/magic/nexus3/plugins/herdr/herdr-plugin.toml`
(verified: `grep -c '^\[\[panes\]\]'` → 10, `grep -c '^\[\[actions\]\]'` → 13).
This plugin currently declares **2 `[[panes]]`** and **5 `[[actions]]`**
(verified: counted from
`/home/newman/magic/herdr-plugin-msb/herdr-plugin.toml`).

---

## 6. Host-config conflict — current state

Contents of `~/.config/herdr/config.toml` as of the time this document was
written (read-only):

```toml
default_shell = "/home/newman/.local/bin/nexus3-guest-shell"

[experimental]
allow_nested = true

[keys]
new_tab = ""

[[keys.command]]
key = "prefix+p"
type = "plugin_action"
command = "jt.command-palette.open"
description = "Command palette"

[[keys.command]]
key = "prefix+c"
type = "plugin_action"
command = "nexus3.new-tab"
description = "New tab (guest pane in nexus3 spaces)"
```

Both mechanisms are already installed — but they point at nexus3, not this
plugin.

- `[terminal] default_shell` → `nexus3-guest-shell` (nexus3's Mechanism B binary)
- `[keys] new_tab = ""` + `[[keys.command]] command = "nexus3.new-tab"` (nexus3's Mechanism A)

Adopting Mechanism B for this plugin means retargeting `default_shell` to
`herdr-msb-default-shell`.  This is an operator-visible change to a shared host
config that the predecessor project installed.  An operator must decide whether
to replace nexus3's entry or run both plugins concurrently (which requires the
two default-shell wrappers to chain, an arrangement not yet designed here).

The keybinding wiring (`new_tab = ""` plus the `[[keys.command]]` block) also
needs to be retargeted from `nexus3.new-tab` to `herdr-plugin-msb.new-tab`, or
a second `[[keys.command]]` entry added if both plugins must coexist on the same
keybinding — herdr's behaviour for duplicate key entries is not verified here.
