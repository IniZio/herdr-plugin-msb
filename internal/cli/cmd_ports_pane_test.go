package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderPortsPane_NoAgentState(t *testing.T) {
	out := RenderPortsPane(nil, 0, time.Now())
	if !strings.Contains(out, "laptop agent not connected") {
		t.Errorf("missing 'laptop agent not connected': %q", out)
	}
	if !strings.Contains(out, "forwards.state not found") {
		t.Errorf("missing 'forwards.state not found': %q", out)
	}
	if !strings.Contains(out, "q  close pane") {
		t.Errorf("missing 'q  close pane': %q", out)
	}
	if strings.Contains(out, "LIVE") {
		t.Errorf("unexpected 'LIVE' in no-agent screen: %q", out)
	}
}

func TestRenderPortsPane_NoLaunchd(t *testing.T) {
	out := RenderPortsPane(nil, 0, time.Now())
	if strings.Contains(out, "launchd") {
		t.Errorf("nil-state text must not mention launchd: %q", out)
	}
	if strings.Contains(out, "Install local agent") {
		t.Errorf("nil-state text must not mention Install local agent: %q", out)
	}
	if !strings.Contains(out, "herdr --remote") {
		t.Errorf("nil-state text must mention herdr --remote: %q", out)
	}
}

func TestRenderPortsPane_LiveForward(t *testing.T) {
	state := &ForwardsState{
		UpdatedAt: time.Now(),
		Forwards: []PortForward{{
			Port:        3000,
			Status:      PFStatusLive,
			ConfirmedAt: time.Date(2026, 9, 9, 10, 0, 30, 0, time.UTC),
		}},
	}
	out := RenderPortsPane(state, 0, time.Now())
	if !strings.Contains(out, "LIVE") {
		t.Errorf("missing 'LIVE': %q", out)
	}
	if !strings.Contains(out, "3000") {
		t.Errorf("missing '3000': %q", out)
	}
	if !strings.Contains(out, "ctrl+click") {
		t.Errorf("missing 'ctrl+click': %q", out)
	}
}

func TestRenderPortsPane_OutOfRange(t *testing.T) {
	state := &ForwardsState{
		UpdatedAt: time.Now(),
		Forwards:  []PortForward{{Port: 12000, Status: PFStatusOutRange}},
	}
	out := RenderPortsPane(state, 0, time.Now())
	if !strings.Contains(out, "OUT-OF-RANGE") {
		t.Errorf("missing 'OUT-OF-RANGE': %q", out)
	}
	if strings.Contains(out, "LIVE") {
		t.Errorf("unexpected 'LIVE': %q", out)
	}
}

func TestRenderPortsPane_EmptyState(t *testing.T) {
	state := &ForwardsState{UpdatedAt: time.Now(), Forwards: nil}
	out := RenderPortsPane(state, 0, time.Now())
	if !strings.Contains(out, "agent connected") {
		t.Errorf("missing 'agent connected': %q", out)
	}
	if !strings.Contains(out, "no ports") {
		t.Errorf("missing 'no ports': %q", out)
	}
	if strings.Contains(out, "LIVE") {
		t.Errorf("unexpected 'LIVE': %q", out)
	}
}

func TestDispatchPaneKey_ToggleOnIdleRow(t *testing.T) {
	rows := []PortForward{{Port: 3000, Status: PFStatusIdle}}
	result := DispatchPaneKey(KeyEnter, rows, 0)
	if result.EnqueuePort != 3000 {
		t.Errorf("want EnqueuePort=3000, got %d", result.EnqueuePort)
	}
}

func TestDispatchPaneKey_UnknownKeyNoOp(t *testing.T) {
	rows := []PortForward{{Port: 3000, Status: PFStatusIdle}}
	result := DispatchPaneKey(PaneKey('x'), rows, 0)
	if result.EnqueuePort != 0 {
		t.Errorf("want EnqueuePort=0, got %d", result.EnqueuePort)
	}
	if result.Close {
		t.Error("want Close=false")
	}
	if result.MoveDelta != 0 {
		t.Errorf("want MoveDelta=0, got %d", result.MoveDelta)
	}
	if result.ShowRecreate {
		t.Error("want ShowRecreate=false")
	}
}

