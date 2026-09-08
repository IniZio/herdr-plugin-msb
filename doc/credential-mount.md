# Credential delivery — live host bind mount

The rationale lives here rather than in Go doc comments because this repo caps
comment density at 5 lines per 100 in source files.

## Mechanism

Claude OAuth credentials are delivered into the microsandbox by mounting the
host credential file (or its containing directory) into the guest at boot time.

At the seam (`internal/core/runtime/network.go`) this is `runtime.Mount{HostPath,
GuestPath, ReadOnly}`, carried on `SandboxSpec.Mounts`. The microsandbox
implementation resolves it to either `msb --mount-file SRC:DEST` or
`msb --mount-dir SRC:DEST`, and through the SDK as:

    WithMounts(map[string]MountConfig{
        dest: Mount.Bind(src, MountOptions{Readonly: true}),
    })

Host-side logic lives in `internal/core/credmount/`. The guest reads the host
file directly; no value is copied into a sandbox secret store.

## Why not native microsandbox secrets — findings F3 and F4

Two earlier designs were discarded on the basis of measured failures, not
speculation.

**F3 — rotation reaches only new connections.** After `msb modify --secret`,
a keep-alive TLS connection to the microsandbox API continued serving the old
secret value. Twenty-six requests over roughly 52 seconds after rotation all
carried the stale value. There is no knob that forces teardown of an
established connection at rotation time.

**F4 — an Anthropic OAuth refresh immediately revokes the prior token.**
The old access token returned HTTP 401 with `"OAuth access token has been
revoked"` approximately 85 seconds after the refresh grant, while it still
carried 3 hours 32 minutes of nominal validity. The error field is `revoked`,
not `expired`. Observed overlap between old and new token: zero.

The interaction is what makes the combination fatal: F3 opens a staleness
window of at least tens of seconds, and F4 makes anything inside that window
a hard 401 with no retry path. A connection that was valid at the last
keepalive can be dead before the next request, invisibly, with no signal
until the API call fails.

Native microsandbox secrets were rejected on those two measurements together.

## The proxy that was built and then set aside

A per-request token-swapping CONNECT proxy was built and measured working:
inbound Bearer was a 64-character hex placeholder, outbound was the real
108-character token, and the real Anthropic API returned HTTP 200. The proxy
is immune to F3 (it fetches the current token per request, never caching
across connections) and immune to F4 (it reads whatever is current at call
time). It exists as prior art in this codebase.

The operator chose the mount instead. The reason was simplicity, not a
failure of the proxy. The proxy remains available if the mount's tradeoff
proves unacceptable.

## Why the mount is believed immune to F3 and F4 — a tested bet

The mount survives F3 because there is no secret store and no connection
whose cached value can go stale: the guest reads the host file on every
access. A refresh written on the host is visible inside the guest as soon as
the guest opens the file again, leaving no staleness window.

It survives F4 for the same reason: the prior token is not held anywhere
inside the sandbox after the host writes a new one. There is nothing to
revoke.

This is a bet, not a fact. The failure mode that would invalidate it: if any
layer inside the guest caches the token in memory rather than re-reading the
file, the design fails at the first refresh — precisely the shape that F3 and
F4 already used to kill two prior designs. Both F3 and F4 were found by
measurement, not anticipated by analysis. This bet sits under the same
suspicion, and the place where it is actually checked is the test suite, not
this document. See `internal/core/credmount/` and the live harness under
`internal/core/credmount/livemsb/`.

## The inode hazard

An atomic credential write — write a sibling temp file, then `rename` it over
the target — produces a new inode. A bind mount of a single file resolves to
the old inode at mount time. After a rename-style refresh, the host path is
correct and the guest path is silently wrong: the guest holds a mount to the
old inode, which may have been unlinked.

An in-place rewrite (`O_TRUNC` followed by a full write) keeps the inode and
is visible through a file-level bind mount. Mounting the containing directory
rather than the file also survives a rename, because the directory inode does
not change and the new file's inode is what appears at the resolved path.

This is why the code offers both a directory mount and both writer styles. The
hazard is structural; the measured answer to which combination is reliable
under live refresh belongs in the test results, not here.

