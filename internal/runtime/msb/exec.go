package msb

import (
	"context"
	"errors"
	"fmt"
	"io"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
	msbsdk "github.com/superradcompany/microsandbox/sdk/go"
)

func (r *Runtime) Exec(ctx context.Context, ref coreruntime.SandboxRef, req coreruntime.ExecRequest) (coreruntime.ExecResult, error) {
	if len(req.Argv) == 0 {
		return coreruntime.ExecResult{}, fmt.Errorf("msb: exec: argv must not be empty")
	}
	sb, err := r.connect(ctx, ref)
	if err != nil {
		return coreruntime.ExecResult{}, err
	}
	defer func() { _ = sb.Detach(ctx) }()
	return streamExec(ctx, sb, req)
}

func (r *Runtime) RunEphemeral(ctx context.Context, spec coreruntime.SandboxSpec, req coreruntime.ExecRequest) (coreruntime.ExecResult, error) {
	if len(req.Argv) == 0 {
		return coreruntime.ExecResult{}, fmt.Errorf("msb: run-ephemeral: argv must not be empty")
	}
	opts := append(SandboxOptions(spec), msbsdk.WithDetached(), msbsdk.WithEphemeral(true))
	name := SDKName(spec.Project, spec.Name)
	if name == "" {
		return coreruntime.ExecResult{}, fmt.Errorf("msb: run-ephemeral: spec has no name")
	}
	sb, err := msbsdk.CreateSandbox(ctx, name, opts...)
	if err != nil {
		return coreruntime.ExecResult{}, fmt.Errorf("msb: run-ephemeral create %q: %w", name, err)
	}
	if perr := assertNetworkPolicyByName(ctx, name); perr != nil {
		return coreruntime.ExecResult{}, errors.Join(perr, sb.Destroy(ctx))
	}
	var result coreruntime.ExecResult
	result, err = streamExec(ctx, sb, req)
	destroyErr := sb.Destroy(ctx)
	if err != nil {
		return coreruntime.ExecResult{}, err
	}
	if destroyErr != nil {
		return result, fmt.Errorf("msb: run-ephemeral destroy %q: %w", name, destroyErr)
	}
	return result, nil
}

type ptyResizer interface {
	Resize(ctx context.Context, rows, cols uint16) error
}

// pumpResize forwards terminal size changes to the guest PTY until the exec
// finishes. Resize errors are dropped: a failed resize must not abort a
// working session.
func pumpResize(ctx context.Context, h ptyResizer, ch <-chan coreruntime.WinSize, done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case ws, ok := <-ch:
			if !ok {
				return
			}
			if ws.Rows == 0 && ws.Cols == 0 {
				continue
			}
			_ = h.Resize(ctx, ws.Rows, ws.Cols)
		}
	}
}

func streamExec(ctx context.Context, sb *msbsdk.Sandbox, req coreruntime.ExecRequest) (coreruntime.ExecResult, error) {
	stdout := req.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := req.Stderr
	if stderr == nil {
		stderr = io.Discard
	}

	var execOpts []msbsdk.ExecOption
	if len(req.Env) > 0 {
		execOpts = append(execOpts, msbsdk.WithExecEnv(req.Env))
	}
	if req.Cwd != "" {
		execOpts = append(execOpts, msbsdk.WithExecCwd(req.Cwd))
	}
	if req.TTY {
		execOpts = append(execOpts, msbsdk.WithExecTTY(true), msbsdk.WithExecStdinPipe())
	} else if req.Stdin != "" {
		execOpts = append(execOpts, msbsdk.WithExecStdinPipe())
	}

	cmd := req.Argv[0]
	args := req.Argv[1:]
	handle, err := sb.ExecStream(ctx, cmd, args, execOpts...)
	if err != nil {
		return coreruntime.ExecResult{}, fmt.Errorf("msb: exec-stream %q: %w", cmd, err)
	}
	defer handle.Close()

	if req.TTY && (req.Rows > 0 || req.Cols > 0) {
		if rerr := handle.Resize(ctx, req.Rows, req.Cols); rerr != nil {
			return coreruntime.ExecResult{}, fmt.Errorf("msb: resize: %w", rerr)
		}
	}

	if req.TTY && req.ResizeCh != nil {
		done := make(chan struct{})
		defer close(done)
		go pumpResize(ctx, handle, req.ResizeCh, done)
	}

	if req.TTY {
		sink := handle.TakeStdin()
		if sink != nil {
			if req.StdinReader != nil {
				go func() {
					_, _ = io.Copy(sink, req.StdinReader)
					_ = sink.Close()
				}()
			} else {
				_ = sink.Close()
			}
		}
	} else if req.Stdin != "" {
		sink := handle.TakeStdin()
		if _, err := io.WriteString(sink, req.Stdin); err != nil {
			return coreruntime.ExecResult{}, fmt.Errorf("msb: stdin write: %w", err)
		}
		if err := sink.Close(); err != nil {
			return coreruntime.ExecResult{}, fmt.Errorf("msb: stdin close: %w", err)
		}
	}

	var exitCode int
	for {
		ev, err := handle.Recv(ctx)
		if err != nil {
			return coreruntime.ExecResult{}, fmt.Errorf("msb: recv: %w", err)
		}
		switch ev.Kind {
		case msbsdk.ExecEventStdout:
			if _, werr := stdout.Write(ev.Data); werr != nil {
				return coreruntime.ExecResult{}, fmt.Errorf("msb: stdout write: %w", werr)
			}
		case msbsdk.ExecEventStderr:
			if _, werr := stderr.Write(ev.Data); werr != nil {
				return coreruntime.ExecResult{}, fmt.Errorf("msb: stderr write: %w", werr)
			}
		case msbsdk.ExecEventExited:
			exitCode = ev.ExitCode
		case msbsdk.ExecEventFailed:
			return coreruntime.ExecResult{}, fmt.Errorf("msb: exec failed: %v", ev.Failure)
		case msbsdk.ExecEventDone:
			return coreruntime.ExecResult{ExitCode: int32(exitCode)}, nil
		}
	}
}
