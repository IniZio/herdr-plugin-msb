package livemsb

import "testing"

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