Measured, live, against a real `msb` guest with `--mount-file`
(`internal/core/credmount/livemsb/credmount_live_test.go`, subtest
`AC2_host_refresh_midsession`): **both** write modes propagate. An in-place
`O_TRUNC` rewrite (`SaveInPlace`) is visible in the guest, and so is a
sibling-temp-plus-`rename` (`Save`) — the guest read the post-rename bytes
through the same single-file mount. On this microsandbox version the file
mount therefore resolves the host path per access rather than pinning the
inode taken at mount time, so the inode hazard did not materialise and an
in-place write is not *required* for propagation. It remains the safer
default, because that resolution behaviour is a property of the runtime we do
not control and is not part of any contract it publishes.

## Accepted cost — blast radius

The real OAuth token is now inside the microsandbox guest and readable by
anything running there. That is a deliberate containment reversal. The
operator was told and chose simplicity.

Containment therefore rests entirely on the sandbox network profile. That
profile is not hardened by default: `--net-default-egress deny` carries an
implicit `allow@public`, so a default sandbox reaches the public internet.
The network profile is not containment until it is explicitly tightened, and
that tightening is the work of a separate slice.

The blast radius is asserted by a test in the suite, not only by this
document. A future reader should check the test, not the claim here:
`AC4_guest_process_reads_token_blast_radius` reads the token from the mount
inside the guest and compares its digest to the host file's.

Measured shape of that loss: the mount preserves the host file's ownership and
mode, which arrive in the guest as `0:0 600`. `msb exec` runs as root, so the
guest's default process identity — the identity an agent in the sandbox
actually has — reads the live token in full. A second, non-root guest user was
denied (`Permission denied`). The mode is therefore a real boundary against
additional guest users, and no boundary at all against the agent itself.

## Protected path

`~/.claude/.credentials.json` is the operator's live login session. It must
never be read, written, mounted, or rotated by this project. This project
uses `~/.config/nexus3/claude-dedicated/.credentials.json` or an
operator-named path instead.

`internal/core/credmount/` enforces the protected path with an explicit guard
and a test.

**AC5 scope note.** The new dedicated store is at
`~/.config/nexus3/claude-dedicated/.credentials.json`, which is not under
`~/.claude`. It is therefore in scope for mounting by this project.
`~/.claude/.credentials.json` itself remains strictly off-limits and is still
rejected by the path guard. AC5 is MET.

## Credential store — path and shape

**Old store (dead).** `~/.config/nexus3/creds.json`. Flat JSON, snake_case:
`access_token`, `refresh_token`, `expires_at` (RFC 3339 string), `token_type`,
`client_id`, `client_secret`, `token_endpoint`. This file's access token is
revoked and its refresh token is invalid; see the s16-AC6 section below for
how that happened.

**New store (live).** `~/.config/nexus3/claude-dedicated/.credentials.json`,
mode 0600. One top-level key `claudeAiOauth` containing camelCase fields:
`accessToken`, `refreshToken`, `expiresAt` (UNIX epoch milliseconds),
`refreshTokenExpiresAt` (epoch milliseconds), `scopes`, `subscriptionType`,
`rateLimitTier`. The store carries no `client_id`, `client_secret`, or
`token_endpoint`.

`DefaultClientID` (`9d1c250a-e61b-44d9-88ed-5944d1962f5e`) and
`DefaultTokenEndpoint` (`https://platform.claude.com/v1/oauth/token`) are now
constants in `internal/core/credmount/refresh.go`, not fields read from the
store. Note that `platform.claude.com` is the correct host for tokens issued
against this store; `console.anthropic.com` is an older host.

### Defect 1 — silent empty credential (fixed)

The flat struct was pointed at the nested file. `encoding/json` ignores
unknown keys, so `Load()` returned a nil error with every field empty.
Downstream that surfaced as an inexplicable 401 in the guest rather than a
parse failure on the host — strictly worse than an error, because the failure
appeared to be a dead token, not a host-side parse bug.

**Standing rule:** a credential loader must fail loudly on any store it cannot
parse and must never return a zero-valued credential with a nil error. An empty
access token is itself an error.

Fixed by: shape detection (nested `claudeAiOauth` wrapper vs legacy flat),
epoch-millisecond time handling, an error on unrecognised shape or invalid
JSON or empty access token, and round-trip preservation of unmodelled keys so
a save cannot drop fields the loader does not model.

### Defect 2 — 512-byte body cap (fixed, commit b6e1c42)

