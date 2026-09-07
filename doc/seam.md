# The runtime seam — `internal/core/runtime`

This package is the seam between the project and whatever microVM runtime backs
it. It is the one door: callers hold a `Runtime` and nothing else. No file here
imports a runtime implementation or the microsandbox SDK, and `tools/importban`
enforces both halves of that at vet time (see `doc/import-ban.md`).

The rationale lives here rather than in Go doc comments because this repo caps
comment density at 5 lines per 100 in source files.

## `Runtime`

Ten methods, mirroring the sandbox surface this project replaces. Every method
takes a `context.Context` first and returns an `error` last.

    Create(ctx, spec SandboxSpec) (SandboxRef, error)
    CreateAndBoot(ctx, spec SandboxSpec) (SandboxRef, error)
    List(ctx) ([]SandboxRef, error)
    Start(ctx, ref SandboxRef) (SandboxRef, error)
    Stop(ctx, ref SandboxRef) (SandboxRef, error)
    Pause(ctx, ref SandboxRef) (SandboxRef, error)
    Resume(ctx, ref SandboxRef) (SandboxRef, error)
    Remove(ctx, ref SandboxRef) error
    Exec(ctx, ref SandboxRef, req ExecRequest) (ExecResult, error)
    RunEphemeral(ctx, spec SandboxSpec, req ExecRequest) (ExecResult, error)

## Companion types

| Type | Fields |
|---|---|
| `SandboxSpec` | `Project`, `Name`, `ImageRef`, `VCPUs`, `MemoryMiB`, `Motive`, `RemoveOnExit`, `Mounts`, `NetRules` |
| `SandboxRef` | `ID`, `Project`, `Name`, `Status` |
| `SandboxStatus` | `created`, `running`, `paused`, `stopped` |
| `ExecRequest` | `Argv`, `Env`, `Cwd`, `Stdin`, `Stdout`, `Stderr` |
| `ExecResult` | `ExitCode` |
| `Mount` | `HostPath`, `GuestPath`, `ReadOnly` |
| `NetRule` | `Action` (`allow`/`deny`), `Host`, `Port` |

`ExecRequest.Stdout` and `ExecRequest.Stderr` are `io.Writer` sinks. That is
deliberate: the microsandbox SDK exposes `ExecStream`/`ExecHandle`, so a
streaming implementation can be written against these types without changing
them. No streaming machinery is defined at the seam.

## Deliberately absent: rootfs path, memory size, nested virtualisation

Per decision D-9, three fields the predecessor's sandbox surface advertised —
`rootfs_path`, `memory_mib`, `nested_virt` — are dropped, and they appear
nowhere in this package in any spelling. All three are substrate detail that
leaked outward: a caller that names a rootfs image path has already committed
to one runtime's disk model. They are not omitted by oversight and must not be
added back. A backing runtime that needs any of them derives it inside its own
package.

That asymmetry is now settled, and the settlement narrows the paragraph above.
Decision D-9 constrains what the **MCP surface advertises** — `rootfs_path`,
`memory_mib` and `nested_virt` stay out of the MCP schema — not what the seam
may express. Keeping a CPU count while dropping a memory size was not a
principle, so `MemoryMiB` sits alongside `VCPUs` here. Both are applied by the
backing runtime and both are observed from inside a booted VM by
`internal/runtime/msb`'s resource live test; a resource field threaded through
a signature but never applied to the guest is the failure this pair of fields
is tested against. `rootfs_path` and `nested_virt` remain absent in every
spelling.

## Deleted: `ProxyEndpoint`

Decision D-12 removed the CONNECT-proxy credential design entirely; credentials
now reach a guest by volume mount. `ProxyEndpoint` therefore had no referent,
and a type nobody can populate is a trap for the next reader, so it is gone.
`internal/core/runtime`'s absence test fails if the identifier reappears.

## Backing runtime: microsandbox v0.6.17

`internal/runtime/msb` is the only package that may import the SDK. Four of its
mappings are not one-to-one and are worth stating:

- **`Create` boots.** The SDK has no create-without-boot call — `CreateSandbox`
  creates and boots — so `Create` boots and then stops, leaving a persisted,
  stopped sandbox that `Start` can bring back. `CreateAndBoot` passes
  `WithDetached()` and then calls `Detach`, never `Close`: on a detached
  sandbox `Close` stops the VM.
- **`Pause` and `Resume` are unsupported.** v0.6.17 exposes no pause or resume
  API in the SDK and no `msb pause` / `msb resume` verb; only the status value
  `paused` exists. Both methods return a wrapped `msb.ErrUnsupported` rather
  than a silent no-op or a stop-and-snapshot impersonation. `SandboxStatus`
  keeps `paused` because the runtime can still report it.
- **Names.** A microsandbox sandbox has one flat name, so `Project` and `Name`
  compose as `project--name`, and `Project`/`Motive` are also written as the
  labels `herdr.project` / `herdr.motive`, which is what `List` reads back.
- **Mounts.** A `Mount` becomes a bind mount with relaxed stat virtualisation
  and mirrored host permissions, which is what makes a mounted host git
  worktree writable from both sides.

## Standard library only

Everything in this package depends on the standard library and nothing else.
That is what makes the seam substrate-agnostic in fact rather than by
intention: a third-party import here would be a runtime detail with no place
to hide.
