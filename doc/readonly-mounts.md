# Read-only and hardening flags on bind mounts

## The claim that was wrong

`doc/credential-mount.md` recorded, at s16-AC3, that "the read-only alternative
is not expressible". That was inferred from `msb create --help` not listing an
`ro` option. The help text says OPTIONS *"may include"* `quota=` and
`uid=/gid=` — a non-exhaustive phrase, not a closed set. Read-only mounts are
supported at all three layers:

- **CLI** — the third colon-separated segment of `--mount-file` /
  `--mount-dir` is an options list, and `ro` is accepted there:
  `msb run --mount-file "$PWD/probe.txt:/mnt/probe.txt:ro" alpine`.
- **Go SDK v0.6.17** — `MountOptions` carries `Readonly`, `Noexec`, `Nosuid`,
  `Nodev`, `StatVirtualization`, `HostPermissions` and `Owner`.
- **Rust runtime** — the wire form is `:ro,stat-virt=...,host-perms=...`.

So the flag was always available. What was missing was our own plumbing.

## Where the flag was dropped

`internal/runtime/msb/runtime.go` already passed `Readonly: m.ReadOnly` into
`msbsdk.Mount.Bind`. The live-test harness at
`internal/core/credmount/livemsb/harness.go` did not: it built
`--mount-file <host>:<guest>` and discarded `BindMount.ReadOnly` entirely. Every
credential-mount measurement taken through that harness therefore ran read-write
regardless of what the test asked for.

## The four flags

The seam type `runtime.Mount` now carries `ReadOnly`, `Noexec`, `Nosuid` and
`Nodev`, because the SDK exposes them as one group. Each is threaded to the
same `MountOptions` literal. Their individual observed effects are recorded in
the evidence section below; a flag that could not be observed taking effect is
reported as unverified rather than claimed.

## Evidence, 2026-09-08

Measured by `TestLiveMountRoundTrip` in
`internal/runtime/msb/mount_live_test.go`, in one alpine guest with five bind
mounts.

Opposite outcomes, same guest:

- `/work` (`ReadOnly: false`) — `printf 'guest-new\n' > /work/guest.txt` exited
  **0** with empty stderr, and the host file then read `guest-new`.
- `/ro` (`ReadOnly: true`) — `echo fail > /ro/ro.txt` exited **1** with stderr
  `sh: can't create /ro/ro.txt: Read-only file system`, and the host file was
  still `readonly` afterwards.

The guest mount table, verbatim from `/proc/mounts`:

```
nodev_27e8e6b9  /nodev  virtiofs rw,nodev,relatime 0 0
noexec_ae1d39ca /noexec virtiofs rw,noexec,relatime 0 0
nosuid_52a7a370 /nosuid virtiofs rw,nosuid,relatime 0 0
ro_f00f5414     /ro     virtiofs ro,relatime 0 0
work_0c9a453f   /work   virtiofs rw,relatime 0 0
```

Per flag:

- **ReadOnly — proven behaviourally.** The write pair above, plus `ro` in the
  mount table.
- **Noexec — proven behaviourally.** A `chmod 0755` script executed by path
  from `/noexec` exited **126** with `sh: /noexec/probe.sh: Permission denied`;
  the identical script executed from `/work` exited **0** and printed
  `noexec-ran`, so the probe is not vacuous.
- **Nosuid — proven at the mount table only.** `/proc/mounts` shows `nosuid` on
  `/nosuid` and no such flag on `/work`, which discriminates the flag being set
  from it being absent. No behavioural probe was run: creating a setuid binary
  in the guest needs privileges the test does not have.
- **Nodev — proven at the mount table only.** Same shape as nosuid; a
  behavioural probe would need `CAP_MKNOD`.

The wiring itself is held by `TestSandboxOptions_MountHardeningFlags` in
`internal/runtime/msb/mount_options_test.go`, which applies the returned
`SandboxOption` closures to a `SandboxConfig` and reads each flag back from
`cfg.Volumes`. Forcing `Readonly: false` at the construction site makes it fail
with `guest "/ro": Readonly: got false, want true`.

## Recommendation — should the credential store ship mounted `ro`?

This is the operator's call. The two options are not symmetric, and the cost
sits on the `ro` side in a place that is currently unproven.

**Mounting `ro` buys** removal of guest write access to the operator's live
Claude credential. Today a guest can not only read that token but corrupt it:
a peer slice measured `echo SENTINEL > /mnt/creds.json` exiting 0, with the
host bytes changed and the inode unchanged. `ro` closes that.

**Mounting `ro` costs** the guest's ability to write a refreshed token back.
That is the coupling to state explicitly: with a `ro` store, host-side refresh
propagation stops being merely desirable and becomes **required** — the guest
has no other way to obtain a token after the mounted one expires.

**That propagation is currently unproven.** The mid-session refresh half of the
credential bet was never measured: the credential died before any successful
refresh reached a running guest (`doc/credential-mount.md`, s16-AC6). So `ro`
would be trading a measured write-corruption exposure for a dependency on an
unmeasured mechanism.

Two coherent positions follow:

1. **`ro` now.** Treat guest write access to the operator's live credential as
   unacceptable at any price, and accept that sandboxes break at token expiry
   until host-side propagation is proven.
2. **`rw` until propagation is proven, then `ro`.** Keep today's behaviour,
   prove host-side refresh propagation reaches a running guest, and flip to
   `ro` as the last step — at which point the flip costs nothing.

The mechanism is now available either way; this slice does not pick.
