package netprofile_test

import (
	"testing"

	"github.com/IniZio/herdr-plugin-msb/internal/core/netprofile"
	"github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

func TestShippedContainsAnthropicAllow(t *testing.T) {
	rules := netprofile.Shipped()
	for _, r := range rules {
		if r.Host == "api.anthropic.com" && r.Port == 443 && r.Action == runtime.NetAllow {
			return
		}
	}
	t.Fatal("shipped profile missing allow rule for api.anthropic.com:443")
}

func TestShippedNotEmpty(t *testing.T) {
	if len(netprofile.Shipped()) == 0 {
		t.Fatal("shipped profile is empty — implicit allow@public exposure")
	}
}

func TestShippedNoOpenPublicAllow(t *testing.T) {
	for _, r := range netprofile.Shipped() {
		if r.Action == runtime.NetAllow && (r.Host == "public" || r.Host == "*" || r.Host == "") {
			t.Fatalf("shipped profile contains open-public allow rule: %+v", r)
		}
	}
}

func TestCustomConfigReflected(t *testing.T) {
	cfg := netprofile.Config{
		Allow: []netprofile.Entry{
			{Host: "example.com", Port: 8080},
		},
	}
	rules, err := netprofile.Rules(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, r := range rules {
		if r.Host == "example.com" && r.Port == 8080 && r.Action == runtime.NetAllow {
			found = true
		}
	}
	if !found {
		t.Fatal("custom entry not reflected in rules")
	}
}

func TestEmptyConfigError(t *testing.T) {
	_, err := netprofile.Rules(netprofile.Config{})
	if err == nil {
		t.Fatal("expected error for empty config, got nil")
	}
}

func TestApplyPopulatesSpecWithShippedProfile(t *testing.T) {
	var spec runtime.SandboxSpec
	netprofile.Apply(&spec)
	if len(spec.NetRules) == 0 {
		t.Fatal("Apply left SandboxSpec.NetRules empty; msb treats an empty rule set as implicit allow@public")
	}
	for _, r := range spec.NetRules {
		if r.Action == runtime.NetAllow && r.Host == "public" {
			t.Fatalf("Apply produced an open-public allow: %+v", r)
		}
	}
}

func TestClosedIsNotEmpty(t *testing.T) {
	if len(netprofile.Closed()) == 0 {
		t.Fatal("Closed() is empty; an empty rule set is an OPEN policy, not a closed one")
	}
	for _, r := range netprofile.Closed() {
		if r.Action != runtime.NetDeny {
			t.Fatalf("Closed() contains a non-deny rule: %+v", r)
		}
	}
}

func TestShippedRuleSetIsExactlyTheLiveProvenProfile(t *testing.T) {
	want := []runtime.NetRule{
		{Action: runtime.NetAllow, Host: "api.anthropic.com", Port: 443},
	}
	got := netprofile.Shipped()
	if len(got) != len(want) {
		t.Fatalf("shipped rule count changed: got %d want %d; re-run the live reachability proof in doc/netprofile.md before editing this expectation", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("shipped rule %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestRoundTrip(t *testing.T) {
	rules := netprofile.Shipped()
	for _, r := range rules {
		if r.Host == "" {
			t.Fatalf("rule has empty Host: %+v", r)
		}
		if r.Action != runtime.NetAllow && r.Action != runtime.NetDeny {
			t.Fatalf("rule has invalid Action: %+v", r)
		}
	}
}
