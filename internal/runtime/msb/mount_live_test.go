package msb

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

func setupGitWorktree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "init.txt"), []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "init.txt")
	run("commit", "-m", "init")
	return dir
}

func TestLiveMountRoundTrip(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)

	rwDir := setupGitWorktree(t)
	roDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(roDir, "ro.txt"), []byte("readonly\n"), 0644); err != nil {
		t.Fatal(err)
	}
	noexecDir := t.TempDir()
	nosuidDir := t.TempDir()
	nodevDir := t.TempDir()

	script := []byte("#!/bin/sh\necho noexec-ran\n")
	if err := os.WriteFile(filepath.Join(noexecDir, "probe.sh"), script, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rwDir, "probe.sh"), script, 0755); err != nil {
		t.Fatal(err)
	}

	r := New()
	spec := LiveSpec("mount")
	spec.Mounts = []coreruntime.Mount{
		{HostPath: rwDir, GuestPath: "/work", ReadOnly: false},
		{HostPath: roDir, GuestPath: "/ro", ReadOnly: true},
		{HostPath: noexecDir, GuestPath: "/noexec", ReadOnly: false, Noexec: true},
		{HostPath: nosuidDir, GuestPath: "/nosuid", ReadOnly: false, Nosuid: true},
		{HostPath: nodevDir, GuestPath: "/nodev", ReadOnly: false, Nodev: true},
	}

	ref, err := r.CreateAndBoot(ctx, spec)
	if err != nil {
		t.Fatalf("CreateAndBoot: %v", err)
	}
	CleanupSandbox(t, r, ref)

	sb, err := r.connect(ctx, ref)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = sb.Detach(ctx) }()

	guestWrite, err := sb.Exec(ctx, "sh", []string{"-c",
		`printf 'guest-new\n' > /work/guest.txt && printf 'appended\n' >> /work/init.txt`})
	if err != nil {
		t.Fatalf("guest write exec: %v", err)
	}
	t.Logf("AC1 RW write: exit=%d stderr=%q", guestWrite.ExitCode(), guestWrite.Stderr())
	if guestWrite.ExitCode() != 0 {
		t.Fatalf("guest write exit %d stderr: %s", guestWrite.ExitCode(), guestWrite.Stderr())
	}

	gotNew, err := os.ReadFile(filepath.Join(rwDir, "guest.txt"))
	if err != nil {
		t.Fatalf("host read guest.txt: %v", err)
	}
	t.Logf("AC1 RW host-side guest.txt bytes: %q", gotNew)
	if strings.TrimSpace(string(gotNew)) != "guest-new" {
		t.Fatalf("guest.txt on host = %q, want \"guest-new\"", gotNew)
	}

	gotInit, err := os.ReadFile(filepath.Join(rwDir, "init.txt"))
	if err != nil {
		t.Fatalf("host read init.txt: %v", err)
	}
	if !strings.Contains(string(gotInit), "appended") {
		t.Fatalf("init.txt on host missing append: %q", gotInit)
	}

	gsOut, err := exec.Command("git", "-C", rwDir, "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	if !strings.Contains(string(gsOut), "guest.txt") {
		t.Fatalf("git status missing guest.txt: %q", gsOut)
	}

	if err := os.WriteFile(filepath.Join(rwDir, "host-a.txt"), []byte("host-alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rwDir, "host-b.txt"), []byte("host-beta\n"), 0644); err != nil {
		t.Fatal(err)
	}

	guestRead, err := sb.Exec(ctx, "sh", []string{"-c", "cat /work/host-a.txt && cat /work/host-b.txt"})
	if err != nil {
		t.Fatalf("guest read exec: %v", err)
	}
	if guestRead.ExitCode() != 0 {
		t.Fatalf("guest read exit %d stderr: %s", guestRead.ExitCode(), guestRead.Stderr())
	}
	combined := guestRead.Stdout()
	if !strings.Contains(combined, "host-alpha") {
		t.Fatalf("guest did not see host-a.txt: %q", combined)
	}
	if !strings.Contains(combined, "host-beta") {
		t.Fatalf("guest did not see host-b.txt: %q", combined)
	}

	roWrite, err := sb.Exec(ctx, "sh", []string{"-c", "echo fail > /ro/ro.txt 2>&1; echo fail > /ro/ro.txt"})
	if err != nil {
		t.Fatalf("ro write exec: %v", err)
	}
	t.Logf("AC1 RO write: exit=%d stderr=%q", roWrite.ExitCode(), roWrite.Stderr())
	if roWrite.ExitCode() == 0 {
		t.Fatal("write to read-only mount succeeded; expected non-zero exit")
	}
	if roWrite.Stderr() == "" {
		t.Fatal("write to read-only mount produced no stderr")
	}

	roBytes, err := os.ReadFile(filepath.Join(roDir, "ro.txt"))
	if err != nil {
		t.Fatalf("host read ro.txt after failed write: %v", err)
	}
	t.Logf("AC1 RO host-side ro.txt bytes after failed write: %q (must equal \"readonly\\n\")", roBytes)
	if string(roBytes) != "readonly\n" {
		t.Fatalf("ro.txt on host changed after failed write: %q", roBytes)
	}

	procMounts, err := sb.Exec(ctx, "sh", []string{"-c",
		"grep -E ' /ro | /work | /noexec | /nosuid | /nodev ' /proc/mounts"})
	if err != nil {
		t.Fatalf("proc/mounts exec: %v", err)
	}
	t.Logf("AC2 /proc/mounts relevant lines:\n%s", procMounts.Stdout())

	noexecExec, err := sb.Exec(ctx, "sh", []string{"-c", "/noexec/probe.sh"})
	if err != nil {
		t.Fatalf("noexec exec attempt: %v", err)
	}
	t.Logf("AC2 noexec execute attempt: exit=%d stderr=%q stdout=%q", noexecExec.ExitCode(), noexecExec.Stderr(), noexecExec.Stdout())

	rwExec, err := sb.Exec(ctx, "sh", []string{"-c", "/work/probe.sh"})
	if err != nil {
		t.Fatalf("rw exec attempt: %v", err)
	}
	t.Logf("AC2 rw (no noexec) execute: exit=%d stdout=%q", rwExec.ExitCode(), rwExec.Stdout())

	if noexecExec.ExitCode() == 0 {
		t.Error("AC2 noexec: script executed successfully on noexec mount; expected failure")
	}
	if rwExec.ExitCode() != 0 {
		t.Errorf("AC2 noexec control: script failed on rw mount (exit %d); probe is invalid", rwExec.ExitCode())
	}
}
