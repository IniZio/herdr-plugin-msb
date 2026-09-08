# herdr-plugin-msb

A thin [herdr](https://github.com/miko-misa/herdr) plugin that runs sandboxes on
**microsandbox** (`github.com/superradcompany/microsandbox`, a Rust microVM runtime on
libkrun), driven **in-process through its Go FFI SDK** — never by shelling out to `msb`.

This repo replaces `nexus3`. It is a clean-room rebuild, not a migration.

## Relationship to /home/newman/magic/nexus3

`nexus3` is **READ-ONLY reference material** and the fallback if this bet fails. Read it
freely; **do not modify it, and do not port code from it.** The whole point is fresh and
minimal — roughly 9,000 lines that a migration would have dragged across (`cred/` 2,290,
`mitm/` 1,608, `internal/core/config/`, `cmd_herdr_plugin.go` 4,673, `internal/mcp/`) were
deliberately left behind.

Useful things to *read* there: the CLI surface baseline at `doc/specs/cli-surface.md`
(37 verbs, 47 integration files with dispositions) — a **reference list, not a parity
target**, since CLI parity was explicitly dropped.

## Always build and test through `make`

Use `make build`, `make typecheck`, `make vet`, `make test`. Do **not** run bare
`go build ./...` or `go test ./...`.

This is a memory-safety rule, not style. Guest RAM under microsandbox is memfd-backed and
therefore resident and unswappable. A bare `go test -race ./...` runs one test binary per
package at `GOMAXPROCS` on top of live VMs, which has repeatedly exhausted host RAM and
tripped the **global** OOM killer — that does not fail politely, it kills `dbus` and
`ssh-agent` and tears down the whole login session along with any agent running inside it.

The `make` targets carry guards the bare commands do not: capped package/test parallelism,
`choom -n 1000` so the kernel prefers this process tree over session infrastructure, and a
fail-closed `systemd-run --user --scope` with `MemoryHigh`/`MemoryMax`.

Single package, or a tight memory budget:

    make test GOTEST_P=1 GOTEST_PARALLEL=1

Live tests that boot real VMs are gated behind `HERDR_MSB_LIVE=1`. Tests that spend an
Anthropic OAuth **refresh** are gated behind `HERDR_MSB_LIVE_REFRESH=1` as well — refresh
attempts are rate-limited to roughly one per session and a refresh **revokes the prior
token with zero overlap**, so never spend one casually.

Hold **at most one live sandbox at a time**, size it ≤2 GiB, and `msb remove` it when done.
Check `msb list` first and last.

## Gates run over `./...`

`typecheck`, `vet` and `test` all use a hardcoded `./...`, so **one broken file fails every
gate for every package**. If you see a failure in a directory you do not own, report it —
do not fix it, and do not assume the tree was already red.

## The import ban is the architectural boundary

`tools/importban` runs in `make vet` and enforces two rules:

- only `internal/runtime/msb/` may import the microsandbox SDK;
- nothing under `internal/core/` may import an `internal/runtime/*` implementation.

It is a plain stdlib Go program, **not depguard** — `golangci-lint` is not installed here,
so a linter-plugin config would have been a check that never ran.

This ban was once **green over a live violation**: `make vet` passes `.` to the checker,
`filepath.WalkDir`'s root entry has `Name() == "."`, that matched a skip-dot-directories
rule and `SkipDir`'d the entire repository — and five unit tests missed it because each
used an absolute `t.TempDir()` root. Fixed, with a regression test. **If you rely on the
ban, re-prove it bites**, and put any probe file **nested, never at the repo root**.

## Prove a check can fail before reporting it as evidence

Four vacuous assertions were caught in this repo's first days, every one only because
someone ran the negative control and required a **different** outcome:

- the import ban above;
- a raw-IP egress probe using `wget https://<ip>/` — it exits 1 on Cloudflare's
  `SSL alert number 40` for the missing SNI **even when unblocked**, so use plain TCP
  (`nc -z -w5 <ip> 443`);
- busybox `wget` appends its own `HTTP/1.1 401` line, so `grep 'HTTP/' | tail -1` read
  every non-2xx as `STATUS=server`;
- SDK v0.6.17's `(*SandboxHandle).Config().Network` is **structurally always nil**, so a
  zero-value comparison passes vacuously forever — read the daemon record via
  `h.ConfigJSON()` at `network.policy.default_egress` instead.

For any flag or boundary claim, require **two runs with opposite outcomes**. A single pass
cannot distinguish an honoured flag from an ignored one.

## Security posture — read before touching credentials or egress

The real Claude OAuth credential is **mounted into every guest** (read-write, by decision)
and is readable by anything running there. Containment rests **entirely** on the
microsandbox network profile. Two consequences:

- `--net-default-egress` is documented `deny` but carries an **implicit `allow@public`**,
  and `--net-rule` alone does **not** collapse it. An allowlist built the obvious way keeps
  a permanent full-internet bypass.
- The profile is applied **inside `SandboxOptions`**, the single place that builds
  `WithNetwork`. Do not add a second application at a call site — that is exactly how
  `RunEphemeral` shipped unprotected. An AST guard test enforces this.

Never read, write, or mount `~/.claude/.credentials.json`. The dedicated store is
`~/.config/nexus3/claude-dedicated/.credentials.json`; back it up **and verify the backup is
independently usable** before any refresh test. Never cap a token-endpoint response body —
a 2xx the client cannot parse destroys the credential, because the server has already
rotated. This has happened here once.

## Working in a shared tree

Multiple agents commit here concurrently. **Commit with explicit paths only**
(`git commit --only <paths>`); never `git add -A` or `git commit -a`. Never run
`git stash`, `git checkout --`, or `git reset --hard` — restore a mutation with `cp` from a
backup instead. An index race has already cost one agent its staged changes.

## `make build` writes a binary; `make typecheck` does not

`make build` uses an explicit `-o`. `make typecheck` holds the `go build ./...` role
separately and produces nothing. Install with the atomic rename in `make install` — a plain
`cp` over a live path fails with `Text file busy`.

## A repo hook caps comment density

At most 5 comment lines per 100 in any Go file, which makes a Go `doc.go` impossible.
Rationale belongs in `doc/*.md`.
