package cli

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

const testIsolationMarker = "HERDR_MSB_TEST_ISOLATED"

const liveSocketSuffix = "/.config/herdr/herdr.sock"

const isolationHint = "run the suite via `make test`, which starts a throwaway headless herdr server; " +
	"see doc/test-isolation.md"

func isolationViolation(getenv func(string) string) string {
	if getenv(testIsolationMarker) != "1" {
		return testIsolationMarker + " is not set: the suite is not running under the isolated test session"
	}
	if ws := getenv("HERDR_WORKSPACE_ID"); ws != "" {
		return "HERDR_WORKSPACE_ID is set to " + ws + ": a live workspace is reachable from the test binary"
	}
	sock := getenv("HERDR_SOCKET_PATH")
	if sock == "" {
		return "HERDR_SOCKET_PATH is empty: herdr would fall back to the default session socket"
	}
	if strings.HasSuffix(sock, liveSocketSuffix) {
		return "HERDR_SOCKET_PATH points at the default session socket " + sock
	}
	if getenv("XDG_STATE_HOME") == "" {
		return "XDG_STATE_HOME is empty: StateDir would resolve the live herdr-space-bindings.json"
	}
	return ""
}

func TestIsolationViolationOppositeOutcomes(t *testing.T) {
	isolated := map[string]string{
		testIsolationMarker:  "1",
		"HERDR_SOCKET_PATH":  "/tmp/herdr-test-abc/herdr.sock",
		"XDG_STATE_HOME":     "/tmp/herdr-test-abc/state",
		"HERDR_WORKSPACE_ID": "",
	}
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	if why := isolationViolation(env(isolated)); why != "" {
		t.Fatalf("isolated env rejected: %s", why)
	}

	for name, mutate := range map[string]func(map[string]string){
		"no marker":     func(m map[string]string) { delete(m, testIsolationMarker) },
		"live socket":   func(m map[string]string) { m["HERDR_SOCKET_PATH"] = "/home/x/.config/herdr/herdr.sock" },
		"empty socket":  func(m map[string]string) { m["HERDR_SOCKET_PATH"] = "" },
		"live wsid":     func(m map[string]string) { m["HERDR_WORKSPACE_ID"] = "w8" },
		"no state home": func(m map[string]string) { m["XDG_STATE_HOME"] = "" },
	} {
		bad := map[string]string{}
		for k, v := range isolated {
			bad[k] = v
		}
		mutate(bad)
		if why := isolationViolation(env(bad)); why == "" {
			t.Errorf("%s: guard did not fire", name)
		}
	}
}

func TestMain(m *testing.M) {
	if why := isolationViolation(os.Getenv); why != "" {
		fmt.Fprintf(os.Stderr,
			"internal/cli: refusing to run tests against a non-isolated herdr environment.\n"+
				"  reason: %s\n"+
				"  this suite invokes every shipped verb, including space-convert and default-shell;\n"+
				"  it has twice converted the operator's primary workspace into a live sandbox.\n"+
				"  %s\n", why, isolationHint)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
