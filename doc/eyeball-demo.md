# Eyeball Demo — operator walkthrough

Live state as of 2026-09-08. Sandbox `herdr--eyeball` (local:203) running, dev server
inside serving token on port 45455.

## What is running

| Thing | Value |
|---|---|
| Sandbox name | `herdr--eyeball` (msb name), project `herdr`, id local:203 |
| Guest dev-server token | `eyeball-1788867133-22958` |
| Dev server port | 45455 (guest→host forward via libkrun; durable server: `nc -lk -p 45455 -e /tmp/handler-cat.sh`, guest PID 762) |
| SSH forward | laptop 100.64.0.35 → engine-03 100.64.0.156 port 45455, master PID 92315 on laptop, socket `/tmp/herdr-agent-engine-03.ctl` |
| Plugin binary | `~/.local/bin/herdr-plugin-msb` (current, has -pty) |
| herdr workspace for worktree | w8D "herdr-plugin-msb demo" |
| herdr workspace for guest shell | w8E "msb:eyeball" |

## 1. Open the herdr TUI and navigate

```
herdr
```

Navigate to workspace **8** (`msb:eyeball`, w8E) — three panes, two tabs:

- **Tab 1, pane 1** — host shell (`newman@engine-03`)
- **Tab 1, pane 2** — Microsandbox notification (shows "microsandbox ports / no new ports to forward")
- **Tab 2, pane 1** (label: "Microsandbox guest shell") — guest shell inside `herdr--eyeball`

Navigate to workspace **7** (`herdr-plugin-msb demo`, w8D) to see the demo worktree panes.
Both panes in w8D have `foreground_cwd=/home/newman/magic/herdr-plugin-msb-demo` (linked worktree,
branch `demo/eyeball`).

## 2. Confirm you are inside the guest (from within w8E tab 2)

In the guest shell pane (w8E tab 2, labeled "Microsandbox guest shell"), run:

```sh
cat /proc/1/comm
```

Expected output: `init.krun`

On the host, `/proc/1/comm` is `systemd`. Inside the microsandbox guest (libkrun VM), PID 1 is
`init.krun`. This is the definitive in-guest test.

Also run:

```sh
hostname
```

Expected output: `herdr--eyeball`

## 3. Open a NEW guest pane yourself

From any host shell, run:

```
herdr-plugin-msb exec -pty -project herdr eyeball -- /bin/sh
```

This drops you into a root shell (`/ #`) inside the running `eyeball` sandbox. Confirm with
`cat /proc/1/comm` → `init.krun`. Close with `exit` or Ctrl-D.

To open the pane inside the herdr TUI (split into a new pane in the current workspace):

```
herdr pane split --workspace w8E <PANE_ID>
```

Then in the new pane run `herdr-plugin-msb exec -pty -project herdr eyeball -- /bin/sh`.

## 4. Test the port forward from the laptop

The **only** valid test of the SSH port forward is a fetch run **on the laptop**.
An engine-side `curl http://127.0.0.1:45455/` reaches the guest's published port directly
(via libkrun) and never crosses the tunnel — a passing engine-side fetch says nothing
about whether the laptop forward is alive.

From **engine-03**, ssh to the laptop and run curl there:

```sh
ssh 100.64.0.35 'sh -c "curl -sS --max-time 5 http://127.0.0.1:45455/"'
```

Expected output: `eyeball-1788867133-22958`  
Expected exit code: **0**

The durable server (`nc -lk -p 45455 -e /tmp/handler-cat.sh`) sends a proper
`HTTP/1.0 200 OK` response with `Content-Length: 25` and closes the connection cleanly.
curl exits 0 and prints the token with no timeout.

Engine-side fetch (tests only the published guest port, **not** the tunnel):

```sh
curl -sS http://127.0.0.1:45455/
```

Expected output: `eyeball-1788867133-22958`, exit 0.

## 5. Verify plugin verbs (run from engine-03)

```sh
herdr-plugin-msb list
# → [{"id":3,"host":"engine-03","remote_port":45455,...}]   exit 0

herdr-plugin-msb status
# → pending=1 acked=2   exit 0
# Note: counts move as the local-agent polls and acknowledges forwards — this is a snapshot.

herdr-plugin-msb declare -port 45455 -host engine-03 -notify
# → null   exit 0   (port already pending — null is success, not an error)
# → [{"id":3,...}]  exit 0   (port not yet pending — returns the newly queued entry)
```

## 6. Teardown (in order)

**a. Remove the sandbox** (frees VM RAM):

```sh
msb remove herdr--eyeball
```

**b. Kill the SSH port forward** (run on the laptop):

```sh
ssh -S /tmp/herdr-agent-engine-03.ctl -O exit dummy
```

**c. Close the demo workspace** (removes w8E from herdr):

```sh
herdr workspace close w8E
```

**d. Close the worktree workspace**:

```sh
herdr workspace close w8D
```

**e. Remove the linked worktree**:

```sh
git -C /home/newman/magic/herdr-plugin-msb worktree remove /home/newman/magic/herdr-plugin-msb-demo
```

**f. Restore session.json** (pick the backup that predates the demo):

```sh
# Prefer the earlier backup (pre-space-verbs):
cp ~/.config/herdr/session.json.backup.20260908_112826 ~/.config/herdr/session.json

# Or the space-verbs backup:
cp ~/.config/herdr/session.json.bak-space-verbs-20260908-120028 ~/.config/herdr/session.json
```

Restart herdr after restoring.

## Session.json backups on disk

```
~/.config/herdr/session.json                             (live, 9.3K)
~/.config/herdr/session.json.backup.20260908_112826      (5.0K — earliest, pre-workspace-additions)
~/.config/herdr/session.json.bak-space-verbs-20260908-120028  (6.4K — post-space-verbs, pre-demo)
```
