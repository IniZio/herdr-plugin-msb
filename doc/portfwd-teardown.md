# Port-forward teardown and presence-check rationale

## Why `ss -ltn` is the per-forward presence signal

`ssh -O check` reports whether the SSH multiplexed master is alive. It says nothing about which port-forwards are active on that master — it is a master-level probe, not a per-forward one.

The cancel exit code cannot distinguish "cancelled" from "was never applied". Measured directly on this host: `ssh -O cancel -L P:127.0.0.1:P` for a port that was never forwarded exits **0** and prints `mux_client_forward: forwarding request failed: port not forwarded` to stderr. Using the cancel exit code as a presence signal would report every cancel as success regardless of whether the forward existed.

`ss -ltn` output after `ssh -O forward -L P:127.0.0.1:P` shows `[::1]:P` or `127.0.0.1:P`. After `ssh -O cancel` that entry is gone. Both cases were confirmed end-to-end with curl proving traffic flow at port 45456. The local listening socket is the authoritative per-forward fact: the kernel either accepts connections or it does not, and `ss -ltn` reflects that directly.

Port matching uses a substring search for `:<port>` with a guard on the following character: if the character immediately after the matched digits is itself a digit, the match is rejected. This prevents port 45456 matching `:454560` (suffix false-positive) and port 4545 matching `:45456` (prefix false-positive).

## Why teardown is driven by our own records, not process watching

`runtime.SandboxRef` carries no pid. This is deliberate: microVM lifecycles are not bound to a single host process, and a pid would expose an implementation detail the seam is specifically designed to hide.

`Manager` records which forwards it applied and for which sandbox ID. When `Reconcile` sees a sandbox whose status is `SandboxStatusStopped` or which is absent from the list entirely, it cancels every recorded forward for that sandbox ID and removes the records. `TeardownSandbox` does the same for an explicit stop signal. Neither path inspects any process name or process ID.

## Why there is no renumbering path

A forward where the local port differs from the in-sandbox port breaks Vite HMR (which embeds its own address in the WebSocket upgrade URL) and OAuth `redirect_uri` (which must match the registered value exactly). A renumbered forward is worse than no forward: the application appears to start while silently failing.

`Apply` emits `-L <port>:127.0.0.1:<port>` with the identical number on both sides. If the local port is already occupied, `ssh -O forward` exits non-zero and `Apply` returns an error. There is no fallback port, no auto-pick, and no kill-competing-process path. The same-port invariant is enforced by `TestApplySamePortInvariant`, which inspects the `-L` argument in the captured argv and fails if the two port numbers differ.
