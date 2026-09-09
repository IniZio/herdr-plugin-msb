# linkenv-probe — does herdr set HERDR_PLUGIN_CLICKED_URL?

Settles, with a negative control, whether herdr 0.8.0 delivers the clicked URL
to a `[[link_handlers]]` action via the environment variable `HERDR_PLUGIN_CLICKED_URL`
(or via argv).

## Argv-substitution finding (read before running)

The manifest schema for `[[link_handlers]]` exposes only `id`, `title`, `pattern`,
and `action` (an action id reference). There is no `command` field on the handler
itself, and none of the `[[actions]]` command arrays contain any URL placeholder
token (`{url}`, `%u`, `$URL`, or similar). The manifest provides no argv-substitution
mechanism. If the URL reaches the action at all, the delivery channel must be an
environment variable. The probe captures both env and argv in the same run so both
channels are falsifiable simultaneously.

## What was added to herdr-plugin.toml

Two new stanzas appended after the existing `[[link_handlers]]` block (purely additive;
`local-port` and `ports-declare` are byte-identical):

- `[[actions]] id = "linkenv-probe-dump"` — writes `=== argv: …` then `env | sort`
  to `/tmp/herdr-linkenv-probe.txt`.
- `[[link_handlers]] id = "linkenv-probe"` — pattern `http://127\.0\.0\.2:59999`.
  Uses IP `127.0.0.2` (not `127.0.0.1`) so it cannot match the shipped `local-port`
  handler's pattern regardless of whether herdr uses substring or anchored matching.

## Step 0 — reload the plugin

The plugin is already linked locally. Re-link to pick up the new stanzas:

```sh
herdr plugin unlink herdr-plugin-msb
herdr plugin link /home/newman/magic/herdr-plugin-msb
```

Confirm the new handler is visible:

```sh
herdr plugin action list --plugin herdr-plugin-msb
```

`linkenv-probe-dump` must appear in the output before proceeding.

## RUN A — negative control (direct invocation, no click)

Invoke the action without a link click:

```sh
rm -f /tmp/herdr-linkenv-probe.txt
herdr plugin action invoke linkenv-probe-dump --plugin herdr-plugin-msb
```

Then read the output file:

```sh
cat /tmp/herdr-linkenv-probe.txt
```

Record whether `HERDR_PLUGIN_CLICKED_URL` appears and whether `=== argv:` is followed
by any tokens beyond `$0`. Save this file:

```sh
cp /tmp/herdr-linkenv-probe.txt /tmp/herdr-linkenv-probe-run-a.txt
```

If the file was not created, the action did not fire — check
`herdr plugin log list --plugin herdr-plugin-msb` and do not proceed to RUN B until
RUN A produces a file.

## RUN B — positive (click the probe URL)

In any herdr pane or terminal that herdr renders as a link-clickable surface, print:

```sh
printf 'Click this URL: http://127.0.0.2:59999\n'
```

Ctrl+click `http://127.0.0.2:59999`. Wait ~3 s for the action to complete.

```sh
cat /tmp/herdr-linkenv-probe.txt
cp /tmp/herdr-linkenv-probe.txt /tmp/herdr-linkenv-probe-run-b.txt
```

## Interpreting the results

Compare `/tmp/herdr-linkenv-probe-run-a.txt` against `/tmp/herdr-linkenv-probe-run-b.txt`:

| Observation | Conclusion |
|---|---|
| `HERDR_PLUGIN_CLICKED_URL=http://127.0.0.2:59999` present in RUN B, absent in RUN A | env var is the delivery channel — **fires** |
| `=== argv:` in RUN B shows extra token(s) absent in RUN A | argv is the delivery channel — **fires via argv** |
| Neither env var nor extra argv tokens differ between the two runs | herdr does NOT deliver the URL to the action — **does not fire** |
| `/tmp/herdr-linkenv-probe.txt` not updated by RUN B (mtime unchanged) | handler did not invoke the action — **inconclusive**; check that the URL was rendered clickable and that herdr recognised the click |
| RUN A file is absent | action did not fire at all — **inconclusive**; stop and diagnose before RUN B |

A result is only valid if RUN A and RUN B both produced output files. A single passing
run cannot distinguish a set var from an ignored one.

## Revert

```sh
git checkout /home/newman/magic/herdr-plugin-msb/herdr-plugin.toml
herdr plugin unlink herdr-plugin-msb
herdr plugin link /home/newman/magic/herdr-plugin-msb
rm -f /tmp/herdr-linkenv-probe.txt /tmp/herdr-linkenv-probe-run-a.txt /tmp/herdr-linkenv-probe-run-b.txt
```

Confirm revert:

```sh
herdr plugin action list --plugin herdr-plugin-msb
```

`linkenv-probe-dump` must no longer appear.
