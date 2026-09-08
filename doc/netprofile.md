# The egress profile — `internal/core/netprofile`

This package computes the guest egress allowlist that gets threaded into
`SandboxSpec.NetRules`. The rationale lives here rather than in Go doc comments
because this repo caps comment density at 5 comment lines per 100 in source files.

## Why this is a security control, not a convenience

The CONNECT-proxy credential broker was replaced by a live volume mount of the
host credential store. The previous design swapped a placeholder for the real
token per request, so the real credential never entered the sandbox. That is
gone. The operator's real Claude OAuth token now sits inside the guest,
readable by anything running there.

The network profile is therefore the only thing between a compromised in-guest
agent and exfiltration of a live credential. It was defence-in-depth before that
change; it is now the whole defence.

## An empty rule set is an OPEN policy

`msb run --help` states that `--net-default-egress` defaults to `deny` "with an
implicit `allow@public` rule when no other rules are present". Read carefully:
deny is *not* the effective default state. A sandbox created with no network
flags reaches the entire public internet.

Measured on microsandbox 0.6.17, default sandbox, no network flags:

| Case | Target | Result |
|---|---|---|
| DNS | `nslookup api.anthropic.com` | resolves, exit 0 (`160.79.104.10`) |
| HTTPS | `https://api.anthropic.com/` | reachable — `HTTP/1.1 404 Not Found` |
| HTTPS | `https://example.com/` | reachable — `HTTP/1.1 200 OK`, exit 0 |
| HTTP | `http://example.com/` | reachable, exit 0 |
| host group | `host.microsandbox.internal:54273` | blocked, exit 1 (connection refused) |
| private group | `192.168.0.103:54273` (RFC1918 LAN) | blocked, exit 1 (connection refused) |

So the `host` and `private` groups are closed by default, but `public` is open.
That is the exposure the credential delivery change turned into a credential-exfiltration path.

Consequently this package treats an empty allowlist as a programming error rather
than as "no policy": `Rules` returns `ErrEmptyConfig` for a zero `Config`, and
`Closed()` returns an explicit deny rule instead of an empty slice. A zero-length
`[]runtime.NetRule` must never reach the runtime, because
`internal/runtime/msb`'s `networkConfig` returns `nil` for an empty rule set,
`WithNetwork` is then never applied, and the guest falls back to microsandbox's
implicit full-public-egress default.

## Allow rules alone do NOT create an allowlist

The most important measured finding, and the one the help text misleads on.

Adding `--net-rule` entries does **not** collapse the implicit `allow@public`:

    msb run --net-rule "allow@api.anthropic.com:tcp:443" --net-rule "allow@dns" \
      alpine -- wget -T5 -q -O- https://example.com/
    # exit 0 — example.com STILL REACHABLE

An explicit deny default is mandatory. With `--no-net` (sugar for
`--net-default deny`) added, the same allowlist behaves correctly:

| Case | Target | Result |
|---|---|---|
| negative | `https://example.com/` | blocked, exit 1 |
| negative, raw IP | `104.20.23.154:443` | blocked, exit 1 |
| negative, wrong port | `api.anthropic.com:80` | blocked, exit 1 |
| positive control | `api.anthropic.com:443` | reachable — `HTTP/1.1 404` |
| positive control | DNS for the allowlisted name | resolves, exit 0 |

The raw-IP case matters on its own: `allow@api.anthropic.com:tcp:443` binds to the
domain, not to the resolved address, so the deny is enforced on the connection and
is not merely a name-resolution block. A name-only block would be trivially
bypassed by an adversarial guest — the same reasoning that made nexus3 choose
transparent SNI interception over `HTTPS_PROXY`, since an adversarial guest will
not honour `HTTPS_PROXY`.

The minimal correct flag set is therefore:

    --no-net --net-rule "allow@api.anthropic.com:tcp:443" --net-rule "allow@dns"

## Why the allowlist is config-derived

`DefaultConfig` is a declared data table, not a literal assembled inside the
function that emits rules, and `Rules` accepts any `Config`. The allowlist is data
the operator can change; no call site hardcodes a hostname, and a test can assert
on `DefaultConfig` independently of the emit logic. `api.anthropic.com` on port 443
is in the shipped table because the product requires the guest to reach the Claude API.

`Shipped()` names the one profile the product actually applies, and `Apply` stamps
it onto a `SandboxSpec`. Both exist so a test can assert on the shipped policy as a
single named thing. A policy the code can express but never sets is a threaded
parameter left at the off value — a documented failure mode in this codebase.

## Seam note

`runtime.NetRule.Host` is a bare `string` with no discriminator between a domain, a
CIDR, a group target (`public`), and a suffix (`*.example.com`). The seam types were
left unchanged. `internal/runtime/msb`'s `networkConfig` passes `Host` straight
through as the SDK `Destination`, so a group sentinel only works because
microsandbox's own grammar accepts `public` as a target.

An earlier draft of `Rules` appended a trailing `deny@public` rule. It was removed:
`networkConfig` already sets `DefaultEgress: PolicyActionDeny` whenever any rule is
present, so the trailing deny was redundant, and because explicit rules take
precedence over profiles it risked shadowing the allow rule depending on evaluation
order.

## SDK/CLI equivalence — proven

The measurements above used the `msb` **CLI** directly. The product ships through
the **Go SDK** (`msbsdk.NetworkConfig{DefaultEgress: PolicyActionDeny}` plus
per-rule `PolicyRule` entries). SDK/CLI equivalence was not assumed: live
acceptance testing booted sandboxes through `internal/runtime/msb` with
`netprofile.Shipped()` and reproduced two-outcome egress containment:
`8.8.8.8:443` blocked (exit 1), `api.anthropic.com:443` allowed (exit 0).
The product path and the CLI measurements are confirmed equivalent for the
shipped profile.

## Host privilege requirement

microsandbox requires read-write access to `/dev/kvm` and offers no rootless
path. The runtime virtualises guests through libkrun, which uses the KVM
accelerator directly. There is no user-namespace-based isolation and no gvisor
netstack over a TAP file descriptor.

The previous product used a rootless-userns model with gvisor netstack, which
provided zero-networking-privilege: a guest escape reached only the network
namespace granted at start time. That property is not present here. Containment
rests on the sandbox network profile described above, not on host privilege
separation.