`refresh.go` read the token endpoint response through
`io.ReadAll(io.LimitReader(resp.Body, 512))`. The real response exceeds 512
bytes, so `json.Unmarshal` failed and the rotated token pair was discarded
without being written. Because an Anthropic refresh immediately revokes the
prior token with zero overlap (F4), this left the store holding a dead pair
with no recovery path other than a fresh login.

**Standing rules.** Never cap a token-endpoint response body. Persist the raw
response bytes to disk before unmarshalling, so that a parse bug leaves the
rotated pair recoverable. Both rules are now enforced by tests, including a
regression test that fails if a cap is reintroduced.

### Guest extraction trap

In-guest scripts previously extracted the token with a sed one-liner keyed on
the flat field name `access_token`. Against the nested store that matched
nothing, `TOKEN` came out empty, and the guest's API call returned 401 — which
reads exactly like a dead token but was an extraction bug on the guest side.

Both scripts now key on `accessToken` and fail loudly with an
`EXTRACTION_FAILED:` marker and a non-zero exit when the extracted token is
empty, so this class of bug cannot again present as an auth failure.

Guest image note: the base image is minimal Alpine. `busybox sed` is present;
`jq` is not. `curl` is not present by default and must be installed with
`apk add --no-cache curl` before use; only `busybox wget` is available out of
the box.

## Live results, 2026-09-07

The suite is `internal/core/credmount/livemsb/credmount_live_test.go`, run as
`HERDR_MSB_LIVE=1 make test GOTEST_P=1 GOTEST_PARALLEL=1 GOTEST_ARGS='-tags
live -v -timeout 25m -run TestAC'`. It boots one `s16-`-prefixed alpine guest
at 1024 MiB with `~/.config/nexus3/creds.json` bind-mounted as a single file,
installs `curl` in the guest, and reads the token out of the mount inside the
guest at call time — never through an environment variable or a command line.

A real `POST https://api.anthropic.com/v1/messages` from inside the guest,
authorised only by the mounted token, returned **HTTP 200**. Two model ids
were tried: `claude-3-5-haiku-20241022` returned HTTP 404
`not_found_error: model: claude-3-5-haiku-20241022`, which is an
authenticated answer and not an auth failure; `claude-haiku-4-5` returned 200.
A dead token would have produced 401 and a blocked User-Agent a Cloudflare 403
with error 1010, so neither of those was in play.

The mid-session refresh half of the bet is **still not proven — blocked on a
rate limit, not on a defect.** Four attempts at `RefreshFile(..., inPlace:
true)` against `https://platform.claude.com/v1/oauth/token`, spread across
32 minutes with growing waits between them, every one **HTTP 429**:

| # | UTC | status | body |
|---|-----|--------|------|
| 1 | 2026-09-07T18:46:3x | 429 | `rate_limit_error` |
| 2 | 2026-09-07T18:46:55 | 429 | `rate_limit_error` |
| 3 | 2026-09-07T18:57:42 | 429 | `rate_limit_error` |
| 4 | 2026-09-07T19:18:21 | 429 | `rate_limit_error` |

Attempts 1 and 2 landed seconds apart because the first run's status was not
read before the second was launched; 3 and 4 followed the intended 10-minute
and 20-minute waits. The body is identical every time — `{"error":{"type":
"rate_limit_error","message":"Rate limited. Please try again later."}}` — and
no response carried a `Retry-After` header or named a window, so there was
nothing to honour beyond the schedule. Backing off further was not tried and no
workaround was: switching endpoint, grant type, account, or running any `auth
login` flow would answer a different question than the one asked, and would
rotate the operator's session.

Both preconditions held on every attempt. The guest was mid-session with a
**200** already on the board from the mounted token (`AC1`), and the store was
verified byte-identical afterwards each time — `RefreshFile` returns before any
write on a non-2xx (`internal/core/credmount/refresh.go`), so a rate-limited
refresh leaves the credential file untouched. That is now measured, not assumed.

What stays open is only the OAuth half of F4: whether the guest's next call
after a real refresh returns 200 or the revocation 401. The mount half is
settled by the propagation measurement above — new host bytes do reach the
guest under both write modes, so a stale read is not the mechanism that would
break it. The remaining risk is a token this project never holds.

No `auth login` flow is run here. A device-login reuse rotates the OAuth
credentials and logs the operator out of their session. The credential file
this project reads is obtained and renewed outside this project's scope.

## s16-AC3 — write direction, measured 2026-09-07T19:21Z

