package portfwd

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Runner func(ctx context.Context, argv []string) (stdout, stderr string, exitCode int, err error)

func OSRunner(ctx context.Context, argv []string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	runErr := cmd.Run()
	code := 0
	if runErr != nil {
		if ex, ok := runErr.(*exec.ExitError); ok {
			code = ex.ExitCode()
			runErr = nil
		}
	}
	return outBuf.String(), errBuf.String(), code, runErr
}

type Forwarder struct {
	ControlPath string
	SSHHost     string
	Run         Runner
}

func (f *Forwarder) MasterAlive(ctx context.Context) (bool, error) {
	_, _, code, err := f.Run(ctx, []string{
		"ssh", "-O", "check",
		"-o", "ControlPath=" + f.ControlPath,
		f.SSHHost,
	})
	if err != nil {
		return false, err
	}
	switch code {
	case 0:
		return true, nil
	case 255:
		return false, nil
	default:
		return false, fmt.Errorf("ssh -O check: unexpected exit %d", code)
	}
}

func (f *Forwarder) EnsureMaster(ctx context.Context) error {
	alive, err := f.MasterAlive(ctx)
	if err != nil {
		return err
	}
	if alive {
		return nil
	}
	_, _, code, err := f.Run(ctx, []string{
		"ssh", "-M", "-N", "-f",
		"-o", "ControlMaster=yes",
		"-o", "ControlPath=" + f.ControlPath,
		"-o", "ControlPersist=60",
		"-o", "BatchMode=yes",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout=10",
		f.SSHHost,
	})
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("ssh master open: exit %d", code)
	}
	return nil
}

func (f *Forwarder) Apply(ctx context.Context, port uint16) error {
	spec := fmt.Sprintf("%d:127.0.0.1:%d", port, port)
	_, _, code, err := f.Run(ctx, []string{
		"ssh", "-O", "forward",
		"-L", spec,
		"-o", "ControlPath=" + f.ControlPath,
		f.SSHHost,
	})
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("ssh -O forward: exit %d", code)
	}
	return nil
}

func (f *Forwarder) Cancel(ctx context.Context, port uint16) error {
	spec := fmt.Sprintf("%d:127.0.0.1:%d", port, port)
	_, _, _, err := f.Run(ctx, []string{
		"ssh", "-O", "cancel",
		"-L", spec,
		"-o", "ControlPath=" + f.ControlPath,
		f.SSHHost,
	})
	return err
}

func (f *Forwarder) Present(ctx context.Context, port uint16) (bool, error) {
	stdout, _, code, err := f.Run(ctx, []string{"ss", "-ltn"})
	if err != nil {
		return false, err
	}
	if code != 0 {
		return false, fmt.Errorf("ss -ltn: exit %d", code)
	}
	return portInOutput(stdout, port), nil
}

func portInOutput(output string, port uint16) bool {
	needle := fmt.Sprintf(":%d", port)
	for _, line := range strings.Split(output, "\n") {
		start := 0
		for start < len(line) {
			idx := strings.Index(line[start:], needle)
			if idx < 0 {
				break
			}
			abs := start + idx
			after := abs + len(needle)
			if after >= len(line) || line[after] < '0' || line[after] > '9' {
				return true
			}
			start = abs + 1
		}
	}
	return false
}
