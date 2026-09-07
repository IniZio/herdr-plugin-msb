---
id: C-MSP
type: concept
title: Microsandbox-backed runtime
parent: C-HERDR-PLUGIN-MSB
summary: "Requirements for the microsandbox-backed runtime: mounted credentials, network-profile containment, host mounts, port forwarding, verb surface, import seam."
status: active
origin_decision_ref: nexus3-microsandbox-pivot#D-12
---

# Microsandbox-backed runtime (`MSP-R-*`)

This area carries the acceptance criteria of the `nexus3-microsandbox-pivot`
motive charter. Before this area existed, all nine charter ACs were untraced
(charter open item TBR-3): the four nexus3 spec areas — `PDF-R-*`, `RES-R-*`,
`SUR-R-*`, `PER-R-*` — describe the cloud-hypervisor substrate and none of them
describes a microsandbox-backed runtime.

## Build-state legend

**An unbadged claim reads as a positive claim.** Every node in this area
therefore carries an explicit `**Build state**` line as its first body line, with
exactly one of these values:

| Value | Meaning |
|---|---|
| `TARGET — obligation not met; fit criterion not run` | The node states an obligation the product does not yet meet. Nothing in the node may be read as a description of shipped behaviour. |
| `PARTIAL — <what exists>` | A named construct exists and is named on the line, but the obligation is not fully met. |
| `MET` | The obligation is met **and** the fit criterion has been run. No node carries this value. |

Build state describes the **obligation**, not the presence of code. A node can be
`TARGET` while unit-level building blocks for it exist, because the fit criteria
here are live end-to-end conditions and none of them has been run.

Peer slices are landing work into this tree concurrently and uncommitted. At the
time this area was written the working tree carried `internal/core/credmount/`,
`internal/core/netprofile/`, and files under `internal/runtime/msb/` that are not
in `HEAD`. This area does **not** characterise what those packages do — that is
their own slices' evidence to give — and no node's build state is upgraded on the
strength of their existence.

A node's `verification:` frontmatter is `unverified` while no test exercises it.
The `**Verification**` annotation line names the *target* method separately, so
"no test exists yet" is never mistaken for "manual verification is intended".

Anchors are **construct names**, never line numbers: a line number can go stale
in the same run that wrote it. Where a node's target construct does not exist
yet, the `**Anchor**` line says so in words.

## Charter restatement under D-12 (this is not a rewording)

Decision **D-12** (2026-09-07, accepted) deleted the credential design the
charter was built on. It reverses D-4 and moots D-5, D-6 and D-10. Credentials
now reach the guest by **live volume mount of the host credential store**,
codebox-style. **There is no CONNECT proxy, no placeholder swap, and no host
level broker**, and nothing descends from `internal/core/perimeter/cred/` or
`internal/core/perimeter/mitm/`.

Charter **AC-1** and **AC-2** are written in the vocabulary of that deleted
design, so their literal text now describes a requirement the product
deliberately no longer meets. Writing nodes against them would produce a gate
that passes against a void requirement. They are restated here; the charter file
itself is unchanged, because charter edits are not this area's to make.

### AC-1 as written (VOID)

> **AC-1:** An agent inside a microsandbox sandbox completes a real Claude API
> call through nexus3's own CONNECT proxy, having never held the real access
> token.

Void in both clauses. There is no CONNECT proxy, and under D-12 the guest **does**
hold the real access token.

### AC-1 restated (`AC-1'`)

> **AC-1':** An agent inside a microsandbox sandbox completes a real Claude API
> call using the credential it reads from a **live mount of the host credential
> store**, with no proxy and no placeholder in the path. The mount is the **sole**
> delivery path: no credential material is copied, snapshotted, rsynced or
> templated into the guest image, the guest environment, or any guest-local file,
> and removing the mount makes the same call fail.
>
> **AC-1' carries a containment obligation in place of the deleted one.** The
> charter's "having never held the real access token" is not weakened, it is
> **reversed**: the real credential IS present in the guest and is readable by
> anything running there. Containment rests **entirely** on the microsandbox
> network profile, which by F7 is **not** closed by default —
> `--net-default-egress` defaults to `deny` with an implicit `allow@public`, so a
> sandbox left at the default reaches the public internet. The profile must
> therefore be set explicitly, and no spec or document may claim the token is
> absent from the guest.

Nodes: [MSP-R-001](requirements/msp-r-001-credential-live-mount.md),
[MSP-R-002](requirements/msp-r-002-credential-in-guest-containment.md).

### AC-2 as written (VOID)

> **AC-2:** Token refresh mid-session does not break an in-flight agent.
> *note: forces a refresh (which by F4 revokes the old token) while the agent
> holds a keep-alive connection, and asserts the next request succeeds. This is
> the F3+F4 regression guard.*

