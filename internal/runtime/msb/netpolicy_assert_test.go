package msb

import (
	"strings"
	"testing"
)

const s19DaemonRecordDeny = `{"name":"s19-unit","network":{"enabled":true,"policy":{"default_egress":"deny","default_ingress":"allow","rules":[{"direction":"egress","destination":{"domain":"api.anthropic.com"},"protocols":["tcp"],"ports":[{"start":443,"end":443}],"action":"allow"}]}}}`

func TestCheckNetworkPolicy_acceptsDenyDefault(t *testing.T) {
	if err := checkNetworkPolicy("s19-unit", s19DaemonRecordDeny); err != nil {
		t.Fatalf("real deny-default daemon record rejected: %v", err)
	}
}

func TestCheckNetworkPolicy_rejectsMissingNetwork(t *testing.T) {
	err := checkNetworkPolicy("s19-unit", `{"name":"s19-unit"}`)
	if err == nil {
		t.Fatal("a daemon record with no network stanza was accepted")
	}
	if !strings.Contains(err.Error(), "s19-unit") || !strings.Contains(err.Error(), "absent") {
		t.Fatalf("error does not name the sandbox and the finding: %v", err)
	}
}

func TestCheckNetworkPolicy_rejectsMissingPolicy(t *testing.T) {
	if err := checkNetworkPolicy("s19-unit", `{"network":{"enabled":true}}`); err == nil {
		t.Fatal("network stanza with no policy was accepted")
	}
}

func TestCheckNetworkPolicy_rejectsAllowDefault(t *testing.T) {
	err := checkNetworkPolicy("s19-unit", `{"network":{"enabled":true,"policy":{"default_egress":"allow"}}}`)
	if err == nil {
		t.Fatal("default_egress=allow was accepted")
	}
	if !strings.Contains(err.Error(), `default_egress="allow"`) {
		t.Fatalf("error does not report what was actually read: %v", err)
	}
}

func TestCheckNetworkPolicy_rejectsEmptyRecord(t *testing.T) {
	if err := checkNetworkPolicy("s19-unit", ""); err == nil {
		t.Fatal("empty daemon record was accepted")
	}
}
