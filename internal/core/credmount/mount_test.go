package credmount

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileMountBasic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")
	m, err := FileMount(path, "/run/creds.json", true)
	if err != nil {
		t.Fatal(err)
	}
	if m.HostPath != path || m.GuestPath != "/run/creds.json" || !m.ReadOnly {
		t.Fatalf("unexpected mount: %+v", m)
	}
}

func TestDirMountBasic(t *testing.T) {
	dir := t.TempDir()
	m, err := DirMount(dir, "/run/creds", false)
	if err != nil {
		t.Fatal(err)
	}
	if m.HostPath != dir || m.GuestPath != "/run/creds" || m.ReadOnly {
		t.Fatalf("unexpected mount: %+v", m)
	}
}

func TestRelativePathRejectedFile(t *testing.T) {
	_, err := FileMount("relative/path.json", "/guest/path", false)
	if !errors.Is(err, ErrRelativePath) {
		t.Fatalf("expected ErrRelativePath, got %v", err)
	}
}

func TestRelativePathRejectedDir(t *testing.T) {
	_, err := DirMount("relative/dir", "/guest/dir", false)
	if !errors.Is(err, ErrRelativePath) {
		t.Fatalf("expected ErrRelativePath, got %v", err)
	}
}

func TestAC5FileMountCredentialPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0700); err != nil {
		t.Fatal(err)
	}
	credPath := filepath.Join(claudeDir, ".credentials.json")

	_, err := FileMount(credPath, "/guest/creds.json", true)
	if !errors.Is(err, ErrProtectedCredentialPath) {
		t.Fatalf("expected ErrProtectedCredentialPath, got %v", err)
	}
}

func TestAC5DirMountClaudeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0700); err != nil {
		t.Fatal(err)
	}

	_, err := DirMount(claudeDir, "/guest/.claude", true)
	if !errors.Is(err, ErrProtectedCredentialPath) {
		t.Fatalf("expected ErrProtectedCredentialPath for .claude dir, got %v", err)
	}
}

func TestAC5SubpathRejected(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	subPath := filepath.Join(home, ".claude", "subdir", "file.json")

	_, err := FileMount(subPath, "/guest/file.json", true)
	if !errors.Is(err, ErrProtectedCredentialPath) {
		t.Fatalf("expected ErrProtectedCredentialPath for .claude subpath, got %v", err)
	}
}

func TestNonClaudePathAllowed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	otherPath := filepath.Join(home, ".config", "creds.json")

	_, err := FileMount(otherPath, "/guest/creds.json", true)
	if errors.Is(err, ErrProtectedCredentialPath) {
		t.Fatal("non-.claude path should not be protected")
	}
}
