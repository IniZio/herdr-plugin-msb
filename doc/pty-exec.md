# `exec -pty`: guest PTY size and local terminal mode

## The defect

`exec -pty` shipped with two independent faults that made the guest-shell herdr
pane unusable by hand. Both were invisible to CLI capture, because a captured
run has no terminal on either side.

### 1. The guest PTY size was a hardcoded 24x80

`internal/cli/cmd_sandbox.go` set `req.Rows = 24` / `req.Cols = 80`
unconditionally. Measured against the live `eyeball` sandbox from a host PTY
sized 62x242, the guest reported:

    $ stty size
    24 80

The guest shell's line editor therefore wrapped at column 80 while the real
terminal wrapped at column 242, and positioned the cursor against 24 rows in a
62-row pane. Every redraw — prompt, history recall, completion list — landed in
the wrong place. This is the "behaved randomly" the operator saw.

### 2. The local terminal was left in cooked mode

Nothing put the local terminal into raw mode. Sampling the host PTY's `termios`
while an `exec -pty` was running gave `ECHO=True, ICANON=True, ISIG=True`, so:

- input was line-buffered locally — Tab, arrow keys and any other in-line
  editing key reached the guest only after Enter, if at all;
- every keystroke was echoed twice, once locally and once by the guest;
- `OPOST` was still on, so the guest's CRLF was doubled to `\r\r\n`;
- `ISIG` meant Ctrl-C raised SIGINT against the *plugin* process, tearing down
  the whole pane instead of interrupting the guest command;
- cursor-position replies (`ESC[6n` → `ESC[<r>;<c>R`) were echoed back as
  literal text. Those stray `^[[1;5R` fragments in earlier captures were a
  symptom, not noise.

### 3. No SIGWINCH propagation

Resizing the pane never reached `ExecHandle.Resize`, so even a correct initial
size went stale on the first resize.

## The fix

- **Size comes from the controlling terminal** (`TIOCGWINSZ` on stdin, then
  stdout, then stderr), not from a constant and not from herdr.
- **`-rows` / `-cols` override** the detected size, for non-terminal callers
  and for tests.
- **24x80 remains the fallback** only when no standard stream is a terminal,
  which preserves existing scripted behaviour.
- **Raw mode** is entered on stdin when `-pty` is set and stdin is a terminal,
  and restored on exit.
- **SIGWINCH** re-reads the size and forwards it over `ExecRequest.ResizeCh` to
  `ExecHandle.Resize` for the lifetime of the exec.

### Why not ask herdr for the pane size

`herdr pane layout --pane <id>` does report a rect (`width` 242, `height` 62),
so the information exists. It is still the wrong source:

- it is stale the moment the pane is resized, and gives no resize event;
- it would require the plugin to know its own pane id;
- it would couple the plugin to herdr's JSON, and break `exec -pty` under
  plain ssh, tmux, or a bare terminal.

`TIOCGWINSZ` on the controlling terminal is correct in all of those cases, and
herdr already sets the pane PTY's winsize correctly. `pane.sh` therefore needs
no change and passes no size.

### Why stdlib `syscall` and not `golang.org/x/term`

This module has exactly one direct dependency by design. The host is Linux-only
(libkrun microVMs), so the portability `x/term` buys is unused, and the required
surface is three ioctls: `TCGETS`, `TCSETS`, `TIOCGWINSZ`. A non-Linux stub
keeps the package building elsewhere.

## Proving it

`internal/cli/terminal_linux_test.go` allocates a real PTY via `/dev/ptmx` and
asserts the detected size and the cleared `ECHO`/`ICANON`/`ISIG`/`OPOST` bits.
`internal/runtime/msb/resize_test.go` covers the resize pump.

Each was confirmed to fail against perturbed production source with the test
files byte-identical between runs.

A SIGWINCH check needs the child to own a **controlling terminal**
(`setsid()` + `TIOCSCTTY`). Without it the kernel has no foreground process
group to signal and the resize silently never arrives — a harness defect that
looks exactly like a product defect. herdr panes do give the process a
controlling terminal.
