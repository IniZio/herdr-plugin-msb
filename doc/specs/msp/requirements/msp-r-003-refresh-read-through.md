---
id: MSP-R-003
type: requirement
concept: C-MSP
title: A token refresh propagates across the mount in both directions
pattern: event
verification: unverified
criticality: must
status: active
origin_decision_ref: nexus3-microsandbox-pivot#D-12
---

## MSP-R-003 — A token refresh propagates across the mount in both directions {#msp-r-003}

**Build state:** TARGET — obligation not met; fit criterion not run.

**When** an OAuth refresh rotates the access token and the refresh token, the
system **shall** make the rotated values observable on the other side of the
mount at the **next credential read**, in both directions:

- **host to guest** — a refresh written to the host credential store, whether by
  the operator, a host process, or another sandbox mounting the same store, is
  observed by the in-guest agent on its next read, **including** while that agent
  holds an already-established keep-alive connection to the API;
- **guest to host** — a refresh performed by the agent inside the guest is
  written through to the host store, so the host and every other sandbox mounting
  it observe both rotated values.

No component **shall** retain a copy of a token beyond the read that used it.

- **Why** — F4 makes a cached token a hard failure rather than a degraded one:
  an Anthropic refresh revokes the prior access token immediately, the observed
  overlap is **zero**, and the error is `revoked`, not `expired`. There is
  therefore no "stale but valid" window to design around, and any mitigation that
  assumes one is void. Read-through across the mount is what removes the second
  location; a snapshot or an rsync reintroduces exactly the staleness window that
  killed both the native-secrets option (F3) and the copy option. The guest-to-host
  direction matters as much as the reverse, because the agent inside the guest is
  the party most likely to perform the refresh, and if its rotation is not written
  through then the host and every sibling sandbox are left holding a revoked
  token.
- **Fit criterion** — Live, real OAuth. (a) Force a host-side refresh while the
  in-guest agent holds an established keep-alive connection; the agent's next
  request returns HTTP 200, not 401 `revoked`. (b) Force a refresh from inside the
  guest, then read the host store and assert both the rotated access token and the
  rotated refresh token are present host-side. (c) Scan the guest filesystem and
  process environment and assert no second copy of either token exists outside the
  mount point.
- **Verification**: unverified — no test exists. Target method: automated, live,
  real OAuth against the real Anthropic API. Per charter D-2 the token endpoint
  may be stubbed only in unit tests under an explicit waiver; this node's gate
  test **shall not** stub it, because F3 and F4 are exactly the class of failure a
  stub cannot express.
- **Criticality**: must
- **Anchor** — Target construct: the credential read path used by the in-guest
  agent, and the host-store mount source resolved at sandbox create time. Neither
  exists yet.
- **See also** [MSP-R-001](msp-r-001-credential-live-mount.md)
