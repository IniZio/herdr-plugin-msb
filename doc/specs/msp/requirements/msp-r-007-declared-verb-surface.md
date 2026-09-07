---
id: MSP-R-007
type: requirement
concept: C-MSP
title: The shipped verb set is declared and every absent nexus3 verb is named
pattern: ubiquitous
verification: unverified
criticality: must
status: active
origin_decision_ref: nexus3-microsandbox-pivot#D-13
---

## MSP-R-007 — The shipped verb set is declared and every absent nexus3 verb is named {#msp-r-007}

**Build state:** TARGET — obligation not met; fit criterion not run.

The plugin **shall** carry a declared enumeration of the CLI verbs it ships, and
that enumeration **shall** be asserted against the command registry so a verb
cannot appear or disappear without the declaration changing.

Every verb present in the captured nexus3 surface baseline and **absent** from
the shipped set **shall** be listed by name as out of scope. No verb **shall**
disappear silently.

- **Why** — D-13 drops CLI parity as a goal, so the charter's AC-6 obligation
  "every one of the 29 verbs is either preserved or listed with a written reason"
  is no longer reachable and would make minimality unreachable by construction if
  it were kept. What survives the decision is the property that made AC-6 worth
  having: a verb dropped on purpose is indistinguishable from a verb forgotten
  unless the full old list exists somewhere and the absences are named against it.
  The baseline was measured at 37 registry entries and is retained as a
  **reference list**, not a parity target. Without the named-absence half, a
  minimal surface is a claim rather than a decision.
- **Fit criterion** — A test enumerates the command registry and fails on any
  divergence from the declared verb set, in either direction. A second check
  asserts that the set difference between the retained nexus3 baseline and the
  shipped set is fully enumerated in the out-of-scope list, with no unlisted
  remainder. The registry-divergence test is proven able to fail by adding an
  undeclared verb.
- **Verification**: unverified — no test exists. Target method: automated.
- **Criticality**: must
- **Anchor** — Target constructs: the command registry enumeration in the CLI
  package, the declared verb-set fixture, and the named out-of-scope list. None
  exist yet. The nexus3-side baseline was captured and committed in the nexus3
  repository, not here.
