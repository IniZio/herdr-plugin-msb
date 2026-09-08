# CLI verb set

CLI parity with nexus3 is dropped. The nexus3 verb registry (37 registered
verbs, 35 visible, 2 hidden) is a reference list, not a target. This repo ships the
minimum surface that satisfies its use cases. Every verb absent from the shipped set is
named below with a specific reason, so a forgotten verb is distinguishable from a
deliberate decline.

---

## Shipped verbs

### Sandbox verbs

| Verb | What it does | Why it ships |
|---|---|---|
| `create` | Create and boot a sandbox; mounts the worktree read-write and the dedicated Claude credential store read-write | The fundamental lifecycle primitive; without it nothing starts |
| `ps` | List sandboxes from the runtime | Operators need to see what is running; the name follows the nexus3 convention |
| `exec` | Run a command inside a named sandbox; propagates the guest's exit code | Distinguishing a failing guest command from a CLI error requires the guest exit code; exec is the only general-purpose execution path |
| `start` | Start a stopped sandbox | Needed to restart a sandbox that was stopped without removing it |
| `stop` | Stop a running sandbox | Graceful shutdown without destroying state |
| `rm` | Remove a sandbox | Cleanup; without it sandboxes accumulate |

### Plugin verbs (shipped by an earlier slice)

| Verb | What it does | Why it ships |
|---|---|---|
| `declare` | Declare guest ports for forwarding | Port numbers must be declared before a session starts because microsandbox v0.6.17 has no post-hoc port-publish surface; a recreate destroys all in-guest state |
| `status` | Show port-forward status | Operators need to know which forwards are active |
| `list` | List the pending port-forward declaration queue | `list` is the port queue; `ps` is sandboxes — the names are not interchangeable |
| `local-agent` | Run the local port-forward agent loop | The agent loop is the mechanism that applies declared ports via SSH forwarding; it cannot be inlined into another verb without making the CLI process permanent |

### Space verbs

| Verb | What it does | Why it ships |
|---|---|---|
| `space-create` | Create a sandbox and open a guest pane in a new herdr workspace | First-class space creation from the CLI |
| `space-convert` | Convert an existing worktree-backed herdr workspace to an msb sandbox space | Allows adopting an existing workspace without recreating it |
| `space-open-pane` | Open a guest pane inside an existing space | Re-attach after a detach without destroying and recreating the sandbox |
| `new-tab` | Open a new guest tab inside the current space, or a host tab elsewhere | Uniform new-tab UX regardless of context |
| `space-prune` | Survey (dry run by default) or reclaim (`--apply`) msb sandboxes whose bound herdr workspace or worktree checkout is gone; `--apply` without `--workspace` requires `--all` | Prevents stale binding accumulation after workspace or worktree removal; fail-safe design guards against accidental mass-reclaim |

### Other

| Verb | What it does | Why it ships |
|---|---|---|
| `version` | Print the binary's own identity: VCS revision, build time, dirty flag, read from embedded build info | A gate run against a stale binary is indistinguishable from a correct run without this; the dirty flag is needed during development |
| `help` | Usage | Standard |

---

## Declined, by name

