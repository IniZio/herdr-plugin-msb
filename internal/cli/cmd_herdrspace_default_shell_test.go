package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/IniZio/herdr-plugin-msb/internal/core/herdrspace"
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
