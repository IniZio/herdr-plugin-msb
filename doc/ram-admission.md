# RAM Admission

## The hazard

Guest RAM under microsandbox is memfd-backed. Every byte allocated to a running sandbox is
resident in host RAM and cannot be swapped out. Over-commit does not degrade gracefully: it
exhausts host RAM and trips the global OOM killer, which kills dbus and ssh-agent and tears
down the entire login session — including any agent running inside it. A per-sandbox memory
cap bounded total commitment only because the cap was small enough that a single sandbox
could not exhaust the host on its own.

## Operator decision D-23

D-23 raised the per-sandbox cap from 2048 MiB to 8192 MiB to match workloads that require
more headroom for compilation and in-process model inference. That raise is safe only because
an enforced host RAM budget admission check now bounds the aggregate. Without the check the
raise would be a regression: a single sandbox could commit as much RAM as it formerly took
four to consume, and multiple concurrent sandboxes could easily exhaust the host. The
admission check is therefore a hard precondition of D-23, not an optional safety layer on top
of it.

## Where the budget comes from

The budget is read from the environment variable `HERDR_MSB_HOST_RAM_BUDGET_MIB`. When the
variable is absent or empty the budget defaults to `admission.DefaultHostBudgetMiB`, which is
8192 MiB.

That default is conservative by design. The development host measures 31200 MiB total with
roughly 6300 MiB already in use; 8192 MiB is about a quarter of total host RAM, leaving the
host, the desktop session, and the build and test toolchain ample unswappable headroom. The
default is a floor chosen for safety, not a measurement of what the host could bear. Operators
on larger hosts raise it deliberately by setting the env var; they should not need to on the
development host.

## How committed memory is measured

Committed memory is the sum of each existing sandbox's configured memory, read from the
microsandbox daemon's own sandbox records via the Go SDK. It is not a guess, not a reading of
host free memory, and not a count of running sandboxes multiplied by an assumed size.

The daemon record is the right source because it reflects what was actually committed at create
time, across all sandboxes — including sandboxes this process did not create. A host free-memory
reading would undercount commitment during light load and would also fail to account for non-sandbox
consumers.

## Fail-closed semantics

If committed memory cannot be determined — the daemon is unreachable, or the runtime exposes no
`Accountant` implementation — the create is refused with `admission.ErrCommittedUnknown`. It is
not allowed.

The specific trap this guards against: an accountant returning `(0, err)` must never be read as
"0 MiB committed, plenty of room." The error is the signal; the zero is noise. Any error from the
accountant is treated as unknown committed memory and the create is refused.

Similarly, if no `Accountant` implementation is wired up at all — `acct == nil` — the create is
refused. An unimplemented interface is not evidence of zero commitment.

## Enforcement points

The check is applied on the create path before the sandbox is created. An advisory warning after
the fact would not satisfy D-23: the hazard is the act of creation, not the observation that
creation happened.

The check is applied at three points, and this is deliberate rather than accidental duplication:

- `msb.(*Runtime).CreateAndBoot`, immediately before `msbsdk.CreateSandbox`. This is the
  authoritative point. `(*Runtime).Create` delegates to `CreateAndBoot` and inherits it, and
  `internal/mcp` calls `CreateAndBoot` on the runtime directly without passing through the
  service layer, so a service-only check would leave the MCP path uncovered.
- `msb.(*Runtime).RunEphemeral`, which calls `msbsdk.CreateSandbox` directly rather than routing
  through `CreateAndBoot`. `RunEphemeral` once shipped without the network profile for exactly
  this reason; it needs its own call or it bypasses admission entirely.
- `service.(*Service).Create`, after `Spec` and before either runtime call, so the CLI refuses
  early with a legible error.

Note the contrast with the network profile, which has a single application point in
`SandboxOptions` (see CLAUDE.md's security posture section). That constraint exists because the
network profile *configures* the sandbox, and a second application site is how a call path ends
up configured differently — that is what let `RunEphemeral` ship unprotected. Admission does not
configure anything; it only refuses. A redundant refusal is not a divergence risk, whereas a
missing one is the whole hazard. The asymmetry is the point: for configuration, prefer one site;
for refusal, cover every site that can create.

The two runtime sites are held by AST guard tests that parse the source and fail unless
`admission.Admit` appears before `msbsdk.CreateSandbox` in that function. The service site is
held behaviourally: its refusal tests assert not only that an error is returned but that the
underlying runtime create was never invoked, which is what distinguishes an enforced check from
an advisory one.

## Proving it

A guard that has never been observed refusing is not a proven guard. Acceptance requires three
cases: one create admitted under budget, one refused for exceeding the budget, and one refused
because committed memory could not be determined — each with real captured output showing the
expected outcome.

The refusal cases are proven by injecting committed-memory figures through the `Accountant`
interface with a small configured budget in unit tests: one run where `committed + requested`
stays under the budget, and one where it does not. Booting actual sandboxes to test an OOM guard
would be precisely the failure the guard exists to prevent.

## Stopped sandboxes and the conservative count

This check counts every daemon-reported sandbox regardless of status, because SDK v0.6.17 offers
no per-status guarantee. `doc/stop-start.md` records a host-side measurement of what a stopped
sandbox actually costs: the `libkrun VM` process exits on `stop`, so its guest RAM — 472 MB of
`RssAnon` in the measured case — is fully returned to the host. A stopped sandbox costs zero host
RAM, and there is no "paused" case, because the runtime has no pause. The count remains
conservative by choice; the data is on record so the trade can be revisited deliberately.
