package cli

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/IniZio/herdr-plugin-msb/internal/core/portfwd"
)

func argIn(argv []string, v string) bool {
	for _, a := range argv {
		if a == v {
			return true
		}
	}
	return false
}

func argsIn(argv []string, want ...string) bool {
	for _, w := range want {
		if !argIn(argv, w) {
			return false
		}
	}
	return true
}

func anyCall(calls [][]string, want ...string) bool {
	for _, c := range calls {
		if argsIn(c, want...) {
			return true
		}
	}
	return false
}

func mkEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func xdgSD(xdg string) string {
	return filepath.Join(xdg, StateDirNS)
}

func cliEnv(xdg string) map[string]string {
	return map[string]string{
		"XDG_STATE_HOME":            xdg,
		"HERDR_BIN":                 "herdr",
		"HERDR_WORKSPACE_ID":        "w8",
		"HERDR_PLUGIN_CONTEXT_JSON": `{"invocation_source":"cli"}`,
		"HERDR_PLUGIN_CLICKED_URL":  "",
	}
}

func TestParseClickedPort(t *testing.T) {
	cases := []struct {
		in      string
		want    uint16
		wantErr bool
	}{
		{"http://127.0.0.1:3000", 3000, false},
		{"http://localhost:3000/some/path", 3000, false},
		{"http://127.0.0.2:59999", 59999, false},
		{"http://127.0.0.1:3000/", 3000, false},
		{"http://127.0.0.1:3000/some/path?foo=bar", 3000, false},
		{"http://example.com:3000", 0, true},
		{"http://127.0.0.1/noport", 0, true},
		{"not-a-url", 0, true},
		{"http://192.168.1.1:3000", 0, true},
	}
	for _, tc := range cases {
		got, err := ParseClickedPort(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseClickedPort(%q): want err, got port %d", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseClickedPort(%q): unexpected err: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseClickedPort(%q): got %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestPortsToggle_CLIPath(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	sd := xdgSD(tmpDir)

	var calls [][]string
	run := portfwd.Runner(func(_ context.Context, argv []string) (string, string, int, error) {
		calls = append(calls, argv)
		return `{"type":"plugin_pane_opened"}`, "", 0, nil
	})
	env := mkEnv(cliEnv(tmpDir))

	var out, errOut strings.Builder
	code := runPortsToggleWith(ctx, []string{"--port", "3000"}, &out, &errOut, run, env)
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr: %s", code, errOut.String())
	}
	if !anyCall(calls, "--pane-id", "ports") {
		t.Errorf("no pane-open call with --pane-id ports; calls: %v", calls)
	}
	state, ok, err := LoadForwardsState(sd)
	if err != nil {
		t.Fatalf("LoadForwardsState: %v", err)
	}
	if !ok {
		t.Fatalf("forwards.state not written")
	}
	var found bool
	for _, f := range state.Forwards {
		if f.Port == 3000 && f.Status == PFStatusPending {
			found = true
		}
	}
	if !found {
		t.Errorf("port 3000 not pending; got: %v", state.Forwards)
	}

	run2 := portfwd.Runner(func(_ context.Context, _ []string) (string, string, int, error) {
		return `{"type":"plugin_pane_opened"}`, "", 0, nil
	})
	var out2, errOut2 strings.Builder
	code2 := runPortsToggleWith(ctx, []string{}, &out2, &errOut2, run2, env)
	if code2 == 0 {
		t.Errorf("no --port on cli path: want non-zero exit, got 0")
	}
}

func TestPortsToggle_LinkClickPath(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	sd := xdgSD(tmpDir)
	var calls [][]string
	run := portfwd.Runner(func(_ context.Context, argv []string) (string, string, int, error) {
		calls = append(calls, argv)
		return `{"type":"plugin_pane_opened"}`, "", 0, nil
	})
	env := mkEnv(map[string]string{
		"XDG_STATE_HOME":            tmpDir,
		"HERDR_BIN":                 "herdr",
		"HERDR_WORKSPACE_ID":        "w8",
		"HERDR_PLUGIN_CONTEXT_JSON": `{"invocation_source":"link_click"}`,
		"HERDR_PLUGIN_CLICKED_URL":  "http://127.0.0.1:4000",
	})

	var out, errOut strings.Builder
	code := runPortsToggleWith(ctx, []string{}, &out, &errOut, run, env)
	if code != 0 {
		t.Fatalf("link_click valid URL: want exit 0, got %d; stderr: %s", code, errOut.String())
	}
	if !anyCall(calls, "--pane-id", "ports") {
		t.Errorf("link_click: no pane-open call; calls: %v", calls)
	}
	state, ok, err := LoadForwardsState(sd)
	if err != nil {
		t.Fatalf("LoadForwardsState: %v", err)
	}
	if !ok {
		t.Fatalf("forwards.state not written")
	}
	var found bool
	for _, f := range state.Forwards {
		if f.Port == 4000 && f.Status == PFStatusPending {
			found = true
		}
	}
	if !found {
		t.Errorf("port 4000 not pending; got: %v", state.Forwards)
	}

	tmpDir2 := t.TempDir()
	var calls2 [][]string
	run2 := portfwd.Runner(func(_ context.Context, argv []string) (string, string, int, error) {
		calls2 = append(calls2, argv)
		return `{"type":"plugin_pane_opened"}`, "", 0, nil
	})
	env2 := mkEnv(map[string]string{
		"XDG_STATE_HOME":            tmpDir2,
		"HERDR_BIN":                 "herdr",
		"HERDR_WORKSPACE_ID":        "w8",
		"HERDR_PLUGIN_CONTEXT_JSON": `{"invocation_source":"link_click"}`,
		"HERDR_PLUGIN_CLICKED_URL":  "http://example.com:4000",
	})
	var out2, errOut2 strings.Builder
	code2 := runPortsToggleWith(ctx, []string{}, &out2, &errOut2, run2, env2)
	if code2 == 0 {
		t.Errorf("non-loopback URL: want non-zero exit, got 0")
	}
	if len(calls2) > 0 {
		t.Errorf("non-loopback URL: pane should not be opened; calls: %v", calls2)
	}
}

func TestPortsToggle_VisibleConfirmation(t *testing.T) {
	tmpDir := t.TempDir()
	sd := filepath.Join(tmpDir, StateDirNS)

	_, ok, err := LoadForwardsState(sd)
	if err != nil || ok {
		t.Fatalf("forwards.state should not exist before upsert; ok=%v err=%v", ok, err)
	}

	if err := os.MkdirAll(sd, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := upsertPortInState(sd, 3000, PFStatusPending); err != nil {
		t.Fatalf("upsertPortInState: %v", err)
	}

	state, ok, err := LoadForwardsState(sd)
	if err != nil {
		t.Fatalf("LoadForwardsState after upsert: %v", err)
	}
	if !ok {
		t.Fatalf("forwards.state not written after upsert")
	}
	var found bool
	for _, f := range state.Forwards {
		if f.Port == 3000 && f.Status == PFStatusPending {
			found = true
		}
	}
	if !found {
		t.Errorf("port 3000 not pending after upsert; got: %v", state.Forwards)
	}

	argv := portsPaneOpenArgv("herdr", "w8")
	if !argsIn(argv, "--placement", "tab") {
		t.Errorf("portsPaneOpenArgv: want --placement tab; got %v", argv)
	}
}

func TestPortsToggle_OutOfRange(t *testing.T) {
	ctx := context.Background()

	t.Run("out_of_range_not_enqueued", func(t *testing.T) {
		tmpDir := t.TempDir()
		sd := xdgSD(tmpDir)
		var calls [][]string
		run := portfwd.Runner(func(_ context.Context, argv []string) (string, string, int, error) {
			calls = append(calls, argv)
			return `{"type":"plugin_pane_opened"}`, "", 0, nil
		})
		env := mkEnv(cliEnv(tmpDir))

		var out, errOut strings.Builder
		code := runPortsToggleWith(ctx, []string{"--port", "12000"}, &out, &errOut, run, env)
		if code != 0 {
			t.Fatalf("out-of-range port: want exit 0, got %d; stderr: %s", code, errOut.String())
		}
		if !anyCall(calls, "--pane-id", "ports") {
			t.Errorf("out-of-range: pane not opened; calls: %v", calls)
		}
		q, err := LoadQueue(sd)
		if err != nil {
			t.Fatalf("LoadQueue: %v", err)
		}
		for _, r := range q.Pending {
			if r.LocalPort == 12000 {
				t.Errorf("port 12000 should not be in queue; pending: %v", q.Pending)
			}
		}
		state, ok, err := LoadForwardsState(sd)
		if err != nil {
			t.Fatalf("LoadForwardsState: %v", err)
		}
		if !ok {
			t.Fatalf("forwards.state not written for out-of-range")
		}
		var found bool
		for _, f := range state.Forwards {
			if f.Port == 12000 && f.Status == PFStatusOutRange {
				found = true
			}
		}
		if !found {
			t.Errorf("port 12000 not out_of_range; got: %v", state.Forwards)
		}
	})

	t.Run("in_range_enqueued", func(t *testing.T) {
		tmpDir := t.TempDir()
		sd := xdgSD(tmpDir)
		run := portfwd.Runner(func(_ context.Context, _ []string) (string, string, int, error) {
			return `{"type":"plugin_pane_opened"}`, "", 0, nil
		})
		env := mkEnv(cliEnv(tmpDir))

		var out, errOut strings.Builder
		code := runPortsToggleWith(ctx, []string{"--port", "3000"}, &out, &errOut, run, env)
		if code != 0 {
			t.Fatalf("in-range port: want exit 0, got %d; stderr: %s", code, errOut.String())
		}
		q, err := LoadQueue(sd)
		if err != nil {
			t.Fatalf("LoadQueue: %v", err)
		}
		var found bool
		for _, r := range q.Pending {
			if r.LocalPort == 3000 {
				found = true
			}
		}
		if !found {
			t.Errorf("port 3000 should be in queue; pending: %v", q.Pending)
		}
	})
}

func TestPortsPaneOpenArgv_IncludesPlacementAndWorkspace(t *testing.T) {
	argv := portsPaneOpenArgv("herdr", "w8")
	if !argsIn(argv, "--placement", "tab") {
		t.Errorf("want --placement tab; got %v", argv)
	}
	if !argsIn(argv, "--workspace", "w8") {
		t.Errorf("want --workspace w8; got %v", argv)
	}
	if !argsIn(argv, "--pane-id", "ports") {
		t.Errorf("want --pane-id ports; got %v", argv)
	}
	if argIn(argv, "--entrypoint") {
		t.Errorf("--entrypoint should not appear; got %v", argv)
	}

	argv2 := portsPaneOpenArgv("herdr", "")
	if argIn(argv2, "--workspace") {
		t.Errorf("empty workspaceID: --workspace should not appear; got %v", argv2)
	}
}

func TestManifestPatternCoversLocalhost(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "herdr-plugin.toml"))
	if err != nil {
		t.Fatalf("read herdr-plugin.toml: %v", err)
	}
	var patterns []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "pattern") {
			s := strings.SplitN(line, "'", 3)
			if len(s) == 3 {
				patterns = append(patterns, s[1])
			}
		}
	}
	check := func(probe string) {
		t.Helper()
		for _, p := range patterns {
			ok, err := regexp.MatchString(p, probe)
			if err == nil && ok {
				return
			}
		}
		t.Errorf("no pattern in herdr-plugin.toml matches %q; patterns: %v", probe, patterns)
	}
	check("http://localhost:3000")
	check("http://127.0.0.1:3000")
}

