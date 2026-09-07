package msb

import (
	"testing"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
	msbsdk "github.com/superradcompany/microsandbox/sdk/go"
)

func TestWithDefaultNetProfile_appliesShippedWhenEmpty(t *testing.T) {
	spec := coreruntime.SandboxSpec{}
	got := withDefaultNetProfile(spec)
	net := networkConfig(got.NetRules)
	if net == nil {
		t.Fatal("expected non-nil NetworkConfig for unconfigured spec")
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

func TestWithDefaultNetProfile_keepsExistingRules(t *testing.T) {
	custom := []coreruntime.NetRule{{Action: coreruntime.NetAllow, Host: "example.com", Port: 80}}
	spec := coreruntime.SandboxSpec{NetRules: custom}
	got := withDefaultNetProfile(spec)
	if len(got.NetRules) != 1 || got.NetRules[0].Host != "example.com" {
		t.Fatalf("custom rules were overwritten: %v", got.NetRules)
	}
}
