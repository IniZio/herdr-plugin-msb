package runtime

import (
	"testing"
)

func TestSamePortMap(t *testing.T) {
	m := SamePortMap([]uint16{80, 443, 8080})
	for _, p := range []uint16{80, 443, 8080} {
		if m[p] != p {
			t.Errorf("SamePortMap: port %d mapped to %d", p, m[p])
		}
	}
}

func TestValidatePorts(t *testing.T) {
	cases := []struct {
		name    string
		ports   []uint16
		wantErr bool
	}{
		{"ok", []uint16{80, 443}, false},
		{"zero", []uint16{0}, true},
		{"duplicate", []uint16{80, 80}, true},
		{"zero_and_valid", []uint16{8080, 0}, true},
	}
	for _, tc := range cases {
		err := ValidatePorts(tc.ports)
		if tc.wantErr && err == nil {
			t.Errorf("%s: expected error, got nil", tc.name)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
		}
	}
}
