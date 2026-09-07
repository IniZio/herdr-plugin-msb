# MCP tool surface

## Advertised tools

| Tool | Seam method |
|---|---|
| `sandbox_create` | `CreateAndBoot` |
| `sandbox_list` | `List` |
| `sandbox_start` | `Start` |
| `sandbox_stop` | `Stop` |
| `sandbox_remove` | `Remove` |
| `sandbox_exec` | `Exec` |

## Named omissions

**Pause / Resume** (D-14): microsandbox v0.6.17 has no pause or resume API. The adapter returns `ErrUnsupported` for both. Advertising a tool that always fails is worse than omitting it.

**RunEphemeral**: not exposed. It is a convenience composite (create → exec → remove) that belongs in a higher-level orchestration layer, not on the primitive MCP surface.

## Excluded parameters (D-9)

`rootfs_path`, `memory_mib`, and `nested_virt` are absent from every advertised schema. Passing any of them returns an explicit error naming the rejected parameter. These are substrate details that callers must not depend on.
