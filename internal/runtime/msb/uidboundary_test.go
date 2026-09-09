package msb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeHelper(t *testing.T, exitCode int, recordFile string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "helper.sh")
	content := fmt.Sprintf("#!/bin/sh\necho \"$@\" > %s\nexit %d\n", recordFile, exitCode)
	if err := os.WriteFile(script, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	return script
}

func TestNewUidBoundary_CapturesUID(t *testing.T) {
	u := NewUidBoundary("/fake/path")
	if u.ownerUID != os.Getuid() {
		t.Fatalf("ownerUID=%d want %d", u.ownerUID, os.Getuid())
	}
	if u.helperPath != "/fake/path" {
		t.Fatalf("helperPath=%q want /fake/path", u.helperPath)
	}
}

func TestUidBoundary_Apply_BuildsArgs(t *testing.T) {
	record := filepath.Join(t.TempDir(), "args.txt")
	helper := makeHelper(t, 0, record)
	u := NewUidBoundary(helper)

	if err := u.Apply(context.Background(), 5000); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("apply 5000 %d", os.Getuid())
	if strings.TrimSpace(string(got)) != want {
		t.Fatalf("args=%q want %q", strings.TrimSpace(string(got)), want)
	}
}

func TestUidBoundary_Apply_Error(t *testing.T) {
	record := filepath.Join(t.TempDir(), "args.txt")
	helper := makeHelper(t, 1, record)
	u := NewUidBoundary(helper)

	err := u.Apply(context.Background(), 5000)
	if err == nil {
		t.Fatal("expected error from failing helper")
	}
}

func TestUidBoundary_Remove_BuildsArgs(t *testing.T) {
	record := filepath.Join(t.TempDir(), "args.txt")
	helper := makeHelper(t, 0, record)
	u := NewUidBoundary(helper)

	if err := u.Remove(context.Background(), 15000); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "remove 15000" {
		t.Fatalf("args=%q want \"remove 15000\"", strings.TrimSpace(string(got)))
	}
}

func TestUidBoundary_Remove_Error(t *testing.T) {
	record := filepath.Join(t.TempDir(), "args.txt")
	helper := makeHelper(t, 1, record)
	u := NewUidBoundary(helper)

	err := u.Remove(context.Background(), 15000)
	if err == nil {
		t.Fatal("expected error from failing helper")
	}
}
