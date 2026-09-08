package msb

import (
	"testing"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
	msbsdk "github.com/superradcompany/microsandbox/sdk/go"
)

var _ coreruntime.Publisher = (*Runtime)(nil)

func TestSandboxOptionsWithPorts(t *testing.T) {
	spec := coreruntime.SandboxSpec{
		Name:  "test",
		Ports: []uint16{45455, 8080},
	}
	cfg := &msbsdk.SandboxConfig{}
	for _, opt := range SandboxOptions(spec) {
		opt(cfg)
	}
	if cfg.Ports[45455] != 45455 {
		t.Errorf("port 45455: got %d, want 45455", cfg.Ports[45455])
	}
	if cfg.Ports[8080] != 8080 {
		t.Errorf("port 8080: got %d, want 8080", cfg.Ports[8080])
	}
}

func TestSandboxOptionsNoPorts(t *testing.T) {
	spec := coreruntime.SandboxSpec{Name: "test"}
	cfg := &msbsdk.SandboxConfig{}
	for _, opt := range SandboxOptions(spec) {
		opt(cfg)
	}
	if len(cfg.Ports) != 0 {
		t.Errorf("expected no ports, got %v", cfg.Ports)
	}
}
