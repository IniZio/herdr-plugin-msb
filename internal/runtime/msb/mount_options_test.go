package msb

import (
	"testing"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
	msbsdk "github.com/superradcompany/microsandbox/sdk/go"
)

func TestSandboxOptions_MountHardeningFlags(t *testing.T) {
	spec := coreruntime.SandboxSpec{
		ImageRef: "test-image",
		Mounts: []coreruntime.Mount{
			{HostPath: "/host/ro", GuestPath: "/ro", ReadOnly: true, Noexec: true, Nosuid: true, Nodev: true},
			{HostPath: "/host/rw", GuestPath: "/rw", ReadOnly: false, Noexec: false, Nosuid: false, Nodev: false},
		},
	}

	opts := SandboxOptions(spec)

	var cfg msbsdk.SandboxConfig
	for _, o := range opts {
		o(&cfg)
	}

	tests := []struct {
		guest    string
		host     string
		readonly bool
		noexec   bool
		nosuid   bool
		nodev    bool
	}{
		{"/ro", "/host/ro", true, true, true, true},
		{"/rw", "/host/rw", false, false, false, false},
	}

	for _, tc := range tests {
		mc, ok := cfg.Volumes[tc.guest]
		if !ok {
			t.Fatalf("guest path %q missing from cfg.Volumes", tc.guest)
		}
		if mc.Bind != tc.host {
			t.Errorf("guest %q: host path: got %q, want %q", tc.guest, mc.Bind, tc.host)
		}
		if mc.Readonly != tc.readonly {
			t.Errorf("guest %q: Readonly: got %v, want %v", tc.guest, mc.Readonly, tc.readonly)
		}
		if mc.Noexec != tc.noexec {
			t.Errorf("guest %q: Noexec: got %v, want %v", tc.guest, mc.Noexec, tc.noexec)
		}
		if mc.Nosuid != tc.nosuid {
			t.Errorf("guest %q: Nosuid: got %v, want %v", tc.guest, mc.Nosuid, tc.nosuid)
		}
		if mc.Nodev != tc.nodev {
			t.Errorf("guest %q: Nodev: got %v, want %v", tc.guest, mc.Nodev, tc.nodev)
		}
	}
}
