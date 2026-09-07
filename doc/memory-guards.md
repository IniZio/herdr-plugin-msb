# Memory Guards

The CAPPED macro in the Makefile wraps every build and test command in four guards:

**GOMAXPROCS exported into the environment.** The only guard that reaches nested toolchains. Tests that shell out to `go build` spawn a fresh toolchain whose default `-p` is GOMAXPROCS; it inherits nothing from the outer `go test -p`. On 2026-08-30 the most conservative `-p 1 -parallel 1` setting still reached 147 concurrent linkers at ~195 MB each and exhausted 30 GB of RAM plus 8 GB of swap, because the tests themselves launched uncapped toolchains.

**-p / -parallel caps.** Bound how many packages and tests run concurrently at the Go level.

**choom -n 1000.** Maximizes this process tree's OOM score so the kernel kills it before session infrastructure (dbus, ssh-agent). Without this, an OOM can tear down the login session and any agent running inside it.

**systemd-run --user --scope with MemoryHigh and MemoryMax.** Bounds the entire tree inside a transient cgroup so a runaway suite is throttled then killed before reaching a global OOM. ManagedOOMPreference=avoid is required: user@.service ships ManagedOOMMemoryPressure=kill with a 60%/20s PSI limit, and a transient scope inherits it. MemoryHigh throttling is precisely what drives cgroup PSI over that limit, so without avoid, systemd-oomd kills the scope well before MemoryMax — observed 2026-08-30 as "systemd-oomd killed 500 process(es) in this unit" roughly 90s into a test run at a MemoryMax the suite never reached.

**Fail-closed behaviour.** An earlier nexus3 version fell back to an uncapped run when the systemd user manager was unavailable. That is backwards: the probe only fails when the machine is already thrashing. The fallback let a run reach 28G with swap 100% full. Now: if the scope cannot be created, the run does not start. Set HERDR_MSB_ALLOW_UNCAPPED=1 in CI environments with no user systemd instance.

**-count=1 on test.** Go caches successful test results. -race does not defeat caching — it is a build flag consumed before the binary runs, so it never reaches the cacheability scan. Without -count=1 the test target can exit 0 having run nothing, hiding real failures behind a cached green.
