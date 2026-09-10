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

## The startup hook must not resolve the agent through PATH

Option A below is implemented: `dist/` carries the cross-compiled agent and the
macOS `[[build]]` step copies it into the plugin root. That settles *delivery*,
but it did not settle *resolution*, and the two were conflated for several days.

`[[startup]]` declared `command = ["herdr-plugin-msb-agent", "local-agent-startup"]`.
herdr resolves a bare command name against `PATH`, exactly as it resolves a
relative path against the pane's `--cwd` rather than the plugin root. Nothing
ever put that binary on `PATH`: the macOS `[[build]]` step writes it to the
plugin root, and `make install` — which does install both binaries to
`~/.local/bin` — is never run on macOS, and had not been re-run on the Linux
engine since the agent binary was added. The hook died with exit 127 before
reaching any of its own code, on both platforms.

Nothing reported this. `herdr plugin log --plugin herdr-plugin-msb` held zero
entries on the engine, which reads identically to "no startup has occurred yet".
The failure was first seen only when an operator typed the bare name in a shell
and got `command not found`.

The fix is the form the shell pane in the same manifest already used:

    command = ["sh", "-c", 'exec "$HERDR_PLUGIN_ROOT/herdr-plugin-msb-agent" local-agent-startup']

`HERDR_PLUGIN_ROOT` is set for startup hooks — `startup.go` and `agentrun.go`
already read it to locate the binaries they ship to an engine — so this needs no
`PATH` contribution from the operator on either platform.

Proven by two runs with opposite outcomes: with `HERDR_PLUGIN_ROOT` unset, the
bare name exits 127 `not found`; the `$HERDR_PLUGIN_ROOT` form exits 0.
`TestManifestStartupCommandDoesNotDependOnPATH` guards the manifest, and was
mutation-proven — RED against the old command line, GREEN against the new one.
Both runs went through `make test`: run directly, `go test ./internal/cli/`
aborts on the suite's isolation guard, and a naive RED/GREEN pair both "fail"
for that reason while asserting nothing.

## No-Go-on-Mac: the distribution options as they were weighed

`[[build]]` now carries `platforms = ["linux"]`. herdr will not run `make build`
on macOS, eliminating the Go toolchain requirement there.

The binary must therefore arrive another way. Three options:

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

Option A is implemented, so no manual build is required on the Mac.

## What remains unproven

End-to-end: the operator's Mac Local server is not running. The `provision`
command and `EnsureProvisioned` are unit-tested with injected SSH runners but
have not run against a live engine.

The `platforms = ["linux"]` guard on `[[build]]`: untested on macOS. If herdr
ignores unknown keys on `[[build]]`, `make build` would still run there.

`herdr plugin link` surface on the engine: assumed identical to the Mac surface.
Not verified against an actual engine herdr version.
