# portfwd state file

## Location and directory choice

`forwards.state` lives in the laptop-side state directory returned by `StateDir()`:
`$XDG_STATE_HOME/herdr-plugin-msb/` (defaulting to `~/.local/state/herdr-plugin-msb/`).

This directory is a normal local filesystem directory, not a single-file bind mount.
Atomic temp-file+rename is safe here. The known EBUSY hazard (recorded in project MEMORY)
affects single-file bind mounts where the rename target is the mounted inode; that
scenario cannot arise for this directory because the whole directory — not a single file
within it — would need to be the mount target, and `StateDir()` is never configured that
way.

The temp file is `forwards.state.tmp`, written then renamed into `forwards.state` in the
same directory, so the rename is always same-filesystem.

## Format

JSON object: `written_by` (string), `updated_at` (RFC3339 time), `forwards` (array of
port-forward objects). Each forward: `port`, `sandbox`, `status`, `confirmed_at`, `error`.

Status values: `idle` `live` `pending` `dead` `error` `expired` `out_of_range`.

## Staleness

The pane checks `now.Sub(state.UpdatedAt) > 5m` (constant `paneStaleThreshold` in
`cmd_ports_pane.go:26`). If the laptop agent stops writing, the pane renders a WARNING
after five minutes. No additional staleness mechanism exists — the `UpdatedAt` field IS
the staleness signal.

## DEAD state — PROVISIONAL (pending OQ-3)

The `dead` status is expressible in the state type and rendered by the pane, but no
production code currently transitions a forward to `dead`. The transition would require
confirming that an ssh forward that reports alive via `ssh -O check` is actually passing
traffic, which depends on open question OQ-3 (unresolved; requires the operator to sleep
and wake their laptop to reproduce). Until OQ-3 is resolved and a reliable liveness probe
is identified, any code that writes `Status: "dead"` must be treated as provisional and
must not claim the forward is non-functional solely on the basis of `ssh -O check` exit 0.
