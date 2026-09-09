package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IniZio/herdr-plugin-msb/internal/core/herdrspace"
	"github.com/IniZio/herdr-plugin-msb/internal/core/service"
)

func fakeHerdrConvertBin(t *testing.T, wsJSON string) (bin, logPath string) {
	t.Helper()
	d := t.TempDir()
	bin = filepath.Join(d, "herdr-fake")
	logPath = filepath.Join(d, "argv.log")
	paneJSON := `{"id":"cli:pane:list","result":{"panes":[{"pane_id":"w9:p1","tab_id":"w9:t1","workspace_id":"w9","focused":true}]}}`
	script := "#!/bin/sh\n" +
		"echo \"$*\" >> " + logPath + "\n" +
		"case \"$1 $2\" in\n" +
		"  'workspace get') cat <<'EOF'\n" + wsJSON + "\nEOF\n" + "    ;;\n" +
		"  'pane list') cat <<'EOF'\n" + paneJSON + "\nEOF\n" + "    ;;\n" +
		"  *) echo ok ;;\n" +
		"esac\n"
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return bin, logPath
}

func readArgvLog(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("argv log: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func stubConvertSandbox(t *testing.T, created *[]string, createErr error) {
	t.Helper()
	origCreate := convertCreateSandbox
	origRemove := convertRemoveSandbox
	convertCreateSandbox = func(ctx context.Context, project string, opts service.CreateOptions) error {
		*created = append(*created, project+"/"+opts.Name+" worktree="+opts.Worktree)
		return createErr
	}
	convertRemoveSandbox = func(ctx context.Context, project, name string) error { return nil }
	t.Cleanup(func() {
		convertCreateSandbox = origCreate
		convertRemoveSandbox = origRemove
	})
}

const convertWorkspaceJSON = `{"id":"cli:workspace:get","result":{"type":"workspace_info","workspace":{"workspace_id":"w9","label":"lms","active_tab_id":"w9:t1","worktree":{"checkout_path":"/home/u/wt/lms","repo_root":"/home/u/lms","is_linked_worktree":true}}}}`

const primaryCheckoutWorkspaceJSON = `{"id":"cli:workspace:get","result":{"type":"workspace_info","workspace":{"workspace_id":"w9","label":"lms","active_tab_id":"w9:t1","worktree":{"checkout_path":"/home/u/lms","repo_root":"/home/u/lms","is_linked_worktree":false}}}}`

const noLinkedFlagWorkspaceJSON = `{"id":"cli:workspace:get","result":{"type":"workspace_info","workspace":{"workspace_id":"w9","label":"lms","active_tab_id":"w9:t1","worktree":{"checkout_path":"/home/u/lms","repo_root":"/home/u/lms"}}}}`

func assertConvertRefusesPrimaryCheckout(t *testing.T, wsJSON string) {
	t.Helper()
	stateParent := t.TempDir()
	bin, logPath := fakeHerdrConvertBin(t, wsJSON)
	t.Setenv("HERDR_BIN_PATH", bin)
	t.Setenv("XDG_STATE_HOME", stateParent)

	var created []string
	stubConvertSandbox(t, &created, nil)

	var stdout, stderr bytes.Buffer
	code := runSpaceConvert(context.Background(),
		[]string{"--workspace", "w9", "--image", "img:latest"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("primary checkout must never convert; stdout=%q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "primary checkout") || !strings.Contains(stderr.String(), "worktree") {
		t.Fatalf("refusal must say why and what to do instead; stderr=%q", stderr.String())
	}
	if len(created) != 0 {
		t.Fatalf("no sandbox may be created for the primary checkout; got %v", created)
	}
	dir, err := StateDir(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := herdrspace.GetByWorkspaceID(context.Background(), dir, "w9"); err == nil {
		t.Fatal("refusal must not write a binding")
	}
	for _, line := range readArgvLog(t, logPath) {
		if !strings.HasPrefix(line, "workspace get") {
			t.Fatalf("refusal must fire before any other herdr call; got %q", line)
		}
	}
}

func TestSpaceConvert_RefusesPrimaryCheckout(t *testing.T) {
	assertConvertRefusesPrimaryCheckout(t, primaryCheckoutWorkspaceJSON)
}

func TestSpaceConvert_RefusesWhenLinkedWorktreeFlagAbsent(t *testing.T) {
	assertConvertRefusesPrimaryCheckout(t, noLinkedFlagWorkspaceJSON)
}

const liveW8WorkspaceJSON = `{"id":"cli:workspace:get","result":{"type":"workspace_info","workspace":{"active_tab_id":"w9:t1","agent_status":"working","focused":true,"label":"herdr-plugin-msb","number":2,"pane_count":1,"tab_count":1,"workspace_id":"w9","worktree":{"checkout_path":"/home/newman/magic/herdr-plugin-msb","is_linked_worktree":false,"repo_key":"/home/newman/magic/herdr-plugin-msb/.git","repo_name":"herdr-plugin-msb","repo_root":"/home/newman/magic/herdr-plugin-msb"}}}}`

func TestSpaceConvert_RefusesLiveOperatorPrimaryCheckoutPayload(t *testing.T) {
	assertConvertRefusesPrimaryCheckout(t, liveW8WorkspaceJSON)
}

func TestSpaceConvert_SplitsBeforeClosingRootAndNeverCreatesWorkspace(t *testing.T) {
	stateParent := t.TempDir()
	bin, logPath := fakeHerdrConvertBin(t, convertWorkspaceJSON)
	t.Setenv("HERDR_BIN_PATH", bin)
	t.Setenv("XDG_STATE_HOME", stateParent)

	var created []string
	stubConvertSandbox(t, &created, nil)

	var stdout, stderr bytes.Buffer
	code := runSpaceConvert(context.Background(),
		[]string{"--workspace", "w9", "--image", "img:latest", "--project", "demo"},
		&stdout, &stderr)
	if code != 0 {
		t.Fatalf("space-convert: want 0, got %d; stderr=%q", code, stderr.String())
	}

	argv := readArgvLog(t, logPath)
	openIdx, closeIdx := -1, -1
	for i, line := range argv {
		if strings.HasPrefix(line, "workspace create") {
			t.Fatalf("space-convert must never call `herdr workspace create`; argv=%v", argv)
		}
		if strings.HasPrefix(line, "plugin pane open") && openIdx < 0 {
			openIdx = i
		}
		if strings.HasPrefix(line, "pane close") && closeIdx < 0 {
			closeIdx = i
		}
	}
	if openIdx < 0 {
		t.Fatalf("no guest-pane open recorded; argv=%v", argv)
	}
	if closeIdx < 0 {
		t.Fatalf("old root pane was never closed; argv=%v", argv)
	}
	if closeIdx < openIdx {
		t.Fatalf("pane close must come AFTER the guest pane split (closing the last pane destroys the workspace); open=%d close=%d argv=%v", openIdx, closeIdx, argv)
	}
	if !strings.Contains(argv[openIdx], "--placement split") || !strings.Contains(argv[openIdx], "--target-pane w9:p1") {
		t.Fatalf("guest pane must be a split off the existing root pane; got %q", argv[openIdx])
	}
	if !strings.Contains(argv[closeIdx], "w9:p1") {
		t.Fatalf("pane close must target the old root pane; got %q", argv[closeIdx])
	}
	if len(created) != 1 || !strings.Contains(created[0], "worktree=/home/u/wt/lms") {
		t.Fatalf("sandbox must be created with the workspace checkout_path; got %v", created)
	}

	b, err := os.ReadFile(filepath.Join(stateParent, StateDirNS, "herdr-space-bindings.json"))
	if err != nil {
		t.Fatalf("binding store: %v", err)
	}
	if !strings.Contains(string(b), `"w9"`) {
		t.Fatalf("binding must record the EXISTING workspace id w9; got %s", string(b))
	}
}

func TestSpaceConvert_GuestPaneOpensInTheWorktreeCheckout(t *testing.T) {
	stateParent := t.TempDir()
	bin, logPath := fakeHerdrConvertBin(t, convertWorkspaceJSON)
	t.Setenv("HERDR_BIN_PATH", bin)
	t.Setenv("XDG_STATE_HOME", stateParent)

	var created []string
	stubConvertSandbox(t, &created, nil)

	var stdout, stderr bytes.Buffer
	code := runSpaceConvert(context.Background(),
		[]string{"--workspace", "w9", "--image", "img:latest", "--project", "demo"},
		&stdout, &stderr)
	if code != 0 {
		t.Fatalf("space-convert: want 0, got %d; stderr=%q", code, stderr.String())
	}

	open := ""
	for _, line := range readArgvLog(t, logPath) {
		if strings.HasPrefix(line, "plugin pane open") {
			open = line
			break
		}
	}
	if open == "" {
		t.Fatalf("no guest-pane open recorded")
	}
	if !strings.Contains(open, "--cwd /home/u/wt/lms") {
		t.Fatalf("guest pane must open with the worktree checkout as its host cwd, or herdr\n"+
			"resolves the sidebar branch/git_status rows from the plugin root instead of the\n"+
			"worktree (the sidebar then reads the plugin repo's branch, e.g. \"main\").\n"+
			"want --cwd /home/u/wt/lms in: %s", open)
	}
}

func TestSpaceConvert_NoWorktreeFailsAndCreatesNothing(t *testing.T) {
	stateParent := t.TempDir()
	noWT := `{"id":"cli:workspace:get","result":{"type":"workspace_info","workspace":{"workspace_id":"w9","label":"scratch","active_tab_id":"w9:t1"}}}`
	bin, logPath := fakeHerdrConvertBin(t, noWT)
	t.Setenv("HERDR_BIN_PATH", bin)
	t.Setenv("XDG_STATE_HOME", stateParent)

	var created []string
	stubConvertSandbox(t, &created, nil)

	var stdout, stderr bytes.Buffer
	code := runSpaceConvert(context.Background(),
		[]string{"--workspace", "w9", "--image", "img:latest"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("workspace without a worktree must fail; stdout=%q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "w9") {
		t.Fatalf("error must name the workspace id; stderr=%q", stderr.String())
	}
	if len(created) != 0 {
		t.Fatalf("no sandbox may be created when there is no worktree; got %v", created)
	}
	for _, line := range readArgvLog(t, logPath) {
		if strings.HasPrefix(line, "pane close") || strings.HasPrefix(line, "plugin pane open") || strings.HasPrefix(line, "workspace create") {
			t.Fatalf("no mutating herdr call allowed on the no-worktree path; got %q", line)
		}
	}
}

func TestSandboxNameFromLabel(t *testing.T) {
	for _, tc := range []struct{ label, ws, want string }{
		{"msb:demo", "w9", "demo"},
		{"my space", "w9", "my-space"},
		{"", "w9", "ws-w9"},
	} {
		if got := sandboxNameFromLabel(tc.label, tc.ws); got != tc.want {
			t.Fatalf("sandboxNameFromLabel(%q): got %q want %q", tc.label, got, tc.want)
		}
	}
}

func TestSpaceConvert_EnvWorkspaceID(t *testing.T) {
	stateParent := t.TempDir()
	bin, _ := fakeHerdrConvertBin(t, convertWorkspaceJSON)
	t.Setenv("HERDR_BIN_PATH", bin)
	t.Setenv("XDG_STATE_HOME", stateParent)
	t.Setenv("HERDR_WORKSPACE_ID", "w9")

	var created []string
	stubConvertSandbox(t, &created, nil)

	var stdout, stderr bytes.Buffer
	code := runSpaceConvert(context.Background(), nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("space-convert with HERDR_WORKSPACE_ID: want 0, got %d; stderr=%q", code, stderr.String())
	}
	if len(created) != 1 {
		t.Fatalf("sandbox must be created; got %v", created)
	}
}

func TestSpaceConvert_NoWorkspaceNoEnv(t *testing.T) {
	t.Setenv("HERDR_WORKSPACE_ID", "")

	var stdout, stderr bytes.Buffer
	code := runSpaceConvert(context.Background(), nil, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("no workspace: want exit 2, got %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "workspace ID required") {
		t.Fatalf("error must contain 'workspace ID required'; stderr=%q", stderr.String())
	}
}

func TestSpaceConvert_DefaultImage(t *testing.T) {
	stateParent := t.TempDir()
	bin, _ := fakeHerdrConvertBin(t, convertWorkspaceJSON)
	t.Setenv("HERDR_BIN_PATH", bin)
	t.Setenv("XDG_STATE_HOME", stateParent)

	var capturedImage string
	orig := convertCreateSandbox
	convertCreateSandbox = func(ctx context.Context, project string, opts service.CreateOptions) error {
		capturedImage = opts.ImageRef
		return nil
	}
	t.Cleanup(func() { convertCreateSandbox = orig })

	var stdout, stderr bytes.Buffer
	code := runSpaceConvert(context.Background(), []string{"--workspace", "w9"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("space-convert no --image: want 0, got %d; stderr=%q", code, stderr.String())
	}
	if capturedImage != "alpine" {
		t.Fatalf("default image must be alpine; got %q", capturedImage)
	}
}

func TestSpaceConvert_AlreadyBound(t *testing.T) {
	stateParent := t.TempDir()
	bin, _ := fakeHerdrConvertBin(t, convertWorkspaceJSON)
	t.Setenv("HERDR_BIN_PATH", bin)
	t.Setenv("XDG_STATE_HOME", stateParent)

	stateDir := filepath.Join(stateParent, StateDirNS)
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := herdrspace.Put(context.Background(), stateDir, herdrspace.Binding{
		HerdrWorkspaceID: "w9",
		SandboxHandle:    "demo/lms-w9",
		SpaceLabel:       "lms",
	}); err != nil {
		t.Fatal(err)
	}

	calls := 0
	orig := convertCreateSandbox
	convertCreateSandbox = func(ctx context.Context, project string, opts service.CreateOptions) error {
		calls++
		return nil
	}
	t.Cleanup(func() { convertCreateSandbox = orig })

	var stdout, stderr bytes.Buffer
	code := runSpaceConvert(context.Background(),
		[]string{"--workspace", "w9", "--image", "img:latest", "--project", "demo"},
		&stdout, &stderr)
	if code != 1 {
		t.Fatalf("already-bound workspace must exit 1; got %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "demo/lms-w9") {
		t.Fatalf("error must name existing sandbox handle; stderr=%q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "space-open-pane") {
		t.Fatalf("error must mention space-open-pane; stderr=%q", stderr.String())
	}
	if calls != 0 {
		t.Fatalf("convertCreateSandbox must NOT be called when already bound; call count=%d", calls)
	}
}

func TestSpaceConvert_UnboundDoesNotRefuse(t *testing.T) {
	stateParent := t.TempDir()
	bin, _ := fakeHerdrConvertBin(t, convertWorkspaceJSON)
	t.Setenv("HERDR_BIN_PATH", bin)
	t.Setenv("XDG_STATE_HOME", stateParent)

	var created []string
	stubConvertSandbox(t, &created, nil)

	var stdout, stderr bytes.Buffer
	code := runSpaceConvert(context.Background(),
		[]string{"--workspace", "w9", "--image", "img:latest"},
		&stdout, &stderr)
	if code != 0 {
		t.Fatalf("unbound workspace must not be refused; got %d; stderr=%q", code, stderr.String())
	}
}
