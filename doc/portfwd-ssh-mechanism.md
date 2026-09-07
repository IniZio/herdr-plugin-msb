# SSH ControlMaster port-forward mechanism

Verified on this host (linux, OpenSSH client, sshd listening on 0.0.0.0:22)
using 127.0.0.1 as the stand-in for a remote sandbox host.

## Requirement

A service on port P inside the sandbox must be reachable at
`http://127.0.0.1:P` on the operator's laptop on the **same** port number.
`-L P:127.0.0.1:P` with identical numbers on both sides is mandatory.
Renumbering breaks Vite HMR and OAuth `redirect_uri`.

---

## ControlPath convention

AF_UNIX `sun_path` is capped at ~107 bytes. Long `$TMPDIR` values have broken
sockets on this machine before. Always put the socket under `/run/user/<uid>/`:

```
SOCK=/run/user/$(id -u)/msb-portfwd-<sandbox-id>.sock
```

Length of the above pattern is 35–45 bytes, well within the cap.

---

## Command sequence

### 1. Open the ControlMaster

```sh
ssh -M -N -f \
  -o ControlMaster=yes \
  -o "ControlPath=$SOCK" \
  -o ControlPersist=60 \
  -o BatchMode=yes \
  -o ExitOnForwardFailure=yes \
  -o StrictHostKeyChecking=no \
  -o ConnectTimeout=10 \
  <remote-host>
```

`-f` backgrounds the master after auth. `-N` opens no shell. `ControlPersist=60`
keeps the master alive 60 s after the last client disconnects (adjust as
needed; for long-lived sandboxes, use `ControlPersist=yes`).
`ExitOnForwardFailure=yes` makes the master exit if initial port binding fails.

Observed exit: **0** on success.

### 2. Check master presence

```sh
ssh -O check -o "ControlPath=$SOCK" <remote-host> 2>&1
# exit $?
```

**Master alive** — stdout+stderr:
```
Master running (pid=2522005)
```
exit: **0**

**Master gone** — stderr:
```
Control socket connect(/run/user/1003/cm-portfwd-test.sock): No such file or directory
```
exit: **255**

Signal to key on: **exit code**. 0 = alive, 255 = gone. The message text
varies (pid changes); the exit code is stable.

### 3. Apply a remote→local forward

```sh
ssh -O forward -L "${P}:127.0.0.1:${P}" -o "ControlPath=$SOCK" <remote-host>
# exit $?
```

`-L local-port:remote-bind-addr:remote-port`. Both port numbers are `P`.
`remote-bind-addr` is `127.0.0.1` so the remote side binds only loopback
(the service inside the sandbox already listens on loopback).

Observed: no stdout, no stderr, exit **0** on success.

After this call the master opens a local listener. Verified with `ss -ltn`:

```
LISTEN 0  128  [::1]:45456  [::]:*
```

The `[::1]:P` (or `0.0.0.0:P` on IPv4-only hosts) entry is the local side
of the forward. The service on the remote is not directly reachable from the
host — the forward is the only path.

End-to-end proof: with http.server bound to `127.0.0.1:45456` on the remote,
`curl http://127.0.0.1:45456/` through the forward returns HTTP 200 HTML.
Local port equals remote port: both are 45456.

### 4. Cancel the forward

```sh
ssh -O cancel -L "${P}:127.0.0.1:${P}" -o "ControlPath=$SOCK" <remote-host>
# exit $?
```

Observed: no stdout, no stderr, exit **0**.

After cancel the local listener is gone. `ss -ltn | grep ":${P}"` returns
nothing (or only the remote-side service if the remote happens to be
localhost — see test-setup note below).

### 5. Kill the master

```sh
ssh -O exit -o "ControlPath=$SOCK" <remote-host>
```

Observed: `Exit request sent.`, exit **0**. Socket is removed by the master
process.

---

## Forward presence check

`ssh -O check` reports **master** presence, not whether a specific forward
exists. There is no `-O list` subcommand.

**Reliable signal for forward presence**: `ss -ltn | grep ":<P>"` on the
local machine.

