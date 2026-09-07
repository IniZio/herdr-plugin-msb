---
id: MSP-R-001
type: requirement
concept: C-MSP
title: Credentials reach the guest only by a live mount of the host store
pattern: event
verification: unverified
criticality: must
status: active
origin_decision_ref: nexus3-microsandbox-pivot#D-12
---

## MSP-R-001 — Credentials reach the guest only by a live mount of the host store {#msp-r-001}

**Build state:** TARGET — obligation not met; fit criterion not run. No sandbox
is created by this repository yet, so no credential reaches any guest by any
path.

**When** a sandbox is created for an agent that needs Claude credentials, the
plugin **shall** deliver them by mounting the host credential store into the
guest as a live host mount over virtio-fs, and **shall not** copy, snapshot,
rsync, template, or otherwise materialise credential material into the guest
image, the guest process environment, or any guest-local file.

- **Why** — Two delivery shapes were rejected for measured reasons, and a third
  would silently reintroduce both. microsandbox native secrets are excluded by F3:
  rotation is new-connections-only, and 26 requests over ~52 s on one keep-alive
  connection carried the old value after rotation. A copied file is excluded by
  F4: an Anthropic refresh revokes the prior access token immediately, with
  observed overlap zero, so a copy is a hard 401 rather than a stale-but-usable
  value. A mount is chosen because the guest reads the host file itself and there
  is no second location to go stale. That property is destroyed the moment any
  component also holds a copy, so "mount only" is the requirement, not "mount
  primarily".
- **Fit criterion** — Live, real OAuth, real microsandbox VM. (a) An agent inside
  a sandbox issues a real Anthropic API request that returns HTTP 200, reading its
  credential from the mount. (b) The guest's process environment and its image
  layers contain no bearer token, refresh token, or client secret; the only path
  to credential material is the mount point. (c) Removing the mount and repeating
  (a) fails, proving the mount was the delivery path rather than an unused
  decoration.
- **Verification**: unverified — no test exists. Target method: automated, live,
  against the real Anthropic API per charter D-2, with the token endpoint
  **not** stubbed.
- **Criticality**: must
- **Anchor** — Target constructs, by name: the mount list on the runtime create
  spec in package `internal/core/runtime`, and the code path in package
  `internal/runtime/msb` that applies it to a created sandbox. The
  `internal/core/runtime` package exists in this repository; the create path that
  would carry a credential mount is not asserted to exist. Anchors are named
  constructs and never line numbers.
- **See also** [MSP-R-002](msp-r-002-credential-in-guest-containment.md),
  [MSP-R-003](msp-r-003-refresh-read-through.md)