**Authoritative count: 37 registered nexus3 verbs — 35 visible + 2 hidden.** The motive
charter cited 29 in an early estimate (before s06's inventory); that figure is superseded
by s06's measured result. The nexus3 CLAUDE.md itself cites 37. The spec's "Visible verbs"
section lists 35. The remaining 2 are hidden verbs (`__herdr-plugin` and `herdr`, both
registered with `Hidden: true` for plugin-private use). All 35 visible nexus3 verbs are
accounted for below.

| nexus3 verb | Disposition | Reason |
|---|---|---|
| `attach` | Declined | microsandbox v0.6.17 exposes no attach/TTY-handoff API; `exec` with an interactive command covers the interactive use case |
| `auth` | Declined | Credentials are delivered into the guest by a live read-write host bind mount at boot time (`internal/core/credmount/`); there is no OAuth flow to drive from the CLI |
| `config-ssh` | Declined | No SSH surface exists in microsandbox guests; the runtime does not install or configure an SSH daemon |
| `cp` | Declined | No file-copy API in microsandbox v0.6.17; operators use `exec` with `tar` or declare mounts at create time |
| `create` | **Adopted** | Name and core semantics adopted; differs from nexus3 in that it mounts the worktree and dedicated credential store — nexus3 `create` required separate `auth` and volume management steps |
| `doctor` | Declined | Scope decision; no self-diagnostic surface is defined for this repo |
| `egress` | Declined | The egress profile is a fixed, shipped configuration (`netprofile.Shipped()`) applied at one place in the adapter (`SandboxOptions`); it is not operator-tunable at runtime, so a verb to inspect or mutate it would be misleading |
| `exec` | **Adopted** | Name and semantics adopted; the shipped `exec` explicitly propagates the guest's exit code, which is the same behaviour the nexus3 spec required |
| `fork` | Declined | Snapshot-based fork was marked out-of-scope in the nexus3 spec itself |
| `forward` | Declined | Port forwarding is covered by the `declare` / `status` / `local-agent` plugin verbs; a separate `forward` verb would duplicate that surface |
| `harvest` | Declined | Artifact collection from a guest is accomplished through `exec` and standard UNIX tools; no dedicated harvest path is defined |
| `image` | Declined | No image build, push, or pull surface; microsandbox images are managed out-of-band by the operator, not by this plugin |
| `log` | Declined | No log-streaming API in microsandbox v0.6.17; guest stdout/stderr reaches the caller through `exec` |
| `ls` | Declined | nexus3 `ls` was a redundant alias for `ps`; only `ps` is adopted here; a second name for the same list operation adds surface for no gain |
| `mcp` | Declined | This repo is itself a herdr plugin that runs as an MCP server; there is no separate MCP sub-process to manage |
| `orca` | Declined | The nexus3 `orca` verb drove orca-specific workspace provisioning; orca integration is not applicable to this repo |
| `pause` | Declined | microsandbox v0.6.17 has no pause API; the adapter returns `ErrUnsupported`; advertising a verb that always fails is worse than omitting it |
| `ps` | **Adopted** | Name and semantics adopted; nexus3 `ps` delegated to `sandbox list` and was equivalent to `ls`; here `ps` is the canonical list verb and `ls` is declined as a redundant alias |
| `reap` | Declined | nexus3 `reap` cleaned up orphaned per-sandbox supervisor processes and dangling lock files; this repo has no supervisor process model — there is no per-sandbox supervisor to reap |
| `recover` | Declined | nexus3 `recover` recovered a sandbox whose supervisor had failed; no supervisor exists here, so there is nothing to recover |
| `restore` | Declined | Snapshot restore was marked out-of-scope in the nexus3 spec itself |
| `resume` | Declined | microsandbox v0.6.17 has no resume API; the adapter returns `ErrUnsupported` |
| `rm` | **Adopted** | Name and semantics adopted without change |
| `run` | Declined | `run` was a convenience composite (create → exec → remove) equivalent to `RunEphemeral`; composite operations belong in a higher-level orchestration layer, not on the primitive CLI surface (see `doc/mcp-tool-surface.md` named omissions) |
| `sandbox` | Declined | nexus3 `sandbox` was a dispatch group verb whose sub-commands (`sandbox create`, `sandbox list`, etc.) are shipped here as flat top-level verbs; the group wrapper is unnecessary |
| `sandbox agent-upgrade` | Declined | The nexus3 verb hot-swapped the in-guest Claude Code agent binary by pushing the current host binary over SSH; no SSH surface exists in this repo, and agent self-update is the agent's own responsibility |
| `shell` | Declined | An interactive shell requires SSH or a TTY-attach API; neither is available (no SSH server in guests, no attach API in microsandbox v0.6.17); `exec` with an interactive process is the substitute |
| `snapshot` | Declined | Snapshot creation was marked out-of-scope in the nexus3 spec itself |
| `ssh` | Declined | No SSH surface in microsandbox guests; the runtime does not install or configure an SSH daemon |
| `start` | **Adopted** | Name and semantics adopted without change |
| `stop` | **Adopted** | Name and semantics adopted without change |
| `supervisor-backfill-netns-identity` | Declined | No supervisor exists in this repo; additionally the nexus3 spec itself marked this verb out-of-scope ("CH-specific netns identity backfill not applicable to microsandbox") |
| `supervisor-upgrade` | Declined | No supervisor exists in this repo; there is no detached per-sandbox supervisor process to upgrade |
| `version` | **Adopted** | Name and semantics adopted; both repos print the binary's own build identity |
| `volume` | Declined | nexus3 `volume` managed a separate volume lifecycle; mounts in this repo are declared inline at `create` time as `SandboxSpec.Mounts` entries — no independent volume objects exist |

---

## How to re-check this list

The shipped verb set is pinned by a test in `internal/cli/cmd_sandbox_test.go` that
asserts the usage string's verb list exactly. Adding a verb without updating this
document will not automatically fail that test, but the test will fail if the usage
string changes and the test string is not updated — so adding a verb without also
updating the test fails the gate. Any discrepancy between this document and the test's
expected verb list is a bug.
