# The import ban

`make vet` runs `go run ./tools/importban .` after `go vet`. The checker parses
the imports of every `.go` file in the repository and exits non-zero if either
rule below is broken, printing one `IMPORT-BAN:` line per violation.

## Rule 1 — only `internal/runtime/msb/` may import the microsandbox SDK

Banned for every other package: `github.com/superradcompany/microsandbox` and
any subpackage of it, including `github.com/superradcompany/microsandbox/sdk/go`.

microsandbox has no plugin surface. One was built upstream — roughly 3,600
lines — and then deleted, because closures cannot cross its fork+exec boundary.
That is not a gap waiting to be filled; it is a structural property of how the
runtime spawns work. This project is therefore permanently an *outside* wrapper
of microsandbox, and will never be a plugin inside it.

The consequence for layout is that all SDK contact must sit in one package with
exactly one blast site. microsandbox is a beta dependency shipping breaking
changes roughly twice weekly (see `doc/dependency-pin.md`), so the question is
not whether the SDK surface will move but how many files have to be read when
it does. One package is the answer, and a toolchain check is what keeps it to
one package.

## Rule 2 — nothing under `internal/core/` may import a runtime implementation

Banned for every package under `internal/core/`: anything under
`github.com/IniZio/herdr-plugin-msb/internal/runtime/`.

`internal/core/runtime` is the substrate-agnostic seam — a single `Runtime`
interface and its companion types, importing nothing but the standard library.
A package under `internal/core/` that imports an implementation inverts that
relationship and makes the seam decorative.

## Why a toolchain check and not a review convention

This project's predecessor has a documented history of exactly two failure
shapes that a reviewer does not catch and a gate does:

- a parameter threaded correctly through every signature and left wired to the
  off value at every call site, so the feature never fired;
- assertions that outlived the mechanism they were written to test, staying
  green while the thing under test had been removed.

Both are invisible in a diff and obvious to a checker. A convention that the
SDK "should" stay in one package is a convention that decays silently. An
exit code does not.

## The checker was green over a live violation once

The first version of the checker passed all five of its unit tests and reported
`make vet` green while `internal/core/service` imported the microsandbox SDK.
The unit tests all passed an absolute `t.TempDir()` root; `make vet` passes `.`,
whose `filepath.WalkDir` root entry has `Name() == "."`, which matched the
skip-dot-directories rule and `SkipDir`ed the entire repository. The check
inspected nothing and exited 0.

It was caught by running the negative case against the real `make vet`, not by
the test suite. `tools/importban/relroot_test.go` now regresses it, and that
test has been observed failing against the unfixed checker. This is why the
acceptance criterion for this slice demanded a demonstrated failure rather than
a demonstrated pass.

## The checker can fail

Both rules have been observed failing. See the slice report for the verbatim
`make vet` output of each negative case, and `tools/importban/checker_test.go`
for the unit cases, which cover a clean tree, both violations, the permitted
SDK import site, and a runtime import from outside `internal/core/`.
