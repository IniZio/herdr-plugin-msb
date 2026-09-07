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

<!-- MEASURED: propagation results, see internal/core/credmount/livemsb -->

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
document. A future reader should check the test, not the claim here.

## Protected path

`~/.claude/.credentials.json` is the operator's live login session. It must
never be read, written, mounted, or rotated by this project. This project
uses `~/.config/nexus3/creds.json` or an operator-named path instead.

`internal/core/credmount/` enforces the protected path with an explicit guard
and a test.

No `auth login` flow is run here. A device-login reuse rotates the OAuth
credentials and logs the operator out of their session. The credential file
this project reads is obtained and renewed outside this project's scope.