func TestDispatchPaneKey_EnterOnPendingIsNoOp(t *testing.T) {
	rows := []PortForward{{Port: 3000, Status: PFStatusPending}}
	result := DispatchPaneKey(KeyEnter, rows, 0)
	if result.EnqueuePort != 0 {
		t.Errorf("want EnqueuePort=0, got %d", result.EnqueuePort)
	}
}

func TestConfirmationPresentAfterEnqueue(t *testing.T) {
	idleState := &ForwardsState{
		UpdatedAt: time.Now(),
		Forwards:  []PortForward{{Port: 3000, Status: PFStatusIdle}},
	}
	out1 := RenderPortsPane(idleState, 0, time.Now())
	if strings.Contains(out1, "PENDING") {
		t.Errorf("PENDING should be absent before action: %q", out1)
	}

	pendingState := &ForwardsState{
		UpdatedAt: time.Now(),
		Forwards:  []PortForward{{Port: 3000, Status: PFStatusPending}},
	}
	out2 := RenderPortsPane(pendingState, 0, time.Now())
	if !strings.Contains(out2, "PENDING") {
		t.Errorf("PENDING should be present after action: %q", out2)
	}
}

func repoRootForTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "herdr-plugin.toml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find herdr-plugin.toml")
		}
		dir = parent
	}
}

func TestManifestPortsPaneEntry(t *testing.T) {
	root := repoRootForTest(t)
	b, err := os.ReadFile(filepath.Join(root, "herdr-plugin.toml"))
	if err != nil {
		t.Fatalf("read herdr-plugin.toml: %v", err)
	}
	content := string(b)
	if !strings.Contains(content, `id = "ports"`) {
		t.Errorf("missing id = \"ports\": %q", content)
	}
	if !strings.Contains(content, `"herdr-plugin-msb", "ports-pane"`) {
		t.Errorf("missing ports-pane entrypoint: %q", content)
	}
	if !strings.Contains(content, `placement = "tab"`) {
		t.Errorf("missing placement = \"tab\": %q", content)
	}
}

func TestNotifierNotify_ChecksTypeField(t *testing.T) {
	mockRun := Runner(func(ctx context.Context, argv []string) (string, string, int, error) {
		return `{"type":"plugin_pane_opened"}`, "", 0, nil
	})
	n := &Notifier{Run: mockRun}
	err := n.Notify(context.Background(), "t", []string{"b"})
	if err != nil {
		t.Errorf("want nil error, got %v", err)
	}
}

func TestNotifierNotify_RejectsWrongType(t *testing.T) {
	mockRun := Runner(func(ctx context.Context, argv []string) (string, string, int, error) {
		return `{"type":"something_else"}`, "", 0, nil
	})
	n := &Notifier{Run: mockRun}
	err := n.Notify(context.Background(), "t", []string{"b"})
	if err == nil {
		t.Error("want non-nil error for wrong type")
	} else if !strings.Contains(err.Error(), "unexpected type") {
		t.Errorf("want 'unexpected type' in error, got %v", err)
	}
}

func TestOutOfRangeDistinct(t *testing.T) {
	state := &ForwardsState{
		UpdatedAt: time.Now(),
		Forwards:  []PortForward{{Port: 12000, Status: PFStatusOutRange}},
	}
	out := RenderPortsPane(state, 0, time.Now())
	if !strings.Contains(out, "OUT-OF-RANGE") {
		t.Errorf("missing OUT-OF-RANGE: %q", out)
	}
	if strings.Contains(out, "LIVE") {
		t.Errorf("unexpected LIVE: %q", out)
	}
	if strings.Contains(out, "IDLE") {
		t.Errorf("unexpected IDLE: %q", out)
	}
}

func TestRecreatGateBlocksByDefault(t *testing.T) {
	rows := []PortForward{{Port: 3000, Status: PFStatusIdle}}
	result := DispatchPaneKey(KeyR, rows, 0)
	if !result.ShowRecreate {
		t.Error("want ShowRecreate=true")
	}
	result2 := DispatchPaneKey(KeyQ, rows, 0)
	if !result2.Close {
		t.Error("want Close=true")
	}
	if result2.EnqueuePort != 0 {
		t.Errorf("want EnqueuePort=0, got %d", result2.EnqueuePort)
	}
}

func TestRecreateCancelOnQ(t *testing.T) {
	result := DispatchPaneKey(KeyQ, nil, 0)
	if !result.Close {
		t.Error("want Close=true")
	}
}
