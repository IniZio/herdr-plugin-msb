package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IniZio/herdr-plugin-msb/internal/core/herdrspace"
	"github.com/IniZio/herdr-plugin-msb/internal/core/service"
)

func writeTestBindings(t *testing.T, dir string, bs []herdrspace.Binding) {
	t.Helper()
	data, err := json.Marshal(bs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "herdr-space-bindings.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestGuestShellArgv(t *testing.T) {
	cases := []struct {
		shell   string
		wantL   bool
	}{
		{"/bin/bash", true},
		{"/usr/bin/bash", true},
		{"/bin/sh", false},
		{"/usr/bin/zsh", false},
	}
	for _, tc := range cases {
		argv := guestShellArgv("/plugin", "proj", "box", tc.shell)
		if len(argv) < 11 {
			t.Fatalf("shell=%s: argv too short: %v", tc.shell, argv)
		}
		if argv[7] != "/bin/sh" {
			t.Errorf("shell=%s: argv[7] want /bin/sh, got %s", tc.shell, argv[7])
		}
		if argv[8] != "-c" {
			t.Errorf("shell=%s: argv[8] want -c, got %s", tc.shell, argv[8])
		}
		cdScript := argv[9]
		if !strings.Contains(cdScript, "cd "+service.DefaultGuestWorktree) {
			t.Errorf("shell=%s: cd script missing cd %s: %s", tc.shell, service.DefaultGuestWorktree, cdScript)
		}
		if !strings.Contains(cdScript, "|| cd /") {
			t.Errorf("shell=%s: cd script missing fallback: %s", tc.shell, cdScript)
		}
		if argv[10] != tc.shell {
			t.Errorf("shell=%s: $0 want %s, got %s", tc.shell, tc.shell, argv[10])
		}
		if strings.Contains(cdScript, tc.shell) {
			t.Errorf("shell=%s: guest shell must not be embedded in -c string", tc.shell)
		}
		if tc.wantL {
			if len(argv) < 12 || argv[11] != "-l" {
				t.Errorf("shell=%s: want -l in $@, argv=%v", tc.shell, argv)
			}
		} else {
			for _, a := range argv[11:] {
				if a == "-l" {
					t.Errorf("shell=%s: unexpected -l in argv=%v", tc.shell, argv)
				}
			}
		}
	}
}

func TestRunDefaultShellStubExec(t *testing.T) {
	t.Setenv("HERDR_WORKSPACE_ID", "")
	t.Setenv("HERDR_MSB_DEFAULT_SHELL_ACTIVE", "")

	var capturedArgv0 string
	var capturedArgv []string
	old := execProcess
	execProcess = func(argv0 string, argv []string, _ []string) error {
		capturedArgv0 = argv0
		capturedArgv = argv
		return nil
	}
	defer func() { execProcess = old }()

	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}

	var errBuf strings.Builder
	runDefaultShell(context.Background(), nil, io.Discard, &errBuf)

	if capturedArgv0 != sh {
		t.Fatalf("stub: argv0 want %q got %q", sh, capturedArgv0)
	}
	if len(capturedArgv) < 1 || capturedArgv[0] != sh {
		t.Fatalf("stub: argv[0] want %q got %v", sh, capturedArgv)
	}
}

func TestRunDefaultShellGuardUnderTest(t *testing.T) {
	t.Setenv("HERDR_WORKSPACE_ID", "")
	t.Setenv("HERDR_MSB_DEFAULT_SHELL_ACTIVE", "")

	var errBuf strings.Builder
	code := runDefaultShell(context.Background(), nil, io.Discard, &errBuf)

	if code == 0 {
		t.Fatalf("want non-zero exit code under go test, got 0 (errW: %q)", errBuf.String())
	}
	sentinel := true
	if !sentinel {
		t.Fatal("unreachable: process was replaced")
	}
	if !strings.Contains(errBuf.String(), "refused") {
		t.Fatalf("want 'refused' in error output, got %q", errBuf.String())
	}
}

func TestDefaultShellDecide(t *testing.T) {
	ctx := context.Background()

	notFound := func(context.Context, string, string) (herdrspace.Binding, error) {
		return herdrspace.Binding{}, herdrspace.ErrNotFound
	}
	lookupErr := func(context.Context, string, string) (herdrspace.Binding, error) {
		return herdrspace.Binding{}, errors.New("db error")
	}
	env := func(k string) string {
		if k == "HERDR_WORKSPACE_ID" {
			return "ws-123"
		}
		return ""
	}

	t.Run("sentinel set", func(t *testing.T) {
		getenv := func(k string) string {
			if k == defaultShellSentinel {
				return "1"
			}
			return env(k)
		}
		if defaultShellDecide(ctx, getenv, "/any", notFound).useGuest {
			t.Fatal("want host when sentinel set")
		}
	})

	t.Run("empty workspace id", func(t *testing.T) {
		if defaultShellDecide(ctx, func(string) string { return "" }, "/any", notFound).useGuest {
			t.Fatal("want host when no workspace id")
		}
	})

	t.Run("ErrNotFound", func(t *testing.T) {
		if defaultShellDecide(ctx, env, "/any", notFound).useGuest {
			t.Fatal("want host on ErrNotFound")
		}
	})

	t.Run("lookup error", func(t *testing.T) {
		if defaultShellDecide(ctx, env, "/any", lookupErr).useGuest {
			t.Fatal("want host on lookup error")
		}
	})

	t.Run("bound workspace", func(t *testing.T) {
		dir := t.TempDir()
		writeTestBindings(t, dir, []herdrspace.Binding{
			{SpaceLabel: "s", HerdrWorkspaceID: "ws-123", SandboxHandle: "proj/box"},
		})
		dec := defaultShellDecide(ctx, env, dir, herdrspace.GetByWorkspaceID)
		if !dec.useGuest {
			t.Fatal("want guest for bound workspace")
		}
		if dec.project != "proj" || dec.sandbox != "box" {
			t.Fatalf("wrong project/sandbox: %q %q", dec.project, dec.sandbox)
		}
	})
}
