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
)

func stubPruneVars(t *testing.T, liveMap map[string]string, listErr error, removeCalls *[]string, removeErr error) {
	t.Helper()
	origList := pruneListWorkspaces
	origRemove := pruneRemoveSandbox
	origStop := pruneStopSandbox
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
	t.Cleanup(func() {
		pruneListWorkspaces = origList
		pruneRemoveSandbox = origRemove
		pruneStopSandbox = origStop
	})
}

func TestSpacePrune_StopsRunningSandboxBeforeRemove(t *testing.T) {
	dir := pruneStateDir(t)
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lblR", HerdrWorkspaceID: "wxR", SandboxHandle: "herdr/running"})

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
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lblS", HerdrWorkspaceID: "wxS", SandboxHandle: "herdr/stuck"})

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
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lbl1", HerdrWorkspaceID: "wx1", SandboxHandle: "proj/box1"})

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
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lbl2", HerdrWorkspaceID: "wx2", SandboxHandle: "proj2/box2"})

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
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lbl3", HerdrWorkspaceID: "wx3", SandboxHandle: "proj3/box3"})

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

func TestSpacePrune_WorktreeGone(t *testing.T) {
	dir := pruneStateDir(t)
	gonePath := filepath.Join(t.TempDir(), "nonexistent")
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lbl4", HerdrWorkspaceID: "wx4", SandboxHandle: "proj4/box4"})

	var removeCalls []string
	stubPruneVars(t, map[string]string{"wx4": gonePath}, nil, &removeCalls, nil)

	var stdout, stderr bytes.Buffer
	code := runSpacePrune(context.Background(), nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("worktree-gone dry: want 0, got %d", code)
	}
	out := stdout.String()
	if !strings.Contains(out, "would-reclaim") || !strings.Contains(out, "worktree-gone:") {
		t.Errorf("worktree-gone dry: want 'would-reclaim ... worktree-gone:'; got %q", out)
	}

	removeCalls = nil
	stdout.Reset()
	code = runSpacePrune(context.Background(), []string{"--apply", "--workspace", "wx4"}, &stdout, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("worktree-gone apply: want 0, got %d", code)
	}
	out = stdout.String()
	if !strings.Contains(out, "reclaimed") || !strings.Contains(out, "worktree-gone:") {
		t.Errorf("worktree-gone apply: want 'reclaimed ... worktree-gone:'; got %q", out)
	}
	if len(removeCalls) != 1 {
		t.Errorf("worktree-gone apply: want remove called once; got %v", removeCalls)
	}
}

func TestSpacePrune_ProbeFailure_ExitsOneRemoveNotCalled(t *testing.T) {
	dir := pruneStateDir(t)
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lbl5", HerdrWorkspaceID: "wx5", SandboxHandle: "proj5/box5"})

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
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lblS", HerdrWorkspaceID: "strandedID", SandboxHandle: "p/stranded"})
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lblL", HerdrWorkspaceID: "liveID", SandboxHandle: "p/live"})

	realDir := t.TempDir()
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
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lbl7", HerdrWorkspaceID: "wx7", SandboxHandle: "otherproj/boxX"})

	var removeCalls []string
	stubPruneVars(t, map[string]string{}, nil, &removeCalls, nil)

	var stdout, stderr bytes.Buffer
	code := runSpacePrune(context.Background(), []string{"--apply", "--workspace", "wx7"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("handle project: want 0, got %d; stderr=%q", code, stderr.String())
	}
	if len(removeCalls) != 1 || removeCalls[0] != "otherproj/boxX" {
		t.Errorf("handle project: want remove with otherproj/boxX; got %v", removeCalls)
	}
}

func TestSpacePrune_ApplyWithoutWorkspaceRequiresAll(t *testing.T) {
	dir := pruneStateDir(t)
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "lbl8", HerdrWorkspaceID: "wx8", SandboxHandle: "proj8/box8"})

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
	pruneAddBinding(t, dir, herdrspace.Binding{SpaceLabel: "inv", HerdrWorkspaceID: "inv1", SandboxHandle: "p/inv"})
	var removeCalls []string
	stubPruneVars(t, map[string]string{"inv1": t.TempDir()}, nil, &removeCalls, nil)
	var stdout bytes.Buffer
	runSpacePrune(context.Background(), nil, &stdout, &bytes.Buffer{})
	if strings.Contains(stdout.String(), "would-reclaim") {
		t.Error("inverted guard fires correctly: alive workspace with existing path must not be stranded")
	}
}
