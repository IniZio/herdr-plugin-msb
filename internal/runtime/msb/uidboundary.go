package msb

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

type UidBoundary struct {
	helperPath string
	ownerUID   int
}

func NewUidBoundary(helperPath string) *UidBoundary {
	return &UidBoundary{helperPath: helperPath, ownerUID: os.Getuid()}
}

func (u *UidBoundary) Apply(ctx context.Context, base uint16) error {
	cmd := exec.CommandContext(ctx, u.helperPath, "apply", strconv.Itoa(int(base)), strconv.Itoa(u.ownerUID))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("uid-boundary: apply base=%d uid=%d: %w: %s", base, u.ownerUID, err, out)
	}
	return nil
}

func (u *UidBoundary) Remove(ctx context.Context, base uint16) error {
	cmd := exec.CommandContext(ctx, u.helperPath, "remove", strconv.Itoa(int(base)))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("uid-boundary: remove base=%d: %w: %s", base, err, out)
	}
	return nil
}
