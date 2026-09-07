//go:build live

package livemsb_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IniZio/herdr-plugin-msb/internal/core/credmount/livemsb"
)

func TestSmoke(t *testing.T) {
	if os.Getenv("HERDR_MSB_LIVE") != "1" {
		t.Skip("HERDR_MSB_LIVE not set")
	}

	dir := t.TempDir()
	fmountPath := filepath.Join(dir, "fmount.txt")
	dirmountPath := filepath.Join(dir, "dirmount.txt")

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	const (
		valInit    = "val-init"
		valInplace = "val-inp1"
		valRename  = "val-rnm1"
		valDInpl   = "val-inp2"
		valDRnm    = "val-rnm2"
	)

	must(os.WriteFile(fmountPath, []byte(valInit), 0644))
	must(os.WriteFile(dirmountPath, []byte(valInit), 0644))

	sb := livemsb.RequireSandbox(t, livemsb.SandboxOpts{
		Name:      "s16-smoke",
		Image:     "alpine",
		MemoryMiB: 512,
		VCPUs:     1,
		FileMounts: []livemsb.BindMount{
			{HostPath: fmountPath, GuestPath: "/mnt/fmount.txt"},
		},
		DirMounts: []livemsb.BindMount{
			{HostPath: dir, GuestPath: "/mnt/dirmount"},
		},
	})

	ctx := context.Background()

	sh := func(script string) (string, int) {
		t.Helper()
		stdout, _, code, err := sb.Sh(ctx, script)
		if err != nil {
			t.Fatalf("sh %q: %v", script, err)
		}
		return strings.TrimSpace(stdout), code
	}

	inplace := func(path, content string) {
		t.Helper()
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
		must(err)
		_, werr := f.WriteString(content)
		cerr := f.Close()
		if werr != nil {
			t.Fatal(werr)
		}
		if cerr != nil {
			t.Fatal(cerr)
		}
	}

	atomic := func(targetDir, path, content string) {
		t.Helper()
		tmp, err := os.CreateTemp(targetDir, ".swap-*")
		must(err)
		_, werr := tmp.WriteString(content)
		cerr := tmp.Close()
		if werr != nil {
			t.Fatal(werr)
		}
		if cerr != nil {
			t.Fatal(cerr)
		}
		must(os.Rename(tmp.Name(), path))
	}

	got, code := sh("cat /mnt/fmount.txt")
	if code != 0 || got != valInit {
		t.Fatalf("file-mount initial: code=%d got=%q", code, got)
	}
	got, code = sh("cat /mnt/dirmount/dirmount.txt")
	if code != 0 || got != valInit {
		t.Fatalf("dir-mount initial: code=%d got=%q", code, got)
	}

	inplace(fmountPath, valInplace)
	got, code = sh("cat /mnt/fmount.txt")
	if code != 0 || got != valInplace {
		t.Fatalf("file-mount in-place: code=%d got=%q want %q", code, got, valInplace)
	}
	t.Logf("MEASURE file-mount in-place-write: visible=true")

	atomic(dir, fmountPath, valRename)
	got, _ = sh("cat /mnt/fmount.txt")
	fileMountRenameVisible := got == valRename
	t.Logf("MEASURE file-mount atomic-rename: visible=%v (guest saw %q)", fileMountRenameVisible, got)

	inplace(dirmountPath, valDInpl)
	got, code = sh("cat /mnt/dirmount/dirmount.txt")
	if code != 0 || got != valDInpl {
		t.Fatalf("dir-mount in-place: code=%d got=%q want %q", code, got, valDInpl)
	}
	t.Logf("MEASURE dir-mount in-place-write: visible=true")

	atomic(dir, dirmountPath, valDRnm)
	got, _ = sh("cat /mnt/dirmount/dirmount.txt")
	dirMountRenameVisible := got == valDRnm
	t.Logf("MEASURE dir-mount atomic-rename: visible=%v (guest saw %q)", dirMountRenameVisible, got)

	iout, _, _, _ := sb.Sh(ctx, "wget -qS --spider https://api.anthropic.com/ 2>&1; true")
	t.Logf("MEASURE internet HTTPS api.anthropic.com: %q", strings.TrimSpace(iout))
}
