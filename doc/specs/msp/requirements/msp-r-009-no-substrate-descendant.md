---
id: MSP-R-009
type: requirement
concept: C-MSP
title: No descendant of the deleted substrate and no runtime selector
pattern: unwanted
verification: unverified
criticality: must
status: active
origin_decision_ref: nexus3-microsandbox-pivot#D-8
---

## MSP-R-009 — No descendant of the deleted substrate and no runtime selector {#msp-r-009}

**Build state:** TARGET — obligation not met; fit criterion not run. The
repository is new and small, so the obligation is currently easy to satisfy and
correspondingly easy to violate without noticing.

This repository **shall not** contain a descendant of the substrate packages the
pivot deletes — the cloud-hypervisor driver, the per-sandbox supervisor, the
builder, the in-guest agent, the memory governor, the volume store, the image and
resize packages, and the substrate-welded netfilter, netstack and SNI packages —
nor a descendant of the deleted credential path (`perimeter/cred`,
`perimeter/mitm`).

This repository **shall not** carry a runtime selector that names a
non-microsandbox runtime, and `make typecheck`, `make vet` and `make test`
**shall** be green with only the microsandbox runtime present.

- **Why** — The pivot's value is the ~29,400 non-test lines it stops maintaining;
  a clean-room repo that grows a second runtime path has bought the beta-dependency
  risk without the maintenance saving. The prohibition on a credential-path
  descendant is separate and is not redundant with MSP-R-001: D-12 states that
  nothing from `cred/` or `mitm/` is ported, and a partial port would put a
  placeholder-swapping component back into a design whose containment story no
  longer mentions one, which is the most confusing possible half-state.
- **Fit criterion** — A repository check enumerates the package list and fails on
  any package matching the F1 delete list or the deleted credential path, and on
  any runtime-selection construct offering a choice of runtime; `make typecheck`,
  `make vet` and `make test` exit 0 in the same run.
- **Verification**: unverified — no check exists. Target method: automated.
- **Criticality**: must
- **Scope limit, stated because a forgotten obligation reads as complete** —
  Charter AC-8 also requires those packages to be **physically removed from the
  nexus3 tree** with nexus3's own `make` targets green. That half cannot be
  asserted by a node in this repository's graph and is **not** covered here.
  Charter slice `s11-substrate-deletion` owns it, and it needs a node in the
  nexus3 spec graph before AC-8 is fully traced.
- **Anchor** — Target construct: the package-list check. Not yet present. The
  `Makefile` targets it must run alongside (`typecheck`, `vet`, `test`) do exist.
