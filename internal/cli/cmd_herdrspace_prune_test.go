package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IniZio/herdr-plugin-msb/internal/core/herdrspace"
	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
	"github.com/IniZio/herdr-plugin-msb/internal/core/service"
)

func stubPruneVars(t *testing.T, liveMap map[string]string, listErr error, removeCalls *[]string, removeErr error) {
	t.Helper()
	origList := pruneListWorkspaces
	origRemove := pruneRemoveSandbox
	origStop := pruneStopSandbox
	origStatus := pruneSandboxStatus
	pruneListWorkspaces = func(_ context.Context, _ string) (map[string]string, error) {
		return liveMap, listErr
	}
	pruneRemoveSandbox = func(_ context.Context, project, name string) error {
		*removeCalls = append(*removeCalls, project+"/"+name)
		return removeErr
	}
	pruneStopSandbox = func(_ context.Context, project, name string) error {
		return fmt.Errorf("stop not stubbed for %s/%s", project, name)
	}
	pruneSandboxStatus = func(_ context.Context, _, _ string) (coreruntime.SandboxStatus, error) {
		return coreruntime.SandboxStatusStopped, nil
	}
	t.Cleanup(func() {
		pruneListWorkspaces = origList
		pruneRemoveSandbox = origRemove
		pruneStopSandbox = origStop
		pruneSandboxStatus = origStatus
	})
}

func stubPruneStatus(t *testing.T, status coreruntime.SandboxStatus, err error, calls *[]string) {
	t.Helper()
	orig := pruneSandboxStatus
	pruneSandboxStatus = func(_ context.Context, project, name string) (coreruntime.SandboxStatus, error) {
		if calls != nil {
			*calls = append(*calls, project+"/"+name)
		}
		return status, err
	}
	t.Cleanup(func() { pruneSandboxStatus = orig })
}

func goneCheckoutPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "worktree-removed")
}

