---
id: MSP-R-005
type: requirement
concept: C-MSP
title: Egress is an explicit allowlist and the negative case is the test
pattern: unwanted
verification: unverified
criticality: must
status: active
origin_decision_ref: nexus3-microsandbox-pivot#D-12
---

## MSP-R-005 — Egress is an explicit allowlist and the negative case is the test {#msp-r-005}

**Build state:** TARGET — obligation not met; fit criterion not run.

**If** a process inside a sandbox dials a host that is not on the sandbox's
allowlist, **then** the microsandbox network policy **shall** block the
connection.

The plugin **shall** configure that allowlist explicitly at create time and
**shall not** treat the library's default egress setting as a deny posture.

- **Why** — Under D-12 this is the only containment left. The real OAuth token
  sits inside the guest, readable by anything running there, so the network
  profile is what stands between a compromised guest and exfiltration of the
  operator's live credential. The charter's original AC-4 qualified the test as "a
  guest that **ignores** `HTTPS_PROXY`", which is moot now that D-12 sets no
  proxy at all — but the negative case it was protecting is now the entire
  requirement rather than a corner of it. F7 supplies the second half: measured on
  microsandbox 0.6.17, `--net-default-egress` defaults to `deny` **with an
  implicit `allow@public`**, so a sandbox created without an explicit profile
  reaches the public internet. Inferring containment from the word "deny" in the
  flag's default is exactly the reading F7 falsifies, which is why this must be
  tested as a negative case and not derived from the rule grammar.
- **Fit criterion** — Live, real microsandbox VM. (a) From inside a sandbox
  created by the plugin, a connection to a host absent from the allowlist fails;
  (b) a connection to a host present on the allowlist succeeds, so (a) is not
  passing because the guest has no network at all; (c) a control sandbox created
  at the library default reaches a public host, reproducing F7 and proving the
  explicit configuration in the plugin's path is what produces the deny.
- **Verification**: unverified — no test exists. Target method: automated live.
  The positive control in (b) and the F7 control in (c) are both required: a
  negative-only assertion passes against a sandbox with no networking.
- **Criticality**: must
- **Anchor** — Target construct: the egress policy translation that emits
  microsandbox network rules from the plugin's policy definition. Not yet present.
- **See also** [MSP-R-002](msp-r-002-credential-in-guest-containment.md)
