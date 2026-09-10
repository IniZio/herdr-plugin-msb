# single-install bootstrap — design rationale

## Problem

herdr does not propagate plugins to SSH hosts. The operator installs the plugin
once on their Mac; the `[[startup]]` hook fires
`herdr-plugin-msb-agent local-agent-startup`, which discovers enabled herdr
machines via `herdr machine list --json`. Each machine is a Linux engine where
`herdr-plugin-msb` must also be installed for sandbox verbs (`space-convert`,
`list`, etc.) and for the port-forward queue to exist.

## Mechanism

`local-agent-startup` already holds the SSH target for every enabled machine.
Before spawning per-target port-forward agents it calls `EnsureProvisioned` for
each. Provisioning transfers two files and runs one command on the engine:

1. `~/.local/bin/herdr-plugin-msb` — Linux binary (scp)
2. `~/.config/herdr/plugins/herdr-plugin-msb/herdr-plugin.toml` — plugin
   descriptor (scp)
3. `herdr plugin link ~/.config/herdr/plugins/herdr-plugin-msb` — registers the
   plugin with herdr on the engine (ssh exec)
4. `echo <version> > ~/.local/share/herdr-plugin-msb/version` — version marker
   for idempotency (ssh exec, best-effort; errors ignored)

The binary source on the client side is `$HERDR_PLUGIN_ROOT/herdr-plugin-msb`.
`HERDR_PLUGIN_ROOT` is set by herdr when running a plugin hook.

## Idempotency

Before copying, `EnsureProvisioned` reads
`~/.local/share/herdr-plugin-msb/version` via ssh. If it matches `PluginVersion`
(the local constant in `state.go`), provisioning is skipped entirely. Two calls
with the same version produce zero additional SSH writes on the second call.

Tested in `TestProvisionIdempotentSameVersion`: negative control confirmed
`Copy` IS called when the remote version differs.

## Version awareness

If the remote marker differs from the local version, provisioning runs. If the
remote version is non-empty and different, a line is written to stderr:

    provision: VERSION MISMATCH on <target>: local=X engine=Y — reprovisioning

A silently mismatched pair is the failure mode this repo keeps hitting.

Tested in `TestProvisionVersionMismatch`: negative control confirmed stderr does
NOT contain `VERSION MISMATCH` when versions match.

## Consent

Provisioning writes to a remote host on the operator's behalf. Explicit per-machine
consent is required before `local-agent-startup` will provision.

**Recording consent**: `herdr-plugin-msb-agent provision --target user@host`
shows the exact plan and prompts `[y/N]`. On `y`, a record is written to
`~/.local/state/herdr-plugin-msb/consent/<sanitized-target>.json` containing
the target, timestamp, and version.

**Revocation**: `herdr-plugin-msb-agent provision --revoke --target user@host`
deletes the record. After revocation, `local-agent-startup` silently skips
provisioning for that machine. The engine-side plugin is NOT uninstalled.

**During startup**: absent consent record → silent skip (non-fatal). Any other
provisioning error → logged to stderr, non-fatal; the port-forward agent is
still spawned.

Tested in `TestProvisionNoConsent`: negative control confirmed Copy IS called
when consent is given.

## No-Go-on-Mac: open distribution question

`[[build]]` now carries `platforms = ["linux"]`. herdr will not run `make build`
on macOS, eliminating the Go toolchain requirement there.

However, `[[startup]]` on macOS still requires `herdr-plugin-msb-agent` in PATH.
Without `make build` running on macOS, the binary must arrive another way. Three
options:

**A. Commit prebuilt binaries in the repo** (recommended for now): cross-compile
`herdr-plugin-msb-agent` for `darwin/arm64` and `darwin/amd64` at release time
and commit them under `dist/`. Add a macOS `[[build]]` step that copies the
appropriate prebuilt. Binary is reproducible from source; bloat is ~3.5 MB.
Smallest-surface option, no external infrastructure.

**B. GitHub/CDN releases**: CI publishes releases; macOS `[[build]]` fetches.
Requires a release workflow, stable URL, and network access at install time.

**C. Require Go on Mac**: remove the `platforms = ["linux"]` guard. Simplest
code path; violates the operator constraint and is ruled out.

`EnsureProvisioned` also needs a Linux `herdr-plugin-msb` binary at
`$HERDR_PLUGIN_ROOT/herdr-plugin-msb`. On a Mac-only install without option A
or B, this path will not exist and provisioning returns `ErrNoSourceBinary`.
Option A (include the Linux binary in `dist/`) resolves this too.

Until option A or B is implemented, the Mac operator must manually install
`herdr-plugin-msb-agent`, for example:

    GOOS=darwin GOARCH=arm64 go build -o herdr-plugin-msb-agent ./cmd/herdr-plugin-msb-agent

## What remains unproven

End-to-end: the operator's Mac Local server is not running. The `provision`
command and `EnsureProvisioned` are unit-tested with injected SSH runners but
have not run against a live engine.

The `platforms = ["linux"]` guard on `[[build]]`: untested on macOS. If herdr
ignores unknown keys on `[[build]]`, `make build` would still run there.

`herdr plugin link` surface on the engine: assumed identical to the Mac surface.
Not verified against an actual engine herdr version.