`TestGuestWriteDirection` in `internal/core/credmount/livemsb/contention_live_test.go`
boots one 512 MiB alpine guest with a working copy of the store bind-mounted at
`/mnt/creds.json` and has the guest run `echo SENTINEL > /mnt/creds.json`.

**AC3 holds on branch (a): the mount is read-write and a guest write reaches the
host.** The guest write exited **0**, with empty stderr. The host file's bytes
changed and its inode did **not** — `30850007` before and after — so the guest
wrote in place through the mount rather than replacing the file. The test
restored the working copy from the store afterwards; the real store was never
the mount target in this test.

A guest-side refresh writer is therefore possible, and a guest that writes the
file also has the power to corrupt the operator's credential store. That widens
the blast radius recorded above from read to read-write.

**Correction (s18).** This paragraph previously claimed the read-only
alternative was "not expressible", inferred from `msb create --help` not listing
an `ro` option. That inference was wrong — the help text says OPTIONS "may
include" `quota=` and `uid=/gid=`, which is non-exhaustive. Read-only mounts are
supported by the CLI, the Go SDK and the Rust runtime; what was missing was our
own plumbing, and the harness's silent drop of `BindMount.ReadOnly` was a defect
rather than a backend limitation. See `doc/readonly-mounts.md`, which carries
the evidence, the recommendation on whether the credential store should ship
mounted `ro`, and the refresh-propagation coupling that choice creates.

## s16-AC6 — BLOCKED, and the credential store was destroyed measuring it

`TestConcurrentMountContention` boots two 1024 MiB guests on the same
single-file mount and then refreshes on the host before letting either guest
call the API. It **SKIPPED both times** — the refresh never succeeded, so the
revocation half of F4 was never exercised. **AC6 is BLOCKED, not met.** No
green result may be read out of this run: the sandbox pair was created and both
guests read the same mount, but a contention finding without a completed
refresh is vacuous.

The measurable half, taken by hand with the same mount and no refresh: two
sandboxes (`s16-manual-a`, `s16-manual-b`) both read the token out of one shared
mount and both got the identical HTTP status. Sharing one file across two
guests is not itself the failure mode.

The blocking cause is not the 429 recorded above. It is a defect in this
package, and it consumed the operator's credentials:

| # | UTC | outcome |
|---|-----|---------|
| 5 | 2026-09-07T19:22:05 | HTTP 2xx, `parse token response: unexpected end of JSON input` |
| 6 | 2026-09-07T19:32:33 | HTTP 400 `{"error": "invalid_grant", "error_description": "Refresh token not found or invalid"}` |

Attempt 5 took the success path — the non-2xx branch in `Refresh` was not
entered — so the server issued a new token pair. `Refresh` then read the
response through `io.ReadAll(io.LimitReader(resp.Body, 512))`, could not parse
the result, and returned an error, which discards the new pair without writing
it. The store was left byte-identical, holding the pair the server had just
rotated away.

Both halves of that are now measured, not inferred. A guest call with the
stored access token returned **HTTP 401** with
`{"type":"authentication_error","message":"OAuth access token has been
revoked."}` — the `revoked` shape of F4, arrived at without any second party,
30 minutes inside the token's nominal validity (`expires_at`
`2026-09-07T19:52:36Z`). Attempt 6 then proved the refresh token had been
rotated too: `invalid_grant`.

The 512-byte cap is now `1<<20` for the body read, keeping the 200-character
truncation only for the error preview. That fixes the next refresh; it cannot
undo this one.

**Operator action required: the store at `~/.config/nexus3/creds.json` is dead.**
Its access token is revoked and its refresh token is invalid. It is still valid
JSON and its `expires_at` still reads `2026-09-07T19:52:36Z`, so nothing in the
file signals the state — only an API call does. The `/tmp` backup is a copy of
the same dead pair. Recovery needs a fresh login, which is outside this
project's scope and was deliberately not attempted here.

Two smaller defects were fixed in the same file while measuring. The guest
status parser took `grep 'HTTP/' | tail -1`, and busybox `wget` writes its own
`wget: server returned error: HTTP/1.1 401 Unauthorized` line last, so every
non-2xx parsed as `STATUS=server` — the exact case AC6's `both401` invariant
exists to catch. It now matches `HTTP/1.x NNN` directly. The probe body also
asked for `claude-3-5-haiku-20241022`, whose 404 is an authenticated answer but
not a positive one; it now asks for `claude-haiku-4-5`, which returns 200 on a
live token.

