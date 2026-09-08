# `stop` and `start`

Operator decision D-22: these two verbs are honest. The microsandbox runtime cannot suspend and
resume a guest, so no verb here claims it can. `pause` and `resume` are not shipped verbs — the
dispatcher rejects them with exit 2, and `internal/runtime/msb/lifecycle.go` implements `Pause`
and `Resume` as stubs returning `ErrUnsupported`.

## What each verb actually does to the guest

`stop` tears the guest down. The `libkrun VM` host process exits. Everything the guest held in
RAM, in `tmpfs`, or in `/dev/shm` is gone. The sandbox record survives in the daemon with status
`stopped`, and the sandbox↔herdr workspace binding survives (see below).

`start` boots the guest again from its image. It is a fresh boot, not a resume: the new guest has
a new `libkrun VM` process and none of the previous guest's in-memory state.

`rm` refuses a running sandbox — `sandbox still running: cannot remove sandbox '…'`. The sequence
is `stop` then `rm`. Unlike `stop`, `rm` deletes the binding and closes the herdr workspace.

The asymmetry lives in one flag. `runStop` calls `spaceCleanup(…, closeWorkspace=false)` and
`runRM` passes `true`; `spaceCleanup` returns immediately when the flag is false, which is what
makes the binding survive a stop.

## Evidence: guest state does not survive a round trip

Measured against a live sandbox (`herdr/s53probe`, alpine, 1024 MiB) with the real `herdr` binary
at `~/.local/bin/herdr` — not a fake injected through `HERDR_BIN_PATH`.

A 400 MiB file was written to the guest's `/dev/shm`, then the sandbox was stopped and started.
After the restart the file was absent: `ls: /dev/shm/blob: No such file or directory`. If `stop`
were a suspend, that file would still be there. This is the direct disproof of pause semantics.

## Evidence: the herdr workspace binding survives stop→start

This closes the s39 fog item. The s36 binding fix had only ever been exercised against a fake
`herdr` binary injected via `HERDR_BIN_PATH`, which proves nothing about the real one.

Read back from herdr's own state (`herdr workspace list`), not from a capture of the plugin's own
output:

| step | binding store | `herdr workspace list` for `w8P` |
| --- | --- | --- |
| after `space-create` | `msb:s53probe → w8P` | present, `pane_count=2` |
| after `stop` | `msb:s53probe → w8P` | present, `pane_count=1` |
| after `start` | `msb:s53probe → w8P` | present, `pane_count=1` |
| after `stop` + `rm` | absent | absent — workspace closed |

The pane count drops from 2 to 1 across `stop` because the guest shell pane dies with the VM. The
workspace itself does not.

The surviving binding was then used functionally rather than merely inspected:
`space-open-pane w8P` after the round trip resolved the binding by workspace ID and opened a new
guest pane (`w8P:p3`), taking the far-side pane count back to 2.

The last row is the opposite-outcome control. A binding that survived every operation would prove
nothing about `stop` specifically; the `rm` path removes it, so the survival across `stop` is a
real distinction and not a store that never changes.

## Evidence: a stopped sandbox releases all of its host guest RAM

This answers the question s54 left open for the RAM admission check.

Guest RAM under microsandbox is demand-allocated anonymous memory in the `libkrun VM` process, so
the host-side figure to watch is that process's `RssAnon` — not the daemon's self-report, and not
the configured `--memory-mib`, which is only a ceiling.

| point | `libkrun VM` pid | `VmRSS` | `RssAnon` |
| --- | --- | --- | --- |
| freshly booted, idle | 1435152 | 80892 kB | 60520 kB |
| after allocating 400 MiB in the guest | 1435152 | 492416 kB | 472044 kB |
| after `stop` | *process exited* | — | — |
| after `start` | 1439435 (new pid) | 79156 kB | — |

The middle row is the control that shows the measurement channel is live: host RSS tracked the
guest's allocation almost exactly (+411 MB for a 400 MiB write). A method that could not move
would not be able to detect a release either.

`stop` releases everything, because the process holding it exits. The answer for admission
purposes is therefore: **a stopped sandbox costs zero host RAM.** There is no separate "paused"
case to measure, because the runtime has no pause.

One caveat on the pid check. `pgrep -f 'sandbox --name herdr--s53probe'` matched the shell running
the check itself and reported a live pid with a 4 MB RSS, which reads exactly like "the VM is
still there, mostly released". The process had in fact exited. Enumerate by `comm` instead — the
VM processes appear as `libkrun VM` — or confirm via `test -d /proc/<pid>`.

## What was deliberately not changed

The admission check in `internal/core/admission` still counts every daemon-reported sandbox
regardless of status. The measurement above says a stopped sandbox could be excluded, but SDK
v0.6.17 gives no per-status guarantee and the conservative count is the safe direction for a guard
whose failure mode is the global OOM killer. Recorded here so the decision can be made with data;
not acted on in this slice.
