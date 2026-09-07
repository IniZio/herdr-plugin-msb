---
id: MSP-R-006
type: requirement
concept: C-MSP
title: A sandbox listener on a remote host is reachable from the laptop on the same port and is torn down with the sandbox
pattern: event
verification: unverified
criticality: must
status: active
origin_decision_ref: nexus3-microsandbox-pivot#D-7
---

## MSP-R-006 — A sandbox listener on a remote host is reachable from the laptop on the same port and is torn down with the sandbox {#msp-r-006}

**Build state:** TARGET — obligation not met; fit criterion not run.

**When** a service starts listening on port *N* inside a sandbox running on a
remote host under a `herdr --remote` attach, the plugin **shall** make that
service reachable from the operator's laptop on port *N* — the **same** port
number.

**When** that sandbox stops, the plugin **shall** tear the forward down.

- **Why** — Renumbering is not a cosmetic compromise. A dev server reached on a
  different port breaks Vite HMR, which hard-codes the origin it was served from,
  and breaks any OAuth flow whose registered `redirect_uri` names the port. A
  forward that survives its sandbox is worse than no forward: the port stays bound
  on the laptop and the next sandbox's forward silently fails or, worse, the
  operator's browser reaches a dead tunnel and reads it as an application fault.
- **Fit criterion** — Live, over a real `herdr --remote` attach with real
  OpenSSH ControlMaster. A listener started inside a sandbox on the remote host is
  fetched successfully from the laptop on the identical port number;
  `ssh -O check` reports the control connection live before the sandbox stops and
  the forward absent after it stops.
- **Verification**: unverified — no test exists. Target method: automated live
  against a real `ssh` ControlMaster and a real herdr binary.
- **Criticality**: must
- **Open dependency** — This node states the **outcome contract**, deliberately
  not the discovery mechanism, because the mechanism is an open question. F5
  proved process-ancestry discovery cannot cross a netns in three distinct ways,
  and charter TBD-1 records that microsandbox's userspace stack (smoltcp) may not
  present a host-enterable network namespace at all. If it does not, discovery
  must come from microsandbox's own port-publishing state and the same-port
  contract needs re-examining. Writing the mechanism into this node would produce
  an assertion that outlives whichever mechanism is chosen.
- **Anchor** — Target construct: the forwarding package that applies
  `ssh -O forward -L` / `-O cancel` / `-O check`, plus the sandbox-to-herdr
  binding lookup in the plugin's own store. Not yet present.
