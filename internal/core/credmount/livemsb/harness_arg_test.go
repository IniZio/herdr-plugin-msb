package livemsb

import (
	"testing"
)

func TestBuildArgsDirMountProducesMountDir(t *testing.T) {
	o := SandboxOpts{
		Name:      "s16-arg-test",
		Image:     "alpine",
		MemoryMiB: 512,
		VCPUs:     1,
		DirMounts: []BindMount{{HostPath: "/host/dir", GuestPath: "/guest/dir"}},
	}
	args := buildArgs(o)
	hasMountDir := false
	for i, a := range args {
		if a == "--mount-file" {
			t.Fatalf("DirMounts produced --mount-file at args[%d]: full argv=%v", i, args)
		}
		if a == "--mount-dir" {
			hasMountDir = true
		}
	}
	if !hasMountDir {
		t.Fatalf("DirMounts did not produce --mount-dir: full argv=%v", args)
	}
}

func TestBuildArgsFileMountProducesMountFile(t *testing.T) {
	o := SandboxOpts{
		Name:      "s16-arg-test",
		Image:     "alpine",
		MemoryMiB: 512,
		VCPUs:     1,
		FileMounts: []BindMount{{HostPath: "/host/file.json", GuestPath: "/guest/file.json"}},
	}
	args := buildArgs(o)
	hasMountFile := false
	for i, a := range args {
		if a == "--mount-dir" {
			t.Fatalf("FileMounts produced --mount-dir at args[%d]: full argv=%v", i, args)
		}
		if a == "--mount-file" {
			hasMountFile = true
		}
	}
	if !hasMountFile {
		t.Fatalf("FileMounts did not produce --mount-file: full argv=%v", args)
	}
}

func TestMountArg(t *testing.T) {
	tests := []struct {
		m    BindMount
		want string
	}{
		{BindMount{HostPath: "/h", GuestPath: "/g"}, "/h:/g"},
		{BindMount{HostPath: "/h", GuestPath: "/g", ReadOnly: true}, "/h:/g:ro"},
		{BindMount{HostPath: "/h", GuestPath: "/g", Noexec: true}, "/h:/g:noexec"},
		{BindMount{HostPath: "/h", GuestPath: "/g", ReadOnly: true, Noexec: true, Nosuid: true, Nodev: true}, "/h:/g:ro,noexec,nosuid,nodev"},
	}
	for _, tt := range tests {
		if got := mountArg(tt.m); got != tt.want {
			t.Errorf("mountArg(%+v) = %q, want %q", tt.m, got, tt.want)
		}
	}
}
