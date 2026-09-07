---
id: MSP-R-008
type: requirement
concept: C-MSP
title: The runtime seam is enforced by the toolchain, not by review
pattern: unwanted
verification: unverified
criticality: must
status: active
origin_decision_ref: nexus3-microsandbox-pivot#D-1
---

## MSP-R-008 — The runtime seam is enforced by the toolchain, not by review {#msp-r-008}

**Build state:** PARTIAL — the checker package `tools/importban` and its `make vet`
wiring exist and `make vet` exits 0 on this tree (run 2026-09-07). What is not
established is the prove-it-can-fail evidence required by
`groundwork:prove-the-check-can-fail`, and the second clause has no subject yet
because no package under `internal/core/` imports a runtime implementation to
be caught.

**If** any package outside `internal/runtime/msb/` imports the microsandbox SDK,
or any package under `internal/core/` imports a runtime implementation, **then**
`make vet` **shall** fail.

- **Why** — microsandbox has no plugin or hook surface: one existed upstream and
  was deleted, ~3,600 lines, because closures cannot cross its fork+exec
  boundary, so this project is permanently an outside wrapper of a beta
  dependency shipping breaking changes roughly twice weekly. Confining all SDK
  contact to one package gives an upstream shape change exactly one blast site.
  Review discipline is not an acceptable substitute here for a reason this
  codebase has measured rather than assumed: it has a recorded history of a
  threaded parameter left at the off value and of assertions outliving the
  mechanism that carried them, and a compiler-or-linter gate catches both classes
  while a reviewer does not.
- **Fit criterion** — `make vet` exits non-zero when a package outside
  `internal/runtime/msb/` adds an import of the microsandbox SDK, and again when
  a package under `internal/core/` adds an import of a runtime implementation.
  Both cases are demonstrated by planting the import and observing the failure,
  then removing it — a green run alone is not evidence, because a checker that
  inspects nothing also exits 0.
- **Verification**: unverified — the checker exists and `make vet` runs it, but no test carries a `// @verifies MSP-R-008` annotation and the prove-it-can-fail evidence has not been produced. Target method: automated.
- **Criticality**: must
- **Anchor** — `tools/importban` (package), invoked from the `vet` target of the
  repository `Makefile`. Rationale in `doc/import-ban.md`.
