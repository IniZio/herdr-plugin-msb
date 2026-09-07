---
id: C-HERDR-PLUGIN-MSB
type: concept
title: herdr-plugin-msb
parent: null
summary: "Requirement graph for the microsandbox-backed herdr sandbox plugin that replaces the nexus3 cloud-hypervisor substrate."
status: active
---

# herdr-plugin-msb

herdr-plugin-msb is a thin herdr plugin driving microsandbox v0.6.17 in process
through its native Go FFI SDK. It is a clean-room rebuild of the substrate layer
`nexus3` previously implemented itself on top of cloud-hypervisor.

## Sub-concepts

| Sub-concept | Prefix | Covers |
|---|---|---|
| [C-MSP](msp/README.md) | `MSP-R-*` | Microsandbox-backed runtime: credential mount, egress containment, host mounts, forwarding, surface, seam |

## Goals

- Every acceptance criterion of the `nexus3-microsandbox-pivot` motive traces to
  a requirement node that a gate can be run against.
- No node asserts behaviour that is not yet built. Build state is stated
  explicitly on every node, because an unbadged claim reads as a positive claim.
