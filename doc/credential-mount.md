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
uses `~/.config/nexus3/creds.json` or an operator-named path instead.

`internal/core/credmount/` enforces the protected path with an explicit guard
and a test.

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

The read-only alternative is not expressible: `msb create --help` exposes no
`ro`, `:ro`, or `readonly` option for `--mount-file` or `--mount-dir`, and the
harness silently ignores `BindMount.ReadOnly`. A guest-side refresh writer is
therefore possible, and a guest that writes the file also has the power to
corrupt the operator's credential store. That widens the blast radius recorded
above from read to read-write.

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
