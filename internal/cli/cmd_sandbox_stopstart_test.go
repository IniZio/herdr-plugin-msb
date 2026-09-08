package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/IniZio/herdr-plugin-msb/internal/core/herdrspace"
)

func TestSpaceCleanup_StopKeepsBinding_RMRemovesBinding(t *testing.T) {
	t.Run("stop_keeps_binding", func(t *testing.T) {
		dir := t.TempDir()
		ctx := context.Background()
		b := herdrspace.Binding{
			SpaceLabel:       "msb:stoptest1",
			HerdrWorkspaceID: "w-stop-001",
			SandboxHandle:    "acme/stoptest1",
		}
		if err := herdrspace.Put(ctx, dir, b); err != nil {
			t.Fatalf("Put: %v", err)
		}

		spaceCleanup(ctx, dir, "acme", "stoptest1", false)

		got, err := herdrspace.GetByHandle(ctx, dir, "acme/stoptest1")
		if err != nil {
			t.Fatalf("stop (closeWorkspace=false): binding must survive, got err: %v", err)
		}
		if got.HerdrWorkspaceID != "w-stop-001" {
			t.Fatalf("workspace ID: got %q want w-stop-001", got.HerdrWorkspaceID)
		}
	})

	t.Run("rm_removes_binding", func(t *testing.T) {
		dir := t.TempDir()
		ctx := context.Background()
		b := herdrspace.Binding{
			SpaceLabel:       "msb:rmtest2",
			HerdrWorkspaceID: "w-rm-002",
			SandboxHandle:    "acme/rmtest2",
		}
		if err := herdrspace.Put(ctx, dir, b); err != nil {
			t.Fatalf("Put: %v", err)
		}

		fakeBin := filepath.Join(t.TempDir(), "herdr-fake")
		if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HERDR_BIN_PATH", fakeBin)

		spaceCleanup(ctx, dir, "acme", "rmtest2", true)

		_, err := herdrspace.GetByHandle(ctx, dir, "acme/rmtest2")
		if !errors.Is(err, herdrspace.ErrNotFound) {
			t.Fatalf("rm (closeWorkspace=true): binding must be removed; got err=%v", err)
		}
	})
}