func TestSpacePrune_RemoveFirstThenStopAndRetryRemove(t *testing.T) {
	dir := pruneStateDir(t)
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lblR", HerdrWorkspaceID: "wxR", SandboxHandle: "herdr/running", CheckoutPath: goneCheckoutPath(t)})

	var removeCalls, stopCalls []string
	stubPruneVars(t, map[string]string{}, nil, &removeCalls, nil)

	origRemove := pruneRemoveSandbox
	origStop := pruneStopSandbox
	pruneRemoveSandbox = func(_ context.Context, project, name string) error {
		removeCalls = append(removeCalls, project+"/"+name)
		if len(stopCalls) == 0 {
			return fmt.Errorf("cannot remove sandbox %q: still running", name)
		}
		return nil
	}
	pruneStopSandbox = func(_ context.Context, project, name string) error {
		stopCalls = append(stopCalls, project+"/"+name)
		return nil
	}
	t.Cleanup(func() { pruneRemoveSandbox = origRemove; pruneStopSandbox = origStop })

	var stdout, stderr bytes.Buffer
	code := runSpacePrune(context.Background(), []string{"--apply", "--workspace", "wxR"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("stop-then-remove: want 0, got %d; stderr=%q", code, stderr.String())
	}
	if len(stopCalls) != 1 || stopCalls[0] != "herdr/running" {
		t.Errorf("stop-then-remove: want one stop of herdr/running; got %v", stopCalls)
	}
	if len(removeCalls) != 2 {
		t.Errorf("stop-then-remove: want remove retried after stop; got %v", removeCalls)
	}
	if !strings.Contains(stdout.String(), "reclaimed herdr/running") {
		t.Errorf("stop-then-remove: want reclaimed line; got %q", stdout.String())
	}
}

func TestSpacePrune_StopFailureSurfacesOriginalRemoveError(t *testing.T) {
	dir := pruneStateDir(t)
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lblS", HerdrWorkspaceID: "wxS", SandboxHandle: "herdr/stuck", CheckoutPath: goneCheckoutPath(t)})

	var removeCalls []string
	stubPruneVars(t, map[string]string{}, nil, &removeCalls, fmt.Errorf("still running"))

	var stdout, stderr bytes.Buffer
	code := runSpacePrune(context.Background(), []string{"--apply", "--workspace", "wxS"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("stop failure: want exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "still running") || !strings.Contains(stderr.String(), "stop after failed remove") {
		t.Errorf("stop failure: want both errors in stderr; got %q", stderr.String())
	}
	if _, err := herdrspace.GetByLabel(context.Background(), dir, "lblS"); err != nil {
		t.Errorf("stop failure: binding must survive a failed reclaim; got %v", err)
	}
}

func pruneStateDir(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	t.Setenv("XDG_STATE_HOME", parent)
	return filepath.Join(parent, StateDirNS)
}

func pruneAddBinding(t *testing.T, dir string, b herdrspace.Binding) {
	t.Helper()
	if err := herdrspace.Put(context.Background(), dir, b); err != nil {
		t.Fatalf("put binding: %v", err)
	}
}

func TestSpacePrune_DryRun_ReportsWouldReclaimWithoutRemove(t *testing.T) {
	dir := pruneStateDir(t)
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lbl1", HerdrWorkspaceID: "wx1", SandboxHandle: "proj/box1", CheckoutPath: goneCheckoutPath(t)})

	var removeCalls []string
	stubPruneVars(t, map[string]string{}, nil, &removeCalls, nil)

	var stdout, stderr bytes.Buffer
	code := runSpacePrune(context.Background(), nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("dry run: want 0, got %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "would-reclaim proj/box1") {
		t.Errorf("dry run: want 'would-reclaim proj/box1'; got %q", stdout.String())
	}
	if len(removeCalls) != 0 {
		t.Errorf("dry run: remove must not be called; got %v", removeCalls)
	}
	bs, err := herdrspace.List(context.Background(), dir)
	if err != nil || len(bs) != 1 {
		t.Errorf("dry run: binding must survive; err=%v bs=%v", err, bs)
	}
}

func TestSpacePrune_Apply_ReclaimsAndDeletesBinding(t *testing.T) {
	dir := pruneStateDir(t)
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lbl2", HerdrWorkspaceID: "wx2", SandboxHandle: "proj2/box2", CheckoutPath: goneCheckoutPath(t)})

	var removeCalls []string
	stubPruneVars(t, map[string]string{}, nil, &removeCalls, nil)

	var stdout, stderr bytes.Buffer
	code := runSpacePrune(context.Background(), []string{"--apply", "--workspace", "wx2"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("apply: want 0, got %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "reclaimed proj2/box2") {
		t.Errorf("apply: want 'reclaimed proj2/box2'; got %q", stdout.String())
	}
	if len(removeCalls) != 1 || removeCalls[0] != "proj2/box2" {
		t.Errorf("apply: want remove called once with proj2/box2; got %v", removeCalls)
	}
	bs, _ := herdrspace.List(context.Background(), dir)
	if len(bs) != 0 {
		t.Errorf("apply: binding must be deleted; got %v", bs)
	}
}

func TestSpacePrune_NegativeControl_KeepsAliveWorkspaceWithExistingPath(t *testing.T) {
	dir := pruneStateDir(t)
	realDir := t.TempDir()
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lbl3", HerdrWorkspaceID: "wx3", SandboxHandle: "proj3/box3", CheckoutPath: realDir})

	var removeCalls []string
	stubPruneVars(t, map[string]string{"wx3": realDir}, nil, &removeCalls, nil)

	var stdout, stderr bytes.Buffer
	code := runSpacePrune(context.Background(), []string{"--apply", "--workspace", "wx3"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("negative control: want 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "keep proj3/box3") {
		t.Errorf("negative control: want 'keep proj3/box3'; got %q", stdout.String())
	}
	if len(removeCalls) != 0 {
		t.Errorf("negative control: remove must not be called; got %v", removeCalls)
	}
	bs, _ := herdrspace.List(context.Background(), dir)
	if len(bs) != 1 {
		t.Errorf("negative control: binding must survive; got %v", bs)
	}
}

func TestSpacePrune_TwoSignalsRequired(t *testing.T) {
	cases := []struct {
		name         string
		workspaceID  string
		checkoutGone bool
		liveHasWS    bool
		wantReclaim  bool
		wantReason   string
	}{
		{"both-signals", "wsA", true, false, true, "workspace-gone+worktree-gone:"},
		{"workspace-gone-only", "wsB", false, false, false, "workspace-gone-worktree-present:"},
		{"worktree-gone-only", "wsC", true, true, false, "worktree-gone-workspace-alive:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := pruneStateDir(t)
			path := t.TempDir()
			if tc.checkoutGone {
				path = filepath.Join(path, "removed")
			}
			pruneAddBinding(t, dir, herdrspace.Binding{
				SpaceLabel:       "lbl-" + tc.name,
				HerdrWorkspaceID: tc.workspaceID,
				SandboxHandle:    "p/" + tc.name,
				CheckoutPath:     path,
			})
			live := map[string]string{}
			if tc.liveHasWS {
				live[tc.workspaceID] = path
			}
			var removeCalls []string
			stubPruneVars(t, live, nil, &removeCalls, nil)

			var stdout bytes.Buffer
			code := runSpacePrune(context.Background(), []string{"--apply", "--workspace", tc.workspaceID}, &stdout, &bytes.Buffer{})
			if code != 0 {
				t.Fatalf("%s: want 0, got %d", tc.name, code)
			}
			out := stdout.String()
			if !strings.Contains(out, tc.wantReason) {
				t.Errorf("%s: want reason %q; got %q", tc.name, tc.wantReason, out)
			}
			if tc.wantReclaim && len(removeCalls) != 1 {
				t.Errorf("%s: want remove called once; got %v", tc.name, removeCalls)
			}
			if !tc.wantReclaim && len(removeCalls) != 0 {
				t.Errorf("%s: one signal must not reclaim; got %v", tc.name, removeCalls)
			}
		})
	}
}

func TestSpacePrune_MissingCheckoutPathIsNeverReclaimed(t *testing.T) {
	dir := pruneStateDir(t)
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lblOld", HerdrWorkspaceID: "wxOld", SandboxHandle: "p/legacy"})

	var removeCalls []string
	stubPruneVars(t, map[string]string{}, nil, &removeCalls, nil)

	var stdout, stderr bytes.Buffer
	code := runSpacePrune(context.Background(), []string{"--apply", "--all"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("legacy binding: want 0, got %d; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "keep p/legacy") || !strings.Contains(out, "no-checkout-path-recorded") {
		t.Errorf("legacy binding: want keep with no-checkout-path-recorded; got %q", out)
	}
	if len(removeCalls) != 0 {
		t.Errorf("legacy binding: remove must not be called; got %v", removeCalls)
	}
	bs, _ := herdrspace.List(context.Background(), dir)
	if len(bs) != 1 {
		t.Errorf("legacy binding: binding must survive; got %v", bs)
	}
}

func TestSpacePrune_RunningSandboxRefusedUnlessKillRunning(t *testing.T) {
	setup := func(t *testing.T) (string, *[]string, *bytes.Buffer, *bytes.Buffer) {
		dir := pruneStateDir(t)
		pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lblRun", HerdrWorkspaceID: "wxRun", SandboxHandle: "herdr/live", CheckoutPath: goneCheckoutPath(t)})
		removeCalls := &[]string{}
		stubPruneVars(t, map[string]string{}, nil, removeCalls, nil)
		stubPruneStatus(t, coreruntime.SandboxStatusRunning, nil, nil)
		return dir, removeCalls, &bytes.Buffer{}, &bytes.Buffer{}
	}

	t.Run("refused-without-flag", func(t *testing.T) {
		dir, removeCalls, stdout, stderr := setup(t)
		code := runSpacePrune(context.Background(), []string{"--apply", "--workspace", "wxRun"}, stdout, stderr)
		if code != 1 {
			t.Fatalf("running refusal: want exit 1, got %d; stderr=%q", code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "refusing to reclaim RUNNING sandbox herdr/live") {
			t.Errorf("running refusal: want refusal line; got %q", stderr.String())
		}
		if len(*removeCalls) != 0 {
			t.Errorf("running refusal: remove must not be called; got %v", *removeCalls)
		}
		bs, _ := herdrspace.List(context.Background(), dir)
		if len(bs) != 1 {
			t.Errorf("running refusal: binding must survive; got %v", bs)
		}
	})

	t.Run("reclaimed-with-flag", func(t *testing.T) {
		dir, removeCalls, stdout, stderr := setup(t)
		code := runSpacePrune(context.Background(), []string{"--apply", "--workspace", "wxRun", "--kill-running"}, stdout, stderr)
		if code != 0 {
			t.Fatalf("kill-running: want exit 0, got %d; stderr=%q", code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "reclaimed herdr/live") {
			t.Errorf("kill-running: want reclaimed line; got %q", stdout.String())
		}
		if len(*removeCalls) != 1 {
			t.Errorf("kill-running: want remove called once; got %v", *removeCalls)
		}
		bs, _ := herdrspace.List(context.Background(), dir)
		if len(bs) != 0 {
			t.Errorf("kill-running: binding must be deleted; got %v", bs)
		}
	})
}

func TestSpacePrune_StatusProbeFailureRefuses(t *testing.T) {
	dir := pruneStateDir(t)
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lblP", HerdrWorkspaceID: "wxP", SandboxHandle: "p/unknownstate", CheckoutPath: goneCheckoutPath(t)})

	var removeCalls []string
	stubPruneVars(t, map[string]string{}, nil, &removeCalls, nil)
	stubPruneStatus(t, "", fmt.Errorf("msb daemon unreachable"), nil)

	var stdout, stderr bytes.Buffer
	code := runSpacePrune(context.Background(), []string{"--apply", "--workspace", "wxP"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("status probe failure: want exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "cannot determine sandbox status") {
		t.Errorf("status probe failure: want refusal line; got %q", stderr.String())
	}
	if len(removeCalls) != 0 {
		t.Errorf("status probe failure: remove must not be called; got %v", removeCalls)
	}
	bs, _ := herdrspace.List(context.Background(), dir)
	if len(bs) != 1 {
		t.Errorf("status probe failure: binding must survive; got %v", bs)
	}
}

func TestSpacePrune_AbsentSandboxIsReclaimable(t *testing.T) {
	dir := pruneStateDir(t)
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lblG", HerdrWorkspaceID: "wxG", SandboxHandle: "p/gone", CheckoutPath: goneCheckoutPath(t)})

	var removeCalls []string
	stubPruneVars(t, map[string]string{}, nil, &removeCalls, nil)
	stubPruneStatus(t, "", fmt.Errorf("%w: gone", service.ErrNotFound), nil)

	var stdout, stderr bytes.Buffer
	code := runSpacePrune(context.Background(), []string{"--apply", "--workspace", "wxG"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("absent sandbox: want 0, got %d; stderr=%q", code, stderr.String())
	}
	if len(removeCalls) != 1 {
		t.Errorf("absent sandbox: want remove called once; got %v", removeCalls)
	}
}

func TestSpacePrune_ProbeFailure_ExitsOneRemoveNotCalled(t *testing.T) {
	dir := pruneStateDir(t)
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lbl5", HerdrWorkspaceID: "wx5", SandboxHandle: "proj5/box5", CheckoutPath: goneCheckoutPath(t)})

	var removeCalls []string
	stubPruneVars(t, nil, fmt.Errorf("herdr unavailable"), &removeCalls, nil)

	var stdout, stderr bytes.Buffer
	code := runSpacePrune(context.Background(), nil, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("probe failure: want exit 1, got %d", code)
	}
	if len(removeCalls) != 0 {
		t.Errorf("probe failure: remove must not be called; got %v", removeCalls)
	}
	bs, _ := herdrspace.List(context.Background(), dir)
	if len(bs) != 1 {
		t.Errorf("probe failure: binding must survive; got %v", bs)
	}
}

func TestSpacePrune_WorkspaceScoping(t *testing.T) {
	dir := pruneStateDir(t)
	realDir := t.TempDir()
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lblS", HerdrWorkspaceID: "strandedID", SandboxHandle: "p/stranded", CheckoutPath: goneCheckoutPath(t)})
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lblL", HerdrWorkspaceID: "liveID", SandboxHandle: "p/live", CheckoutPath: realDir})

	var removeCalls []string
	stubPruneVars(t, map[string]string{"liveID": realDir}, nil, &removeCalls, nil)

	var stdout, stderr bytes.Buffer
	code := runSpacePrune(context.Background(), []string{"--apply", "--workspace", "liveID"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("scope live: want 0, got %d; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "considered=1") {
		t.Errorf("scope live: want considered=1; got %q", out)
	}
	if !strings.Contains(out, "reclaimable=0") {
		t.Errorf("scope live: want reclaimable=0; got %q", out)
	}
	if len(removeCalls) != 0 {
		t.Errorf("scope live: remove must not be called; got %v", removeCalls)
	}

	stdout.Reset()
	removeCalls = nil
	code = runSpacePrune(context.Background(), []string{"--workspace", "unknownID"}, &stdout, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("scope unknown: want 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "considered=0") {
		t.Errorf("scope unknown: want considered=0; got %q", stdout.String())
	}
}

func TestSpacePrune_HandleProjectFromHandle(t *testing.T) {
	dir := pruneStateDir(t)
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lbl7", HerdrWorkspaceID: "wx7", SandboxHandle: "otherproj/boxX", CheckoutPath: goneCheckoutPath(t)})

	var removeCalls []string
	stubPruneVars(t, map[string]string{}, nil, &removeCalls, nil)
	var statusCalls []string
	stubPruneStatus(t, coreruntime.SandboxStatusStopped, nil, &statusCalls)

	var stdout, stderr bytes.Buffer
	code := runSpacePrune(context.Background(), []string{"--apply", "--workspace", "wx7"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("handle project: want 0, got %d; stderr=%q", code, stderr.String())
	}
	if len(removeCalls) != 1 || removeCalls[0] != "otherproj/boxX" {
		t.Errorf("handle project: want remove with otherproj/boxX; got %v", removeCalls)
	}
	if len(statusCalls) != 1 || statusCalls[0] != "otherproj/boxX" {
		t.Errorf("handle project: want status probed with otherproj/boxX; got %v", statusCalls)
	}
}

func TestSpacePrune_ApplyWithoutWorkspaceRequiresAll(t *testing.T) {
	dir := pruneStateDir(t)
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lbl8", HerdrWorkspaceID: "wx8", SandboxHandle: "proj8/box8", CheckoutPath: goneCheckoutPath(t)})

	var removeCalls []string
	stubPruneVars(t, map[string]string{}, nil, &removeCalls, nil)

	var stdout, stderr bytes.Buffer
	code := runSpacePrune(context.Background(), []string{"--apply"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("apply-no-workspace: want exit 2, got %d; stderr=%q", code, stderr.String())
	}
	if len(removeCalls) != 0 {
		t.Errorf("apply-no-workspace: remove must not be called; got %v", removeCalls)
	}
	bs, _ := herdrspace.List(context.Background(), dir)
	if len(bs) != 1 {
		t.Errorf("apply-no-workspace: binding must survive; got %v", bs)
	}

	stdout.Reset()
	stderr.Reset()
	removeCalls = nil
	code = runSpacePrune(context.Background(), []string{"--apply", "--all"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("apply-all: want 0, got %d; stderr=%q", code, stderr.String())
	}
	if len(removeCalls) != 1 {
		t.Errorf("apply-all: want remove called once; got %v", removeCalls)
	}
}

func TestSpacePrune_OSStatPathAbsent(t *testing.T) {
	if os.Getenv("INVERT_GUARD") == "" {
		return
	}
	dir := pruneStateDir(t)
	realDir := t.TempDir()
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "inv", HerdrWorkspaceID: "inv1", SandboxHandle: "p/inv", CheckoutPath: realDir})
	var removeCalls []string
	stubPruneVars(t, map[string]string{"inv1": realDir}, nil, &removeCalls, nil)
	var stdout bytes.Buffer
	runSpacePrune(context.Background(), nil, &stdout, &bytes.Buffer{})
	if strings.Contains(stdout.String(), "would-reclaim") {
		t.Error("inverted guard fires correctly: alive workspace with existing path must not be stranded")
	}
}