The outcome clause survives; the mechanism does not. The note's guard was written
against per-request swapping at a proxy. With the proxy deleted there is no
swap point, and F3 (microsandbox native secret rotation is new-connections-only)
is no longer the mechanism in play either — the mount is not a secret injection.

### AC-2 restated (`AC-2'`)

> **AC-2':** A token refresh propagates **across the mount**, in both directions
> that matter, and no cached or copied token survives on either side.
>
> (a) **Host to guest:** a refresh written to the host credential store — by the
> operator, by a host process, or by another sandbox mounting the same store — is
> observed by the in-guest agent on its **next credential read**, including while
> that agent holds an already-established keep-alive connection to the API. The
> next request succeeds rather than returning 401 `revoked`.
>
> (b) **Guest to host:** a refresh performed by the agent **inside** the guest is
> written through to the host store, so the host and every other sandbox mounting
> it observe the rotated access token and the rotated refresh token.
>
> Read-through, not a snapshot, is the requirement. By F4 an Anthropic refresh
> **immediately revokes** the prior access token — observed overlap **zero**,
> error `revoked` rather than `expired` — so a cached copy anywhere in the system
> is not merely stale, it is a hard 401.

Node: [MSP-R-003](requirements/msp-r-003-refresh-read-through.md).

### Other ACs affected by later decisions

- **AC-4** as written tests "a guest that **ignores** `HTTPS_PROXY`". Under D-12
  no `HTTPS_PROXY` is set at all, so the qualifier is moot. The negative case
  survives and becomes the whole test, and F7 adds a second obligation: deny is
  not the default state. See
  [MSP-R-005](requirements/msp-r-005-egress-default-deny.md).
- **AC-6** as written requires every one of the 29 (measured: 37) nexus3 verbs to
  be preserved or given a written reason. **D-13** drops CLI parity as a goal, so
  the parity target is replaced by a declared minimal verb set plus a named
  out-of-scope list. See
  [MSP-R-007](requirements/msp-r-007-declared-verb-surface.md).
- **AC-8** is a deletion criterion against the **nexus3** tree. Its clean-room
  half is a checkable obligation here; its deletion half is not in this repo.
  See [MSP-R-009](requirements/msp-r-009-no-substrate-descendant.md) and the
  coverage table's note.

## Coverage: charter AC to requirement node

| Charter AC | Restated? | Node(s) | Coverage |
|---|---|---|---|
| AC-1 → **AC-1'** | yes, mechanism void | [MSP-R-001](requirements/msp-r-001-credential-live-mount.md), [MSP-R-002](requirements/msp-r-002-credential-in-guest-containment.md) | full |
| AC-2 → **AC-2'** | yes, mechanism void | [MSP-R-003](requirements/msp-r-003-refresh-read-through.md) | full |
| AC-3 | no | [MSP-R-004](requirements/msp-r-004-worktree-rw-mount.md) | full |
| AC-4 | yes, qualifier moot | [MSP-R-005](requirements/msp-r-005-egress-default-deny.md) | full |
| AC-5 | no | [MSP-R-006](requirements/msp-r-006-same-port-forwarding.md) | full |
| AC-6 | yes, D-13 drops parity | [MSP-R-007](requirements/msp-r-007-declared-verb-surface.md) | full |
| AC-7 | no | [MSP-R-008](requirements/msp-r-008-runtime-seam-import-ban.md) | full |
| AC-8 | split | [MSP-R-009](requirements/msp-r-009-no-substrate-descendant.md) | **PARTIAL — see below** |
| AC-9 | no | [MSP-R-010](requirements/msp-r-010-herdr-remote-attach.md) | full |

**AC-8 is only partly covered, and that is reported rather than omitted.** AC-8
reads: "The named substrate packages are gone from the tree and `make build`,
`make vet`, and `make test` are green with the selector removed." MSP-R-009
covers the half that is checkable in this repo — no descendant of the F1 delete
list, and no runtime selector naming an old path. The other half, physically
deleting ~29,400 non-test lines from `/home/newman/magic/nexus3` and getting its
`make` targets green, cannot be asserted by a node in this repo's spec graph.
Charter slice `s11-substrate-deletion` owns it. Until a node exists in the
nexus3 graph, **AC-8's deletion half is uncovered**. A forgotten obligation reads
as N/N complete, so it is named here instead.

One node in this area traces to a risk rather than an AC:
[MSP-R-011](requirements/msp-r-011-privilege-posture-recorded.md) records
RISK-6, the knowing forfeit of nexus3's zero-networking-privilege property. It
exists because a stale security claim is worse than an absent one, and the same
reversal that put the token in the guest also removed the rootless posture that
would have limited the damage.
