# Test-session isolation

## The incident

Twice in one day, running `make test` from a shell inside a herdr workspace converted the
operator's primary workspace `w8` into a real sandbox: a live microVM, a binding record, and
64 "Microsandbox guest shell" panes — in the workspace where the operator's own Claude
session was running. The first occurrence killed that session.

Two independent defects combined.

**Defect 1 — the suite reached the live environment.** `TestShippedVerbRegistry` in
`internal/cli/cmd_sandbox_test.go` invoked `Run(ctx, []string{verb})` for every shipped verb,
unstubbed. That includes `space-convert`, whose `--workspace` flag defaults to
`$HERDR_WORKSPACE_ID` (`internal/cli/cmd_herdrspace_convert.go:183`). In a shell inside a
workspace that variable is a real workspace ID. The test suite mutated the machine it ran on.

**Defect 2 — the gate reported green over red.** The same loop invoked `default-shell`, whose
`runDefaultShell` called `syscall.Exec($SHELL)` (`internal/cli/cmd_herdrspace_default_shell.go`).
That *replaces the running test binary*. The replacement shell exited 0, so `go test` printed
`ok  internal/cli` for a package with failing tests, and every test declared after that point
never ran.

## Defence 1: a throwaway herdr session

`scripts/test-session.sh` wraps the test command. `herdr server` (bare, no subcommand) is a
headless server start, so the script creates a temp directory, starts a private server on a
socket inside it, runs the command against that server, and tears it down.

Redirected into the temp directory: `HERDR_SOCKET_PATH`, `XDG_CONFIG_HOME`, `XDG_STATE_HOME`
and `HOME`. Scrubbed entirely: `HERDR_WORKSPACE_ID`, `HERDR_PANE_ID`, `HERDR_TAB_ID`,
`HERDR_ENV`.

`XDG_STATE_HOME` is load-bearing beyond herdr itself: `StateDir` in
`internal/cli/cmd_herdr_plugin.go:70` reads it, and that is what locates
`herdr-space-bindings.json` — the file the incident corrupted. Scrubbing the `HERDR_*`
variables alone would still have let tests write the operator's real binding store.

Redirecting `HOME` would otherwise send the Go toolchain to a cold cache, so the script reads
`GOCACHE`, `GOMODCACHE` and `GOPATH` from `go env` *before* the redirect and re-exports the
real values after it.

Teardown is a `trap ... EXIT INT TERM HUP` installed before the server starts, so it runs on
success, on test failure, and on Ctrl-C. `make` does not run recipe cleanup on interrupt,
which is why the trap lives in a shell script rather than in the Makefile.

If `herdr` is absent or the server fails to come up, the script does **not** fall back to the
live environment. It falls back closed: the socket is pointed at a path that cannot exist, so
any test that reaches for herdr fails loudly (`server_not_running`, exit 1) instead of
silently finding the operator's session.

## Defence 2: two guards, in case a redirect is ever dropped

`scripts/test-session.sh` aborts before running anything if the environment it is about to
hand over could still name the default session — if the resolved `HERDR_SOCKET_PATH` is the
live socket, or `XDG_STATE_HOME` resolves to the real state root.

`internal/cli/main_test.go` repeats the check inside the test binary via `TestMain`, and
refuses to run a single test unless `HERDR_MSB_TEST_ISOLATED=1` is set, `HERDR_WORKSPACE_ID`
is unset, `HERDR_SOCKET_PATH` is set and is not a default-session socket, and
`XDG_STATE_HOME` is set. Running `go test ./internal/cli` directly therefore fails with an
explanatory message rather than running unprotected. Use `make test`.

## Defence 3: the exec cannot fire under test

Both `syscall.Exec` call sites in `runDefaultShell` now route through one package-level
`execProcess` indirection, whose default implementation refuses when `testing.Testing()` is
true and returns a non-zero status instead. A test binary can no longer be replaced, so a
failing package can no longer be reported as `ok`.

`default-shell` consequently returns exit 1 under `go test`. That is intended: the verb
genuinely cannot do its job from a test binary, and reporting failure is the honest outcome.

## Known residual risk

The harness isolates *herdr* state. It does not isolate *microsandbox* state — `msb` has no
socket or state-directory override in this harness, so a test that got far enough to create a
VM would create a real one. Nothing in the current suite does, because reaching that point
requires a workspace ID and the harness scrubs it. Do not rely on the harness to contain a
test that creates sandboxes directly.

## Verification

Both defences were proved with opposite-outcome runs; see the slice s64 report. The
load-bearing one: with a deliberately failing test present in `internal/cli`, `make test`
exits 2 and reports `FAIL internal/cli` with the guard in place, and exits 0 reporting
`ok internal/cli 0.028s` with the `testing.Testing()` refusal disabled — the same failing
test, invisible.
