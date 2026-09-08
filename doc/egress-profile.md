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

This document previously claimed "`CreateAndBoot` is the single funnel (`Create`
delegates to it)". **That claim was false.** `SandboxOptions` (`runtime.go:67`)
is the true funnel: it is the sole builder of `msbsdk.WithNetwork`, so every create path
that reaches it gets the network option. `CreateAndBoot` (`runtime.go:190`) and
`Create` (which delegates to `CreateAndBoot`) both route through `SandboxOptions`.
So does `RunEphemeral` (`exec.go:28`): it calls `SandboxOptions(spec)` directly
and then `msbsdk.CreateSandbox` at `exec.go:33`, never touching
`withDefaultNetProfile` (`runtime.go:182`), which was only called from
`CreateAndBoot`. That made `RunEphemeral` a live bypass of the default profile.
The bypass was measured: an ephemeral run against an unconfigured spec reached
`https://example.com/` with `exit=0` and returned real Example Domain HTML.

The fix places default-profile application inside `SandboxOptions` itself, the
one location that constructs `msbsdk.WithNetwork`, so bypass is structurally
impossible regardless of which create path is used. A per-call-site fix was
rejected because it leaves any future create path free to bypass the profile
again. `CreateAndBoot`'s now-redundant `withDefaultNetProfile` call is removed.

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

## Post-create effective-policy assertion

Enforcement is now a measurement rather than an inference. After each create,
the production create path itself reads the sandbox's effective policy back and
**fails the create** if the policy is absent or does not carry a deny-default,
tearing the sandbox down before returning — an unprotected sandbox may already
hold a mounted credential, so it must never be handed back. Both create paths
assert: `CreateAndBoot` (`runtime.go:197`) and `RunEphemeral` (`exec.go:38`).
This closes the gap where a call site being present was taken as proof the
profile was applied.

The read goes to the daemon's persisted record, `(*SandboxHandle).ConfigJSON()`,
which is `ffi.SandboxHandleInfo.ConfigJSON` from the `LookupSandbox` RPC — server
state, not a client-side echo of the create request. The key path is
`network.policy.default_egress`.

**Do not use `(*SandboxHandle).Config().Network` for this.** It is structurally
always nil in SDK v0.6.17: `SandboxConfig.UnmarshalJSON` (`options.go:163-239`)
decodes into a `persistedSandboxConfig` that has no network field and then
rebuilds `SandboxConfig` field by field, never assigning `Network`. Measured on
a live sandbox whose policy was demonstrably present and agreed by `msb inspect`:
`Config() err=<nil> Config().Network=(*microsandbox.NetworkConfig)(nil)`. An
assertion built on that field would have failed every create, and one built on
its zero value would have passed vacuously forever.

That gap matters because one live run observed unfiltered egress despite the
call site being present. Three hypotheses were investigated: sandbox-name reuse
carrying a stale policy from a prior run; a race between the create path and
the network-profile write; and the probe itself being a measurement artefact
rather than a real bypass. Each was ruled out by experiment. The mechanism was
never reproduced and remains unexplained. The post-create assertion is the
response: rather than relying on inference from the call graph, enforcement is
confirmed by measurement on every create.

## Test names must be unique per run

The live test derives its sandbox name from the current time. A fixed name is
reusable in principle, but one run against a name whose earlier incarnation had
been created without a policy reported unfiltered egress, and the mechanism was
never reproduced. A unique name per run removes the variable.
