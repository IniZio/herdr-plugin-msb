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
variable is absent or empty the budget is **derived from the host's real RAM**:
`admission.DefaultBudgetMiB()` reads `MemTotal` from `/proc/meminfo` and returns
`MemTotal / HostBudgetDivisor`, with `HostBudgetDivisor = 4` — **one quarter of total host
RAM**. The env override is unchanged and still wins outright: a set, non-empty, positive
integer is used verbatim, whatever the host's RAM.

### Why one quarter

The quarter is anchored on the figure the previous absolute constant encoded. The development
host measures 31200 MiB total with roughly 6300 MiB already in use, and the old constant of
8192 MiB was about a quarter of that — a ratio that had been lived with and had proved to leave
the host, the desktop session, and the build and test toolchain ample unswappable headroom.
Making the ratio the rule rather than the arithmetic accident preserves that behaviour on this
host while making it scale.

The headroom argument is what fixes the fraction, and it is a *fraction* argument, not an
absolute one: guest RAM is unswappable, so the budget must leave room for the host kernel, the
desktop session, the browser and editor an operator actually works in, and the Go toolchain,
all of which grow roughly with the size of the machine rather than staying at a fixed number of
MiB. A half would leave a 16 GiB laptop with 8 GiB for everything else including page cache —
survivable only if nothing else is running. A quarter leaves three quarters, which absorbs both
the resident working set and the check-then-create race noted under *Two stated limits*.
It is deliberately not a measurement of what a host could bear; it is a floor chosen for
safety, and operators who know their machine raise it with the env var.

On this host the derived default is `31949300 kB / 1024 / 4 = 7800 MiB`, against the previous
8192 MiB — a 392 MiB tightening, and still far above the ~3072 MiB currently committed by the
two running sandboxes. On an 8 GiB host it is 2048 MiB, where the old constant was 8192 MiB and
therefore admitted right up to and past total RAM.

### When `MemTotal` cannot be read

If `/proc/meminfo` is absent, unreadable, carries no `MemTotal` line, or carries one that does
not parse (including a zero or implausible value), `DefaultBudgetMiB()` returns
`admission.FallbackHostBudgetMiB`, which is **2048 MiB**.

Falling back to a *small* floor rather than refusing outright is the deliberate choice, and the
size is what makes it safe. Refusing outright was considered: it is the strictest reading of
fail-closed, but it turns any host without a procfs `MemTotal` — a non-Linux host, a stripped
container, a hardened mount namespace — into one where no sandbox can be created at all, and
the pressure that creates is for an operator to set `HERDR_MSB_HOST_RAM_BUDGET_MIB` to whatever
number makes the error go away, which is a worse outcome than a small honest default.

What must never happen is falling back to a *large* constant, because the machines where
`MemTotal` is unavailable are not correlated with machines that have RAM to spare — that is
exactly the defect being fixed, reintroduced on the unreadable path. 2048 MiB is below the
quarter-budget of any host of 8 GiB or more, so on every host where the fallback fires the
guard refuses strictly more than a correct reading would have. It is also below
`MaxSandboxMemoryMiB` (8192), so a single maximum-size sandbox is refused outright under the
fallback. Fail-safe here means refusing more, never admitting more.

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

## Observing the refusal tests RED — the FFI-path technique

The four behavioural refusal tests (`TestCreateAndBoot_AdmissionRefused`,
`TestCreateAndBoot_FailClosedAccountantError`, `TestRunEphemeral_AdmissionRefused`,
`TestRunEphemeral_FailClosedAccountantError`) shipped without ever being observed failing.
That is the hollow-proof shape this repo keeps catching: with admission removed the tests fall
through to `msbsdk.CreateSandbox`, so the obvious negative control risks contacting the daemon
and booting a VM — which is the one thing an OOM guard's test must not do.

The safe negative control exploits the SDK's own loader. Under build tag
`microsandbox_ffi_path` the SDK reads its FFI library from `$MICROSANDBOX_FFI_PATH`
(`sdk/go@v0.6.17/internal/bundle/bundle_path.go`), and the load is lazy — it happens at the
first FFI call, not at package init. Pointing the variable at a nonexistent file makes
`CreateSandbox` fail at library load, before any daemon contact and long before any VM.

Invocation (single package via `-run`; never over `./...` with a live daemon reachable):

    MICROSANDBOX_FFI_PATH=/nonexistent/no-ffi.so \
      make test GOTEST_P=1 GOTEST_PARALLEL=1 \
        GOTEST_ARGS='-tags microsandbox_ffi_path -v -run "TestCreateAndBoot_|TestRunEphemeral_"'

Two runs with opposite outcomes, both observed, with `msb list` reporting the same single
`herdr--eyeball` sandbox before and after:

- **Admission intact** — all four PASS:

      --- PASS: TestCreateAndBoot_AdmissionRefused (0.00s)
      --- PASS: TestCreateAndBoot_FailClosedAccountantError (0.00s)
      --- PASS: TestRunEphemeral_AdmissionRefused (0.00s)
      --- PASS: TestRunEphemeral_FailClosedAccountantError (0.00s)
      ok  github.com/IniZio/herdr-plugin-msb/internal/runtime/msb 1.011s

