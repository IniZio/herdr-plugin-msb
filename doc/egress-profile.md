# Egress profile

Credentials reach a guest by live volume mount of the host credential store, so a
real OAuth token is present inside every sandbox. The microsandbox network profile
is the only containment left. This document records what was measured, not what the
documentation claims.

## Why a default profile is applied at the seam

`internal/core/netprofile` shipped `Shipped()` and `Apply()` but nothing called them.
`internal/runtime/msb.SandboxOptions` gates `msbsdk.WithNetwork` behind
`networkConfig(spec.NetRules)`, which returns `nil` for an empty rule set, so an
unconfigured spec produced a sandbox with no policy at all — unfiltered public
egress plus a mounted token.

`CreateAndBoot` is the single funnel (`Create` delegates to it). It now calls
`withDefaultNetProfile`, which applies the shipped profile only when the spec
carries no rules of its own, so a caller that configures egress explicitly keeps
its own rules.

## Measured behaviour

A rule list alone does not collapse microsandbox's implicit `allow@public`; an
explicit deny default is mandatory. Measured with the CLI:

| Configuration | raw IP `104.20.23.154:443` | `api.anthropic.com:443` |
| --- | --- | --- |
| no flags | exit 0 (reachable) | exit 0 |
| `--no-net` | exit 1 (blocked) | not probed |
| `--net-default-egress deny` | exit 1 (blocked) | not probed |
| `--net-default-egress deny` + `allow@dns` + `allow@api.anthropic.com:tcp:443` | exit 1 (blocked) | exit 0 (reachable) |

The last row is the policy this package ships, and it is the one the credential
mount depends on: the allowlisted host stays reachable while everything else,
including a raw-IP connection that never consults DNS, is refused.

SDK/CLI equivalence was measured rather than assumed, because microsandbox's
documented default already diverges from its effective behaviour.
`msbsdk.NetworkConfig{DefaultEgress: PolicyActionDeny}` corresponds to
`--net-default-egress deny`, not to `--no-net`, and the SDK serializes the field
on the detached create path. Booting through `internal/runtime/msb` with the
shipped profile reproduces the last row above.

## Enforcement is connection-level

The negative probe uses a raw IP over plain TCP (`nc -z`) precisely so that no
name resolution is involved. A TLS probe against a raw IP is not a valid negative
control: it fails with an SSL handshake alert because the server rejects the
missing SNI, which happens whether or not the packet was allowed through. That
failure mode was observed and the probe was changed to plain TCP.

## Test names must be unique per run

The live test derives its sandbox name from the current time. A fixed name is
reusable in principle, but one run against a name whose earlier incarnation had
been created without a policy reported unfiltered egress, and the mechanism was
never reproduced. A unique name per run removes the variable.
