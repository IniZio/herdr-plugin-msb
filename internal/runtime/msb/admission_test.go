package msb

import (
	"strings"
	"testing"
)

func TestMemoryMiBFromRecord(t *testing.T) {
	tests := []struct {
		name       string
		configJSON string
		wantMiB    uint32
		wantErr    string
	}{
		{
			name:       "resources.memory_mib present",
			configJSON: `{"resources":{"memory_mib":2048},"memory_mib":0}`,
			wantMiB:    2048,
		},
		{
			name:       "resources absent top-level memory_mib",
			configJSON: `{"memory_mib":512}`,
			wantMiB:    512,
		},
		{
			name:       "resources present but zero top-level set uses top-level",
			configJSON: `{"resources":{"memory_mib":0},"memory_mib":1024}`,
			wantMiB:    1024,
		},
		{
			name:       "malformed JSON",
			configJSON: `{not valid json`,
			wantErr:    "parse config record",
		},
		{
			name:       "empty string",
			configJSON: "",
			wantErr:    "config record is empty",
		},
		{
			name:       "both paths absent or zero",
			configJSON: `{"resources":{"memory_mib":0},"memory_mib":0}`,
			wantErr:    "memory_mib absent or zero",
		},
		{
			name: "realistic full daemon record",
			configJSON: `{
				"id":"abc123",
				"name":"s01--foo",
				"status":"running",
				"resources":{"memory_mib":4096,"max_memory_mib":4096,"vcpus":2},
				"memory_mib":1024,
				"network":{"enabled":true,"policy":{"default_egress":"deny"}},
				"labels":{"herdr.project":"s01","herdr.motive":"test"},
				"created_at":"2026-01-01T00:00:00Z"
			}`,
			wantMiB: 4096,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := memoryMiBFromRecord(tc.configJSON)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil (mib=%d)", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantMiB {
				t.Fatalf("got %d MiB, want %d MiB", got, tc.wantMiB)
			}
		})
	}
}
