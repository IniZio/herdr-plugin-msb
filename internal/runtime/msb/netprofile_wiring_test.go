package msb

import (
	"testing"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
	msbsdk "github.com/superradcompany/microsandbox/sdk/go"
)

func builtNetwork(t *testing.T, spec coreruntime.SandboxSpec) *msbsdk.NetworkConfig {
	t.Helper()
	var cfg msbsdk.SandboxConfig
	for _, o := range SandboxOptions(spec) {
		o(&cfg)
	}
	return cfg.Network
}

func TestSandboxOptions_appliesShippedProfileToBareSpec(t *testing.T) {
	net := builtNetwork(t, coreruntime.SandboxSpec{})
	if net == nil {
		t.Fatal("SandboxOptions built a nil Network for a bare spec — unfiltered egress")
	}
	if net.DefaultEgress != msbsdk.PolicyActionDeny {
		t.Fatalf("DefaultEgress = %v, want Deny", net.DefaultEgress)
	}
	found := false
	for _, r := range net.Rules {
		if r.Destination == "api.anthropic.com" && r.Port == "443" &&
			r.Action == msbsdk.PolicyActionAllow &&
			r.Direction == msbsdk.PolicyDirectionEgress {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no allow rule for api.anthropic.com:443 in %v", net.Rules)
	}
}

func TestSandboxOptions_keepsExistingRules(t *testing.T) {
	custom := []coreruntime.NetRule{{Action: coreruntime.NetAllow, Host: "example.com", Port: 80}}
	net := builtNetwork(t, coreruntime.SandboxSpec{NetRules: custom})
	if net == nil {
		t.Fatal("expected non-nil Network for a spec with custom rules")
	}
	if net.DefaultEgress != msbsdk.PolicyActionDeny {
		t.Fatalf("DefaultEgress = %v, want Deny", net.DefaultEgress)
	}
	for _, r := range net.Rules {
		if r.Destination == "api.anthropic.com" {
			t.Fatalf("custom rules were overwritten by the shipped profile: %v", net.Rules)
		}
	}
	found := false
	for _, r := range net.Rules {
		if r.Destination == "example.com" && r.Port == "80" {
			found = true
		}
	}
	if !found {
		t.Fatalf("custom rule example.com:80 missing from %v", net.Rules)
	}
}