- **Admission neutralised** at both call sites — each `if err := admission.Admit(...); err != nil
  { return ..., err }` replaced by `_ = admission.Admit(...)` — all four FAIL, and the failure
  message proves execution reached `CreateSandbox` and stopped at library load rather than at the
  daemon:

      admission_test.go: want ErrBudgetExceeded, got msb: create sandbox "testbox":
        microsandbox: read MICROSANDBOX_FFI_PATH=/nonexistent/no-ffi.so:
        open /nonexistent/no-ffi.so: no such file or directory
      --- FAIL: TestCreateAndBoot_AdmissionRefused (0.00s)
      --- FAIL: TestCreateAndBoot_FailClosedAccountantError (0.00s)
      --- FAIL: TestRunEphemeral_AdmissionRefused (0.00s)
      --- FAIL: TestRunEphemeral_FailClosedAccountantError (0.00s)

Restoring both call sites returns all four to PASS. Note that the `_ =` mutation also fails the
two AST guard tests in `admission_guard_test.go`, which is why the negative control is scoped
with `-run` to the four behavioural tests.

## Paging the daemon's sandbox list

`CommittedMemoryMiB` sums over a cursor-paginated listing. The loop body is factored into
`sumCommittedMiB`, which takes a page fetcher, so every termination condition is unit-testable
without the SDK — `*msbsdk.SandboxHandle` has only unexported fields and no public constructor,
so a fake page cannot be built from a test otherwise.

An unbounded `for { ... cursor = page.NextCursor }` makes the daemon's listing a liveness
dependency of every create: a daemon returning a fixed cursor hangs each create forever. Four
conditions bound it:

- **Iteration bound.** At most `maxCommittedPages` (1000) pages.
- **Unchanged-cursor detection.** A page whose `NextCursor` equals the cursor just sent ends the
  loop.
- **Context check.** `ctx.Err()` is consulted at the top of each iteration, so a cancelled or
  expired context ends the loop before another fetch.
- **Saturating sum.** `total += mib` is unchecked `uint32` arithmetic; a hostile or corrupt set of
  records could wrap it to a small number. The sum saturates at `math.MaxUint32` instead.

The first three end the loop by returning an **error**, not by returning the partial sum. This is
the same fail-closed rule as the rest of this document: a partial sum is an under-count, an
under-count admits more than the budget allows, and that is precisely the hazard. Saturation goes
the other way for the same reason — over-counting refuses, wrapping admits.

Each condition has a test in `admission_test.go`, and each was proven able to fail by mutating
exactly one condition out of `admission.go` and observing that exactly one test went red:
removing the `ctx.Err()` check failed only `TestSumCommittedMiB_ContextCancelledRefused`; raising
the bound tenfold failed only `TestSumCommittedMiB_PageBoundRefused` (`fetched 10000 pages, want
the bound 1000`); removing the cursor comparison failed only
`TestSumCommittedMiB_RepeatedCursorRefused`, which then fell through to the page bound; and
replacing the saturating add with `total += mib` failed only
`TestSumCommittedMiB_SaturatesInsteadOfWrapping` (`got 3995 MiB, want saturation at 4294967295
MiB`).

## Stopped sandboxes and the conservative count

This check counts every daemon-reported sandbox regardless of status, because SDK v0.6.17 offers
no per-status guarantee. `doc/stop-start.md` records a host-side measurement of what a stopped
sandbox actually costs: the `libkrun VM` process exits on `stop`, so its guest RAM — 472 MB of
`RssAnon` in the measured case — is fully returned to the host. A stopped sandbox costs zero host
RAM, and there is no "paused" case, because the runtime has no pause. The count remains
conservative by choice; the data is on record so the trade can be revisited deliberately.

## Two stated limits

**The check-then-create race is not closed.** `CommittedMemoryMiB` reads the daemon's current
sandboxes; it takes no reservation, and nothing holds a lock between the admission check and the
create that follows it. Two concurrent creates can therefore both observe the same committed
figure and both pass a budget that admits only one. The worst case is overshooting the budget by
a single sandbox, and creates here are operator- or hook-driven and so effectively serialized.
This is a stated assumption of the design, not a promise to fix.

**The budget ignores what the host is already using.** The default is a fraction of `MemTotal`,
not of free memory, so it does not shrink when the operator's own browser, editor and toolchain
grow. The three-quarters left unbudgeted is the whole of the allowance for that, and it is a
static allowance. A host already deep into its RAM before any sandbox starts can still admit up
to a quarter of total; measuring `MemAvailable` instead was rejected because it makes admission
depend on a figure that moves between the check and the create, and a budget that shifts under
the caller is harder to reason about than one that is conservative and fixed. Operators who
routinely run the host hot should lower `HERDR_MSB_HOST_RAM_BUDGET_MIB` explicitly.

Superseded: the default budget was formerly an absolute 8192 MiB constant unrelated to the RAM
of the machine, which made the guard inert on any host of 8 GiB or less — it admitted right up
to and past total RAM exactly where over-commit bites hardest. Deriving the default from
`MemTotal` (above) closes that.
