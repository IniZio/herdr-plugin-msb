---
id: MSP-R-011
type: requirement
concept: C-MSP
title: The forfeited zero-networking-privilege property is recorded, not left as a stale claim
pattern: ubiquitous
verification: unverified
criticality: must
status: active
origin_decision_ref: nexus3-microsandbox-pivot#D-3
---

## MSP-R-011 — The forfeited zero-networking-privilege property is recorded, not left as a stale claim {#msp-r-011}

**Build state:** TARGET — obligation not met; fit criterion not run.

The plugin's security documentation **shall** state that it requires read-write
access to `/dev/kvm` and has no rootless path, and **shall not** claim the
rootless-userns and gvisor-netstack-over-a-TAP-fd property that the previous
substrate had.

- **Why** — nexus3's zero-networking-privilege property is forfeited knowingly
  (RISK-6), because microsandbox requires rw `/dev/kvm` and offers no rootless
  mode. A stale security claim is worse than an absent one: it is the input an
  operator uses to decide what to run inside a sandbox, and here it compounds with
  D-12 — the same pivot that put the operator's live OAuth token inside the guest
  also removed the privilege posture that would have limited what a guest escape
  could reach. Leaving the old claim standing would describe a system with a
  broker and no host privilege, which is the exact inverse of what ships.
- **Fit criterion** — The shipped security documentation states the `/dev/kvm`
  requirement and the absence of a rootless path; a documentation gate finds no
  surviving claim of rootless operation, zero networking privilege, a gvisor
  netstack, or a TAP-fd-based perimeter, and the gate is proven able to fail by
  planting such a claim.
- **Verification**: unverified — no gate exists. Target method: automated
  documentation check.
- **Criticality**: must
- **Trace** — This node traces to charter RISK-6 rather than to a numbered
  acceptance criterion. The charter has no AC for it; it is recorded here so the
  obligation is not invisible.
- **Anchor** — Target construct: the security page of the shipped documentation,
  and the documentation gate. Neither exists yet.
- **See also** [MSP-R-002](msp-r-002-credential-in-guest-containment.md)
