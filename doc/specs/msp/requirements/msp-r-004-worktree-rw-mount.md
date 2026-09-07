---
id: MSP-R-004
type: requirement
concept: C-MSP
title: A host git worktree is mounted read-write and edits round-trip both ways
pattern: event
verification: unverified
criticality: must
status: active
origin_decision_ref: nexus3-microsandbox-pivot#D-3
---

## MSP-R-004 — A host git worktree is mounted read-write and edits round-trip both ways {#msp-r-004}

**Build state:** TARGET — obligation not met; fit criterion not run.

**When** a sandbox is created for a herdr workspace backed by a host git
worktree, the plugin **shall** mount that worktree into the guest **read-write**
over virtio-fs, such that a file written inside the guest is visible on the host
and a file written on the host is visible inside the guest, with no copy-in or
copy-out step.

- **Why** — This is the whole herdr worktree workflow, and the old substrate did
  it by capturing a copy of the source into the VM's disk, which meant host edits
  and guest edits diverged and had to be reconciled by hand. A live rw mount is
  the reason the pivot can drop ~5,900 non-test lines of builder and image code
  rather than porting them. Stating only one direction would be enough to pass a
  test while leaving the workflow broken: the agent edits inside the guest, and the
  operator edits in their editor on the host, in the same session.
- **Fit criterion** — Live, real microsandbox VM, real git worktree. A file
  created inside the guest under the mount appears on the host with the same
  content; a subsequent host-side edit to that file is read back inside the guest;
  `git status` run on the host sees the guest's change as a working-tree
  modification.
- **Verification**: unverified — no test exists. Target method: automated live.
- **Criticality**: must
- **Anchor** — Target construct: the mount specification list on the runtime
  create request in `internal/core/runtime`, and its translation to a
  microsandbox mount in `internal/runtime/msb`. Not yet present.
