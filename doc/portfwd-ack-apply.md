# The forward queue: ack must follow apply, and skips must be audible

## The failure

On engine-03 (Linux engine) with a macOS client, herdr 0.9.0, plugin at `ea10135`:

1. A sandbox published guest port 8080, reachable on the engine at `127.0.0.1:8080`.
2. `herdr-plugin-msb declare -port 8080 -host engine-03` enqueued, on the engine, in
   `~/.local/state/herdr-plugin-msb/requests.json`:

   ```json
   {"id":5,"host":"engine-03","remote_port":8080,"local_port":8080,
    "remote_bind":"127.0.0.1","origin":"discovery","plugin_version":"0.1.0"}
   ```

3. The Mac agent was running with a live ControlMaster (`ssh -O check` reported
   `Master running`).
4. The agent wrote `requests.acked` = `5`. The request was consumed.
5. The Mac never listened on 8080, and the agent's stdout and stderr were empty.
6. The same operation by hand — `ssh -S <ctl> -O forward -L 8080:127.0.0.1:8080 engine-03`
   — returned rc 0 and the Mac then listened.

So the ssh mechanism was never at fault. The agent consumed the request without applying it
and said nothing.

## Root cause

The queue lives on the engine and the agent reads and acks it over ssh, so ack and apply are
two operations against two different machines. `Agent.Tick` used a single monotonic cursor
for both, and advanced that cursor on the paths that *skip* a request:

```go
if !a.IsOurs(r) || r.CreatedUnixMS < cutoffMS {
    cursor = r.ID
    continue
}
```

`IsOurs` was `r.Host == "" || r.Host == a.HostName`. `HostName` comes only from the
`--host` flag, and nothing passes it: `SpawnIfAbsent` spawns
`exec.Command(selfBin, "local-agent", "--target", t)`, and `herdr-plugin.toml` declares
`["herdr-plugin-msb", "declare", "--notify"]`. So on the live agent `HostName` was `""`
while the request carried `host: "engine-03"`, `IsOurs` returned false, and the cursor
advanced to 5 without a forward. `requests.acked` = 5 followed.

The second skip path had the same shape. The live record has no `created_unix_ms` key at
all, so the field unmarshals to 0, `0 < cutoffMS` holds for every clock, and an expired
request was likewise consumed rather than applied.

Two further defects made this silent rather than merely wrong:

- `Serve` discarded the tick error entirely (`_, _, tickErr := a.Tick(...)`); it only drove
  the backoff and was never printed.
- `SpawnIfAbsent` left `cmd.Stdout` and `cmd.Stderr` nil, so the detached agent's output went
  to `/dev/null`. Even a printed error could not have reached the operator.

## The rule

A single high-water ack cursor cannot express "applied 1 and 3, skipped 2". Therefore the
cursor advances **only** over a request whose forward actually succeeded, and any request
that is not applied — foreign host, expired, or a failed `ssh -O forward` — stops the cursor
where it stands. The consequence is a visible stall rather than silent data loss: the
request stays pending, the operator is told once per request id why, and the engine's own
`Queue.Prune` clears genuinely expired entries on the next declare.

`ServedHost()` closes the host-filter gap: with no explicit `--host`, the agent serves the
host part of its ssh target, which is the machine whose queue it is reading. An explicit
`--host` still overrides it.

`Agent.Report` is the operator channel. `RunLocalAgent` and the CLI's `local-agent` verb
both bind it to their stderr, and `SpawnIfAbsent` now points the detached child's stdout and
stderr at `<state-dir>/<target>.agent.log` so that stderr has somewhere to land.
