---
id: MSP-R-010
type: requirement
concept: C-MSP
title: Every plugin action is reachable under a remote attach without a custom keybinding
pattern: state
verification: unverified
criticality: must
status: active
origin_decision_ref: nexus3-microsandbox-pivot#D-7
---

## MSP-R-010 — Every plugin action is reachable under a remote attach without a custom keybinding {#msp-r-010}

**Build state:** TARGET — obligation not met; fit criterion not run. No plugin
manifest exists in this repository.

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
- **Verification**: unverified — no test exists. Target method: automated live.
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
- **Anchor** — Target constructs: the `herdr-plugin.toml` manifest and the
  plugin action entry points. Not yet present.