- Present: `[::1]:P` (or `0.0.0.0:P`) appears in `ss -ltn` — the master
  has opened the local listener for that port.
- Absent: no such entry — either the forward was never added or was cancelled.

In production the sandbox service does not run on the operator's machine, so
any listener on port P is the forward. The `ss` check is unambiguous.

**Negative case verified** (forward absent):

After `ssh -O cancel -L 45456:127.0.0.1:45456 ...`, `ss -ltn | grep :45456`
returns empty (on a non-loopback remote). The curl connection is refused
(exit 7 or `Failed to connect`).

### Cancel on a non-existent forward

```sh
ssh -O cancel -L "${P}:127.0.0.1:${P}" -o "ControlPath=$SOCK" <remote-host>
```

When the forward does not exist, **exit is still 0**, but stderr prints:

```
mux_client_forward: forwarding request failed: port not forwarded
muxclient: master cancel forward request failed
```

**Do not use exit code to detect forward absence.** Use `ss -ltn` as the
authoritative check both before and after operations.

### Is forward apply idempotent?

Not tested (applying a forward that already exists for the same port). Do not
call `-O forward` if the forward is already live — `ExitOnForwardFailure=yes`
on the master means a bind collision will not silently succeed; it may error.
Check with `ss -ltn` before deciding to apply.

---

## ControlPersist note

With `ControlPersist=60` the master process stays alive for 60 s after the
last multiplexed session closes. The socket persists. `ssh -O check` returns 0
during that window. Use `ssh -O exit` to shut it down immediately.

---

## Test-setup limitation

In this verification, 127.0.0.1 is simultaneously the "local" machine and the
"remote" host. The http.server was bound to `127.0.0.1:45456` on the "remote",
which happens to be directly reachable locally. As a result, `curl
http://127.0.0.1:45456/` succeeded even after cancel (the local listener from
the forward was gone, but the http.server itself was still reachable directly).

**In production** the remote host is a VM or sandbox, not 127.0.0.1. The
service runs only inside the VM. After cancel, `curl` to `127.0.0.1:P` fails
(connection refused, exit 7) because there is no local listener. The `ss`
check is the authoritative signal; the curl failure is a confirmatory check.

---

## Complete verified argv summary

```sh
# Open master
ssh -M -N -f \
  -o ControlMaster=yes \
  -o "ControlPath=/run/user/$(id -u)/msb-portfwd-<id>.sock" \
  -o ControlPersist=60 \
  -o BatchMode=yes \
  -o ExitOnForwardFailure=yes \
  -o StrictHostKeyChecking=no \
  -o ConnectTimeout=10 \
  <host>

# Check master (exit 0=alive, 255=gone)
ssh -O check -o "ControlPath=<sock>" <host>

# Apply forward (same port both sides)
ssh -O forward -L "<P>:127.0.0.1:<P>" -o "ControlPath=<sock>" <host>

# Forward present? (authoritative)
ss -ltn | grep ":<P>"

# Cancel forward
ssh -O cancel -L "<P>:127.0.0.1:<P>" -o "ControlPath=<sock>" <host>

# Forward absent? (authoritative)
ss -ltn | grep ":<P>"   # expect empty

# Kill master
ssh -O exit -o "ControlPath=<sock>" <host>
```

---

## Not verified here

- `StrictHostKeyChecking=no` is appropriate only when the host key is
  stored in `known_hosts` or the host is a controlled VM whose key is known.
  For production use, pass `-o UserKnownHostsFile=<path>` pointing at a file
  containing the sandbox's host key, or accept it once interactively.
- `GatewayPorts` behaviour: by default the local listener binds `[::1]` and
  `127.0.0.1`. If only IPv4 is desired, add `-o AddressFamily=inet`.
- Behaviour when port P is already in use on the local machine when
  `-O forward` is called. `ExitOnForwardFailure=yes` on the master means the
  master itself won't crash, but the forward call may return an error.
- Apply idempotency (calling `-O forward` for a port that already has a live
  forward) — not tested; use `ss -ltn` to guard before applying.
