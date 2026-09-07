---
id: MSP-R-002
type: requirement
concept: C-MSP
title: The real credential is present in the guest and the network profile is the only boundary
pattern: ubiquitous
verification: unverified
criticality: must
status: active
origin_decision_ref: nexus3-microsandbox-pivot#D-12
---

## MSP-R-002 — The real credential is present in the guest and the network profile is the only boundary {#msp-r-002}

**Build state:** TARGET — obligation not met; fit criterion not run. No sandbox
is created by this repository yet, and no claim gate over the documentation
exists.

The plugin **shall** treat the mounted credential as fully readable by every
process inside the guest, and **shall** rely on the microsandbox network profile
as the **sole** containment boundary for it.

**When** a sandbox is created, the plugin **shall** set the egress default
**explicitly** and **shall not** rely on the library default.

The plugin's specifications and its user-facing documentation **shall not** claim
that the guest never holds the real access token, that a placeholder is
substituted, or that a broker or CONNECT proxy stands between the guest and the
credential.

- **Why** — This is a deliberate reversal, not an omission, and it is the single
  most misreadable fact in the pivot. The charter's whole broker design existed to
  keep the token out of the guest, and codebox-style containers were explicitly
  rejected for putting it in. D-12 accepts that cost for simplicity: any process
  in the guest — including a compromised dependency or a prompt-injected agent —
  can read and exfiltrate the operator's live OAuth token. Two consequences
  follow. First, a spec or manual that still describes a broker is worse than
  silence, because a reader will size their trust to a protection that no longer
  exists. Second, the boundary that remains is **not** closed by default: F7
  measured `--net-default-egress` defaulting to `deny` **with an implicit
  `allow@public`**, so a sandbox left at the default reaches the public internet
  and can post the token anywhere. An unset profile is therefore an open
  exfiltration path, not a conservative fallback.
- **Fit criterion** — Three checks. (a) A repository gate over `doc/` and any
  shipped manual finds no surviving claim of a placeholder, a broker, a CONNECT
  proxy, or a never-in-guest token; the gate is proven able to fail by planting
  such a claim. (b) A sandbox created by the plugin has an effective network
  profile that is an explicit allowlist, asserted from the created sandbox's own
  configuration rather than from the flag the caller passed. (c) A control
  sandbox created at the library default is shown to reach a public host,
  reproducing F7 and proving the explicit setting in (b) is load-bearing.
- **Verification**: unverified — no test exists. Target method: hybrid — (a) an
  automated repository gate, (b) and (c) automated live against a real
  microsandbox VM.
- **Criticality**: must
- **Anchor** — Target constructs, by name: the network-rule list on the runtime
  create spec in package `internal/core/runtime`, and the documentation claim
  gate. Package `internal/core/runtime` exists; it is not asserted to carry an
  explicitly-set egress default, and the gate does not exist. Anchors are named
  constructs and never line numbers.
- **See also** [MSP-R-001](msp-r-001-credential-live-mount.md),
  [MSP-R-005](msp-r-005-egress-default-deny.md),
  [MSP-R-011](msp-r-011-privilege-posture-recorded.md)
