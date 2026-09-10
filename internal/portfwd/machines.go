package portfwd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type Runner func(ctx context.Context, argv []string) (stdout, stderr string, exitCode int, err error)

type Machine struct {
	ProfileID string `json:"id"`
	Label     string `json:"label"`
	SSHTarget string `json:"target"`
	Session   string `json:"session"`
	Enabled   bool   `json:"enabled"`
	Selected  bool   `json:"selected"`
}

var ErrHerdrFailed = errors.New("herdr exited non-zero")

var ErrBadJSON = errors.New("herdr machine list: malformed JSON")

func DiscoverMachines(ctx context.Context, run Runner) ([]Machine, error) {
	stdout, stderr, code, execErr := run(ctx, []string{"herdr", "machine", "list", "--json"})
	if execErr != nil {
		return nil, fmt.Errorf("herdr machine list: %w", execErr)
	}
	if code != 0 {
		msg := stderr
		if msg == "" {
			msg = stdout
		}
		return nil, fmt.Errorf("%w (exit %d): %s", ErrHerdrFailed, code, msg)
	}
	var machines []Machine
	if err := json.Unmarshal([]byte(stdout), &machines); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadJSON, err)
	}
	return machines, nil
}