**Fix status (commit b6e1c42).** The 512-byte cap is now `1<<20`. The store
shape has been updated to the new nested layout. The standing rules from Defect
2 above are enforced by regression tests. The old store at
`~/.config/nexus3/creds.json` remains dead; the live store is at
`~/.config/nexus3/claude-dedicated/.credentials.json`.

## AC status — 2026-09-08

**AC1 — MET (re-confirmed).** Guest reached `api.anthropic.com/v1/messages`
and got HTTP 200 with model `claude-haiku-4-5`. The guest's token digest
matched the host's (`dec964bb5553`). This run confirmed AC1 against the new
nested store.

**AC2 — BLOCKED.** Mount propagation is settled in the design's favour: both
write modes (`Save` rename and `SaveInPlace` O_TRUNC) propagate through
`--mount-file` because the guest re-reads the host path per access. What
remains untested is whether a real refresh reaches a running guest. At
`2026-09-08T01:28:19Z` the token endpoint returned HTTP 429
`rate_limit_error` ("Rate limited. Please try again later.") with no
`Retry-After` header and no documented window. The failure was non-destructive:
`store_untouched=true` and the pre-existing access token still returned HTTP
200 afterwards, confirming a 429 performs no rotation and the operator's login
survived.

**AC3 — MET (unchanged).** Branch (a): the rw write lands and the inode is
stable. The test writes to a copy, never the live store.

**AC4 — MET (unchanged).** The guest's default (root) process reads the live
token in full; a non-root guest user is denied by the preserved host file mode.
The mount is read-write by operator decision (D-15), not by backend limitation:
a read-only store cannot accept an in-guest refresh, which would make host-side
propagation load-bearing while AC2 remains unproven. The earlier claim that
`ReadOnly` was silently ignored on `--mount-file` is RETRACTED — see the s18
correction above. `ReadOnly` is expressible and live-proven; the defect was
`livemsb/harness.go` discarding `BindMount.ReadOnly`, which meant every
credential-mount measurement taken through that harness ran read-write
regardless of what the test asked. That is fixed, and the measurements in this
section were rw as intended.

**AC5 — MET.** The new dedicated store is not under `~/.claude`, so it is in
scope for mounting. `~/.claude/.credentials.json` remains strictly off-limits
and is still rejected by the path guard.

**AC6 — BLOCKED.** Explicitly not passed vacuously. Measurement design: two
concurrent sandboxes mount the same store; both are proven to reach HTTP 200
with identical token digests (`dec964bb5553`); then one host-side refresh is
performed and both guests call again. The refreshing side answers AC2 and the
bystander answers AC6. This settles both ACs off a single rotation, because
with no broker, whichever side refreshes revokes the other's token (F4), and
refresh attempts are too rate-limited to spend one per AC. AC6 cannot be closed
until a refresh returns 2xx.

## Operational guardrails

The test that spends a refresh is gated behind both `HERDR_MSB_LIVE=1` and
`HERDR_MSB_LIVE_REFRESH=1` so it cannot be spent by an ordinary live run.

Before any refresh attempt: back up the store AND verify the backup is
independently usable — load it and make a real API call returning HTTP 200.
A backup taken after rotation holds the same dead pair.

At most one live sandbox at a time (two only briefly for AC6) at ≤2 GiB.
Guest RAM is memfd-backed, resident, and unswappable.

## How to run

AC1 and AC4 (safe, no refresh):

    HERDR_MSB_LIVE=1 make test GOTEST_P=1 GOTEST_PARALLEL=1 \
      GOTEST_ARGS="-tags=live -run 'TestACCredentialMountLive/AC1|TestACCredentialMountLive/AC4'"

AC3 (safe, writes to a copy):

    HERDR_MSB_LIVE=1 make test GOTEST_P=1 GOTEST_PARALLEL=1 \
      GOTEST_ARGS="-tags=live -run TestGuestWriteDirection"

AC2 and AC6 (spends a refresh — back up the store first and verify the backup):

    HERDR_MSB_LIVE=1 HERDR_MSB_LIVE_REFRESH=1 make test GOTEST_P=1 GOTEST_PARALLEL=1 \
      GOTEST_ARGS="-tags=live -run TestAC2AC6RefreshPropagation"

Bare `go test ./...` is forbidden: without the `make` wrapper the test binary
runs at `GOMAXPROCS` and has tripped the global OOM killer, tearing down the
login session.
