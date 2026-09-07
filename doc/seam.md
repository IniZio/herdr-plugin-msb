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
| `SandboxSpec` | `Project`, `Name`, `ImageRef`, `VCPUs`, `Motive`, `RemoveOnExit`, `Mounts`, `NetRules` |
| `SandboxRef` | `ID`, `Project`, `Name`, `Status` |
| `SandboxStatus` | `created`, `running`, `paused`, `stopped` |
| `ExecRequest` | `Argv`, `Env`, `Cwd`, `Stdin`, `Stdout`, `Stderr` |
| `ExecResult` | `ExitCode` |
| `Mount` | `HostPath`, `GuestPath`, `ReadOnly` |
| `NetRule` | `Action` (`allow`/`deny`), `Host`, `Port` |
| `ProxyEndpoint` | `Host`, `Port` |

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

`VCPUs` is retained. Decision D-9 enumerated exactly three fields to drop and
this was not among them, but note the asymmetry — dropping a memory size while
keeping a CPU count is not obviously principled, and it is worth settling
before Wave 1 implementers build against it.

## Standard library only

Everything in this package depends on the standard library and nothing else.
That is what makes the seam substrate-agnostic in fact rather than by
intention: a third-party import here would be a runtime detail with no place
to hide.
