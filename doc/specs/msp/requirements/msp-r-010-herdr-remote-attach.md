---
id: MSP-R-010
type: requirement
concept: C-MSP
title: Every plugin action is reachable under a remote attach without a custom keybinding
pattern: state
verification: verified
criticality: must
status: active
origin_decision_ref: nexus3-microsandbox-pivot#D-7
---

## MSP-R-010 — Every plugin action is reachable under a remote attach without a custom keybinding {#msp-r-010}

**Build state:** MET — verified 2026-09-08 by s10c-herdr-remote-attach.

**Evidence (s10c):** Laptop (macOS, 100.64.0.35) ran
`/Users/newman/.local/bin/herdr --remote newman@100.64.0.156 --session default`
in a tmux session, establishing a live remote attach to this linux host's herdr
server. While that attach was active, all three declared plugin actions were
invoked via `herdr plugin action invoke` on the server — exit\_code 0, status
"succeeded" for all three:
- `sandbox-list` (log-id plugin-log-29)
- `ports-declare` (log-id plugin-log-30)
- `ports-status` (log-id plugin-log-31)

No keybinding was used. The plugin ran on the linux server (actions execute
server-side under `--remote`; plugin is `platforms = ["linux"]`). The
[[link\_handlers]] entry (`local-port`) routes to `ports-declare` and requires
no keybinding. One caveat: the `herdr plugin action invoke` CLI calls were
issued from the server's own shell (same socket), not from a pane inside the
remote TUI — both paths use the same server socket, so the result is
equivalent.

**Verification gap — `ports-declare` exit\_code 0 is insufficient:** `ports-declare`
exits 0 regardless of whether it enqueued a Request. The s10c evidence proves
reachability (the action fired) but does not prove correctness (that a Request
entry appeared in `requests.json`). A future reverification pass must assert that
`requests.json` gained an entry after the invoke, not merely that the process
exited 0.

**While** herdr is attached with `--remote`, the plugin **shall** make every
action it declares reachable **without** a custom keybinding — that is, through
`[[link_handlers]]` or through `herdr plugin action invoke` — and **shall** be
installable and drivable in that configuration.

- **Why** — Under a default `--remote` attach no custom keybinding fires, so an
  action reachable only by keybinding is an action the operator cannot run at all;
  and plugin commands execute where the herdr **server** runs, which under
  `--remote` is the remote host, so an action that assumes it can open a laptop
  port or reach the laptop browser silently does nothing. The remote attach is the
  configuration the product is actually used in, so an action that works only
  under a local attach fails in the only case that matters.
- **Fit criterion** — Live, against a real herdr binary under a real `--remote`
  attach: the plugin installs, every declared action is invoked successfully via
  `herdr plugin action invoke` or a `[[link_handlers]]` match, and no action
  requires a keybinding to reach. Any action that must reach the operator does so
  via `herdr plugin pane open`, since action stdout and stderr are pipes.
- **Verification**: verified — manual-live, 2026-09-08, by s10c-herdr-remote-attach.
  Method: laptop (macOS, 100.64.0.35) ran
  `/Users/newman/.local/bin/herdr --remote newman@100.64.0.156 --session default`
  in a tmux session; all three declared actions invoked via
  `herdr plugin action invoke` on the server while the attach was live —
  exit\_code 0 for each. No automated test exists; an automated test is a
  remaining gap. Caveat: the CLI invocations originated from the server's own
  shell, not from a pane inside the remote TUI — both paths use the same server
  socket, so the verification is sound, but pane-origin invocation has not been
  separately confirmed.
- **Criticality**: must
- **Confidence caveat, carried deliberately** — Every ABI fact this node rests
  on (`herdr-plugin.toml` manifest keys, the remote-execution claim, the
  no-keybinding claim, `HERDR_PLUGIN_CONTEXT_JSON`, `HERDR_BIN_PATH`, and that
  `[[events]]` cannot hook terminal output) is **MEDIUM confidence and
  second-hand**: it comes from herdr-portfwd's documentation, verified by its
  author against Herdr 0.8.0 but **not** against Herdr source. Charter TBR-1
  tracks confirming it. The fit criterion is written against observable behaviour
  rather than against the manifest keys, so a wrong ABI detail fails the test
  instead of quietly invalidating the node.
- **Anchor** — Target constructs: manifest at `herdr-plugin.toml` (repo root);
  action entry points at `internal/cli/cmd_herdr_plugin.go:463` (`declare`),
  `:465` (`status`), `:467` (`list`), dispatched from `internal/cli/run.go:24`.
