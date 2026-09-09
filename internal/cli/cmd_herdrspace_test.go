package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IniZio/herdr-plugin-msb/internal/core/herdrspace"
)

func TestSpaceCleanup_StopKeepsBinding(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	b := herdrspace.Binding{
		SpaceLabel:       "msb:sb1",
		HerdrWorkspaceID: "wXXX",
		SandboxHandle:    "demo/sb1",
	}
	if err := herdrspace.Put(ctx, dir, b); err != nil {
		t.Fatalf("Put: %v", err)
	}

	spaceCleanup(ctx, dir, "demo", "sb1", false)

	got, err := herdrspace.GetByHandle(ctx, dir, "demo/sb1")
	if err != nil {
		t.Fatalf("binding must survive stop (closeWorkspace=false): %v", err)
	}
	if got.HerdrWorkspaceID != "wXXX" {
		t.Fatalf("workspace ID: got %q want wXXX", got.HerdrWorkspaceID)
	}
}

func TestStopStartNewTab_GuestPane(t *testing.T) {
	stateParent := t.TempDir()
	stateDir := filepath.Join(stateParent, StateDirNS)
	ctx := context.Background()

	b := herdrspace.Binding{
		SpaceLabel:       "msb:sb2",
		HerdrWorkspaceID: "wYYY",
		SandboxHandle:    "demo/sb2",
	}
	if err := herdrspace.Put(ctx, stateDir, b); err != nil {
		t.Fatalf("Put: %v", err)
	}

	spaceCleanup(ctx, stateDir, "demo", "sb2", false)

	got, err := herdrspace.GetByHandle(ctx, stateDir, "demo/sb2")
	if err != nil {
		t.Fatalf("binding must survive stop; new-tab after stop->start would open host tab: %v", err)
	}
	if got.HerdrWorkspaceID != "wYYY" {
		t.Fatalf("workspace ID: got %q want wYYY", got.HerdrWorkspaceID)
	}

	fakeBin := filepath.Join(t.TempDir(), "herdr-fake")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\necho \"$@\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_BIN_PATH", fakeBin)
	t.Setenv("XDG_STATE_HOME", stateParent)

	var stdout, stderr bytes.Buffer
	code := runNewTab(ctx, []string{"wYYY"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("new-tab after stop->start: want 0, got %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "plugin") {
		t.Fatalf("new-tab must route to guest pane (plugin pane open), not host tab; stdout=%q", stdout.String())
	}
}

func TestHerdrBin(t *testing.T) {
	t.Run("bin_path_deleted_suffix_file_exists", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "herdr-real")
		if err := os.WriteFile(f, []byte("x"), 0755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HERDR_BIN_PATH", f+" (deleted)")
		t.Setenv("HERDR_BIN", "")
		got := herdrBin()
		if got != f {
			t.Fatalf("want %q, got %q", f, got)
		}
	})

	t.Run("bin_path_deleted_suffix_file_missing", func(t *testing.T) {
		missing := "/nonexistent/path/herdr-gone"
		input := missing + " (deleted)"
		t.Setenv("HERDR_BIN_PATH", input)
		t.Setenv("HERDR_BIN", "")
		got := herdrBin()
		if got != input {
			t.Fatalf("want %q (unchanged), got %q", input, got)
		}
	})

	t.Run("bin_path_plain_no_suffix", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "herdr-plain")
		if err := os.WriteFile(f, []byte("x"), 0755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HERDR_BIN_PATH", f)
		t.Setenv("HERDR_BIN", "")
		got := herdrBin()
		if got != f {
			t.Fatalf("want %q, got %q", f, got)
		}
	})

	t.Run("neither_env_var_set", func(t *testing.T) {
		t.Setenv("HERDR_BIN_PATH", "")
		t.Setenv("HERDR_BIN", "")
		got := herdrBin()
		if got != "herdr" {
			t.Fatalf("want %q, got %q", "herdr", got)
		}
	})

	t.Run("herdr_bin_fallback_deleted_suffix_file_exists", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "herdr-fallback")
		if err := os.WriteFile(f, []byte("x"), 0755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HERDR_BIN_PATH", "")
		t.Setenv("HERDR_BIN", f+" (deleted)")
		got := herdrBin()
		if got != f {
			t.Fatalf("want %q, got %q", f, got)
		}
	})
}

func TestNewTab_CorruptStore_ReturnsNonZero(t *testing.T) {
	stateParent := t.TempDir()
	stateDir := filepath.Join(stateParent, StateDirNS)
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	corrupt := filepath.Join(stateDir, "herdr-space-bindings.json")
	if err := os.WriteFile(corrupt, []byte("{not valid json"), 0600); err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(t.TempDir(), "herdr-ok")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\necho ok\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_BIN_PATH", fakeBin)
	t.Setenv("XDG_STATE_HOME", stateParent)

	var stdout, stderr bytes.Buffer
	code := runNewTab(context.Background(), []string{"wSomeID"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("corrupt store: want non-zero exit, got 0; stderr=%q stdout=%q", stderr.String(), stdout.String())
	}
}
