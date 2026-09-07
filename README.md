# herdr-plugin-msb

A sandbox runtime for herdr built on [microsandbox](https://github.com/superradcompany/microsandbox),
a Rust microVM runtime on libkrun. This is a clean-room rebuild of the substrate
layer that `nexus3` previously implemented itself on top of cloud-hypervisor.

The microsandbox runtime is driven **in process through its native Go FFI SDK**,
`github.com/superradcompany/microsandbox/sdk/go`, not by shelling out to the
`msb` binary. The SDK exposes `ExecStream`/`ExecHandle` and `Modify`, which a
subprocess wrapper would have to re-invent over stdio.

## Layout

| Path | Role |
|---|---|
| `cmd/herdr-plugin-msb/` | CLI entry point |
| `internal/core/runtime/` | The seam. A single `Runtime` interface plus its companion types. Substrate-agnostic; imports no runtime implementation. |
| `internal/runtime/msb/` | The **only** package permitted to import the microsandbox SDK. |
| `tools/importban/` | Toolchain-enforced import ban, run by `make vet`. |
| `doc/` | Rationale for the decisions the toolchain enforces. |

## Why the import ban exists

microsandbox has no plugin surface. One was built and then deleted upstream
because closures cannot cross its fork+exec boundary, so this project is
permanently an outside wrapper. All SDK contact therefore sits in one package
with exactly one blast site, and `make vet` fails if any other package reaches
for the SDK — or if anything under `internal/core/` imports a runtime
implementation and inverts the seam. See `doc/import-ban.md`.

## Building and testing

    make build       # produces the ./herdr-plugin-msb binary
    make typecheck   # type-checks every package, produces nothing
    make vet         # go vet + the import ban
    make test        # race-enabled tests, cache defeated
    make install     # atomic-rename install into ~/.local/bin

`make build` names an explicit output binary on purpose. Its predecessor ran
`go build ./...`, which type-checks every package and discards the results, so
it never wrote a binary — and the stale one left in the repo root got installed
instead, silently shipping old code. `make install` uses an atomic rename
because a plain `cp` over a live path fails with `Text file busy`.

Every build and test target runs inside a memory-bounded cgroup and never
degrades to a bare `go build ./...` or `go test ./...`. That is a memory-safety
rule, not a style preference: an unbounded race-enabled run at `GOMAXPROCS` has
repeatedly exhausted host RAM and tripped the *global* OOM killer, which takes
out `dbus` and `ssh-agent` and with them the whole login session. See
`doc/memory-guards.md`.

## The microsandbox version pin

microsandbox is a beta dependency shipping breaking changes roughly twice
weekly, so the version in `go.mod` is pinned exactly and asserted by a test —
an ambient upgrade cannot land silently. See `doc/dependency-pin.md`.