func TestPortsToggle_PaneOpenTypeCheck(t *testing.T) {
	ctx := context.Background()

	t.Run("wrong_type_rejected", func(t *testing.T) {
		tmpDir := t.TempDir()
		run := portfwd.Runner(func(_ context.Context, _ []string) (string, string, int, error) {
			return `{"type":"wrong_type"}`, "", 0, nil
		})
		env := mkEnv(cliEnv(tmpDir))
		var out, errOut strings.Builder
		code := runPortsToggleWith(ctx, []string{"--port", "3000"}, &out, &errOut, run, env)
		if code == 0 {
			t.Errorf("wrong_type response: want non-zero exit, got 0")
		}
	})

	t.Run("correct_type_accepted", func(t *testing.T) {
		tmpDir := t.TempDir()
		run := portfwd.Runner(func(_ context.Context, _ []string) (string, string, int, error) {
			return `{"type":"plugin_pane_opened"}`, "", 0, nil
		})
		env := mkEnv(cliEnv(tmpDir))
		var out, errOut strings.Builder
		code := runPortsToggleWith(ctx, []string{"--port", "3000"}, &out, &errOut, run, env)
		if code != 0 {
			t.Errorf("correct_type response: want exit 0, got %d; stderr: %s", code, errOut.String())
		}
	})
}
