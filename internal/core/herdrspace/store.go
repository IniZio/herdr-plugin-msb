package herdrspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Binding records the 1:1 relationship between a herdr workspace and an msb sandbox.
// Kept: SpaceLabel (human key), HerdrWorkspaceID (opaque herdr ID), SandboxHandle (msb stable ref).
// Dropped: SandboxID (msb uses handle as stable ref), GuestPaneID (pane lifecycle is caller-managed),
// RepoRoot and WorktreeManaged (worktree-reap flow is out of scope for this package).
type Binding struct {
	SpaceLabel       string `json:"space_label"`
	HerdrWorkspaceID string `json:"herdr_workspace_id"`
	SandboxHandle    string `json:"sandbox_handle"`
	CheckoutPath     string `json:"checkout_path,omitempty"`
}

// ErrNotFound is returned when no matching binding exists.
var ErrNotFound = errors.New("herdrspace: binding not found")

const (
	bindingsFilename = "herdr-space-bindings.json"
	lockFilename     = "herdr-space-bindings.lock"
)

func bindingsPath(dir string) string { return filepath.Join(dir, bindingsFilename) }
func lockPath(dir string) string     { return filepath.Join(dir, lockFilename) }

func withLock(ctx context.Context, dir string, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lk, err := os.OpenFile(lockPath(dir), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("herdrspace: open lock: %w", err)
	}
	defer lk.Close()
	if err := syscall.Flock(int(lk.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("herdrspace: acquire lock: %w", err)
	}
	defer syscall.Flock(int(lk.Fd()), syscall.LOCK_UN)
	return fn()
}

func readAll(dir string) ([]Binding, error) {
	data, err := os.ReadFile(bindingsPath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("herdrspace: read: %w", err)
	}
	var bs []Binding
	if err := json.Unmarshal(data, &bs); err != nil {
		return nil, fmt.Errorf("herdrspace: unmarshal: %w", err)
	}
	return bs, nil
}

func writeAll(dir string, bs []Binding) error {
	data, err := json.MarshalIndent(bs, "", "  ")
	if err != nil {
		return fmt.Errorf("herdrspace: marshal: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".herdr-space-bindings-*.json.tmp")
	if err != nil {
		return fmt.Errorf("herdrspace: create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { os.Remove(tmpName) }()
	if err := os.Chmod(tmpName, 0600); err != nil {
		tmp.Close()
		return fmt.Errorf("herdrspace: chmod temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("herdrspace: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("herdrspace: close temp: %w", err)
	}
	if err := os.Rename(tmpName, bindingsPath(dir)); err != nil {
		return fmt.Errorf("herdrspace: rename: %w", err)
	}
	return nil
}

// Put stores b, replacing any existing entry conflicting on SpaceLabel or SandboxHandle.
func Put(ctx context.Context, dir string, b Binding) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("herdrspace: mkdir: %w", err)
	}
	return withLock(ctx, dir, func() error {
		bs, err := readAll(dir)
		if err != nil {
			return err
		}
		out := bs[:0:0]
		for _, existing := range bs {
			if existing.SpaceLabel == b.SpaceLabel || existing.SandboxHandle == b.SandboxHandle {
				continue
			}
			out = append(out, existing)
		}
		out = append(out, b)
		return writeAll(dir, out)
	})
}

func GetByLabel(ctx context.Context, dir string, label string) (Binding, error) {
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	bs, err := readAll(dir)
	if err != nil {
		return Binding{}, err
	}
	for _, b := range bs {
		if b.SpaceLabel == label {
			return b, nil
		}
	}
	return Binding{}, ErrNotFound
}

func GetByWorkspaceID(ctx context.Context, dir string, id string) (Binding, error) {
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	bs, err := readAll(dir)
	if err != nil {
		return Binding{}, err
	}
	for _, b := range bs {
		if b.HerdrWorkspaceID == id {
			return b, nil
		}
	}
	return Binding{}, ErrNotFound
}

func GetByHandle(ctx context.Context, dir string, handle string) (Binding, error) {
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	bs, err := readAll(dir)
	if err != nil {
		return Binding{}, err
	}
	for _, b := range bs {
		if b.SandboxHandle == handle {
			return b, nil
		}
	}
	return Binding{}, ErrNotFound
}

func Delete(ctx context.Context, dir string, label string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("herdrspace: mkdir: %w", err)
	}
	return withLock(ctx, dir, func() error {
		bs, err := readAll(dir)
		if err != nil {
			return err
		}
		out := bs[:0:0]
		for _, b := range bs {
			if b.SpaceLabel != label {
				out = append(out, b)
			}
		}
		return writeAll(dir, out)
	})
}

func List(ctx context.Context, dir string) ([]Binding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return readAll(dir)
}
