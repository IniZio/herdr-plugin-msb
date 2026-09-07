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

	r := New()
	spec := LiveSpec("mount")
	spec.Mounts = []coreruntime.Mount{
		{HostPath: rwDir, GuestPath: "/work", ReadOnly: false},
		{HostPath: roDir, GuestPath: "/ro", ReadOnly: true},
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
	if guestWrite.ExitCode() != 0 {
		t.Fatalf("guest write exit %d stderr: %s", guestWrite.ExitCode(), guestWrite.Stderr())
	}

	gotNew, err := os.ReadFile(filepath.Join(rwDir, "guest.txt"))
	if err != nil {
		t.Fatalf("host read guest.txt: %v", err)
	}
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

	roWrite, err := sb.Exec(ctx, "sh", []string{"-c", "echo fail > /ro/ro.txt"})
	if err != nil {
		t.Fatalf("ro write exec: %v", err)
	}
	if roWrite.ExitCode() == 0 {
		t.Fatal("write to read-only mount succeeded; expected non-zero exit")
	}
	if roWrite.Stderr() == "" {
		t.Fatal("write to read-only mount produced no stderr")
	}
}
