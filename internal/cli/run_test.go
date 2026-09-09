package cli

import (
	"slices"
	"testing"
)

func TestNormalizeArgv(t *testing.T) {
	cases := []struct {
		name string
		argv0 string
		args  []string
		want  []string
	}{
		{
			name:  "absolute_path_guest_shell_no_args",
			argv0: "/usr/local/bin/herdr-plugin-msb-guest-shell",
			args:  nil,
			want:  []string{"default-shell"},
		},
		{
			name:  "bare_basename_guest_shell_no_args",
			argv0: "herdr-plugin-msb-guest-shell",
			args:  nil,
			want:  []string{"default-shell"},
		},
		{
			name:  "normal_binary_no_args",
			argv0: "/home/newman/.local/bin/herdr-plugin-msb",
			args:  []string{},
			want:  []string{},
		},
		{
			name:  "normal_binary_with_args_not_prefixed",
			argv0: "/home/newman/.local/bin/herdr-plugin-msb",
			args:  []string{"new-tab", "w8T"},
			want:  []string{"new-tab", "w8T"},
		},
		{
			name:  "guest_shell_with_trailing_args",
			argv0: "/usr/local/bin/herdr-plugin-msb-guest-shell",
			args:  []string{"--login", "-c", "bash"},
			want:  []string{"default-shell", "--login", "-c", "bash"},
		},
		{
			name:  "guest_shell_empty_args_slice",
			argv0: "herdr-plugin-msb-guest-shell",
			args:  []string{},
			want:  []string{"default-shell"},
		},
		{
			name:  "guest_shell_does_not_mutate_caller_backing_array",
			argv0: "herdr-plugin-msb-guest-shell",
			args:  nil,
			want:  []string{"default-shell"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var snapshot []string
			if tc.args != nil {
				snapshot = slices.Clone(tc.args)
			}

			got := NormalizeArgv(tc.argv0, tc.args)

			if !slices.Equal(got, tc.want) {
				t.Fatalf("NormalizeArgv(%q, %v) = %v, want %v", tc.argv0, tc.args, got, tc.want)
			}

			if tc.args != nil && !slices.Equal(tc.args, snapshot) {
				t.Fatalf("NormalizeArgv mutated caller slice: before=%v after=%v", snapshot, tc.args)
			}
		})
	}
}

func TestNormalizeArgv_NoBackingArrayAlias(t *testing.T) {
	spare := make([]string, 2, 8)
	spare[0] = "original-0"
	spare[1] = "original-1"
	input := spare[2:2]

	got := NormalizeArgv("herdr-plugin-msb-guest-shell", input)

	got = append(got, "extra")

	if spare[0] != "original-0" || spare[1] != "original-1" {
		t.Fatalf("NormalizeArgv result aliases caller backing array: spare=%v", spare)
	}
	_ = got
}
