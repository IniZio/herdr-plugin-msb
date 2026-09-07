# Spec Index

_Generated: 2026-09-07T14:31:10.032Z_

## Concepts

| Concept | Summary | Status | Views |
| --- | --- | --- | --- |
| C-MSP | Requirements for the microsandbox-backed runtime: mounted credentials, network-profile containment, host mounts, port forwarding, verb surface, import seam. | active | — |

## Microsandbox-backed runtime

### [MSP-R-001 — Credentials reach the guest only by a live mount of the host store](../msp/requirements/msp-r-001-credential-live-mount.md#msp-r-001)

### [MSP-R-002 — The real credential is present in the guest and the network profile is the only boundary](../msp/requirements/msp-r-002-credential-in-guest-containment.md#msp-r-002)

### [MSP-R-003 — A token refresh propagates across the mount in both directions](../msp/requirements/msp-r-003-refresh-read-through.md#msp-r-003)

### [MSP-R-004 — A host git worktree is mounted read-write and edits round-trip both ways](../msp/requirements/msp-r-004-worktree-rw-mount.md#msp-r-004)

### [MSP-R-005 — Egress is an explicit allowlist and the negative case is the test](../msp/requirements/msp-r-005-egress-default-deny.md#msp-r-005)

### [MSP-R-006 — A sandbox listener on a remote host is reachable from the laptop on the same port and is torn down with the sandbox](../msp/requirements/msp-r-006-same-port-forwarding.md#msp-r-006)

### [MSP-R-007 — The shipped verb set is declared and every absent nexus3 verb is named](../msp/requirements/msp-r-007-declared-verb-surface.md#msp-r-007)

### [MSP-R-008 — The runtime seam is enforced by the toolchain, not by review](../msp/requirements/msp-r-008-runtime-seam-import-ban.md#msp-r-008)

### [MSP-R-009 — No descendant of the deleted substrate and no runtime selector](../msp/requirements/msp-r-009-no-substrate-descendant.md#msp-r-009)

### [MSP-R-010 — Every plugin action is reachable under a remote attach without a custom keybinding](../msp/requirements/msp-r-010-herdr-remote-attach.md#msp-r-010)

### [MSP-R-011 — The forfeited zero-networking-privilege property is recorded, not left as a stale claim](../msp/requirements/msp-r-011-privilege-posture-recorded.md#msp-r-011)
