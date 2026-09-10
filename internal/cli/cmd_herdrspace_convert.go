//go:build linux

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/IniZio/herdr-plugin-msb/internal/core/herdrspace"
	"github.com/IniZio/herdr-plugin-msb/internal/core/service"
	"github.com/IniZio/herdr-plugin-msb/internal/runtime/msb"
)

type herdrWorkspaceInfo struct {
	Result struct {
		Workspace struct {
			WorkspaceID string `json:"workspace_id"`
			Label       string `json:"label"`
			ActiveTabID string `json:"active_tab_id"`
			Worktree    *struct {
				CheckoutPath     string `json:"checkout_path"`
				RepoRoot         string `json:"repo_root"`
				IsLinkedWorktree bool   `json:"is_linked_worktree"`
			} `json:"worktree"`
		} `json:"workspace"`
	} `json:"result"`
}

type herdrPaneListInfo struct {
	Result struct {
		Panes []struct {
			PaneID      string `json:"pane_id"`
			TabID       string `json:"tab_id"`
			WorkspaceID string `json:"workspace_id"`
			Focused     bool   `json:"focused"`
		} `json:"panes"`
	} `json:"result"`
}

var convertCreateSandbox = func(ctx context.Context, project string, opts service.CreateOptions) error {
	_, err := service.New(msb.New(), project).Create(ctx, opts)
	return err
}

var convertRemoveSandbox = func(ctx context.Context, project, name string) error {
	return service.New(msb.New(), project).Remove(ctx, name)
}

const defaultConvertImage = "alpine"

type convertWorkspace struct {
	Label            string
	CheckoutPath     string
	RepoRoot         string
	ActiveTabID      string
	IsLinkedWorktree bool
}

func herdrWorkspaceGet(ctx context.Context, bin, workspaceID string) (convertWorkspace, error) {
	out, err := exec.CommandContext(ctx, bin, "workspace", "get", workspaceID).Output()
	if err != nil {
		return convertWorkspace{}, fmt.Errorf("herdr workspace get %s: %w", workspaceID, err)
	}
	raw := strings.TrimSpace(string(out))
	var info herdrWorkspaceInfo
	if jsonErr := json.Unmarshal([]byte(raw), &info); jsonErr != nil {
		return convertWorkspace{}, fmt.Errorf("herdr workspace get %s: parse: %w: %s", workspaceID, jsonErr, raw)
	}
	ws := info.Result.Workspace
	if ws.Worktree == nil || strings.TrimSpace(ws.Worktree.CheckoutPath) == "" {
		return convertWorkspace{}, fmt.Errorf("herdr workspace %s has no worktree checkout_path; space-convert requires a worktree-backed workspace", workspaceID)
	}
	return convertWorkspace{
		Label:            ws.Label,
		CheckoutPath:     ws.Worktree.CheckoutPath,
		RepoRoot:         ws.Worktree.RepoRoot,
		ActiveTabID:      ws.ActiveTabID,
		IsLinkedWorktree: ws.Worktree.IsLinkedWorktree,
	}, nil
}

func refuseNonLinkedWorktree(ws convertWorkspace, workspaceID string) error {
	if ws.IsLinkedWorktree {
		return nil
	}
	root := strings.TrimSpace(ws.RepoRoot)
	if root == "" {
		root = ws.CheckoutPath
	}
	return fmt.Errorf("space-convert: refusing to sandbox workspace %s: %s is the primary checkout of %s, not a linked worktree (herdr reports is_linked_worktree=false or absent).\n"+
		"Sandboxing the primary checkout binds the tree you and your agents work in and tears down its panes when the sandbox is removed.\n"+
		"Instead: create a linked worktree (herdr worktree create), open a workspace on it, and run space-convert there",
		workspaceID, ws.CheckoutPath, root)
}

func herdrRootPaneID(ctx context.Context, bin, workspaceID, activeTabID string) (string, error) {
	out, err := exec.CommandContext(ctx, bin, "pane", "list", "--workspace", workspaceID).Output()
	if err != nil {
		return "", fmt.Errorf("herdr pane list --workspace %s: %w", workspaceID, err)
	}
	raw := strings.TrimSpace(string(out))
	var info herdrPaneListInfo
	if jsonErr := json.Unmarshal([]byte(raw), &info); jsonErr != nil {
		return "", fmt.Errorf("herdr pane list --workspace %s: parse: %w: %s", workspaceID, jsonErr, raw)
	}
	panes := info.Result.Panes
	if len(panes) == 0 {
		return "", fmt.Errorf("herdr pane list --workspace %s: no panes", workspaceID)
	}
	first := ""
	firstInTab := ""
	for _, p := range panes {
		if p.PaneID == "" {
			continue
		}
		if first == "" {
			first = p.PaneID
		}
		if activeTabID != "" && p.TabID == activeTabID {
			if p.Focused {
				return p.PaneID, nil
			}
			if firstInTab == "" {
				firstInTab = p.PaneID
			}
		}
	}
	if firstInTab != "" {
		return firstInTab, nil
	}
	if first == "" {
		return "", fmt.Errorf("herdr pane list --workspace %s: no pane_id", workspaceID)
	}
	return first, nil
}

func herdrPaneClose(ctx context.Context, bin, paneID string) error {
	cmd := exec.CommandContext(ctx, bin, "pane", "close", paneID)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("herdr pane close %s: %w", paneID, err)
	}
	return nil
}

func sandboxNameFromLabel(label, workspaceID string) string {
	s := strings.TrimSpace(label)
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		s = s[i+1:]
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		return "ws-" + workspaceID
	}
	return name
}

func runSpaceConvert(ctx context.Context, args []string, out, errW io.Writer) int {
	fs := flag.NewFlagSet("space-convert", flag.ContinueOnError)
	fs.SetOutput(errW)
	workspaceID := fs.String("workspace", "", "existing herdr workspace id (default: $HERDR_WORKSPACE_ID)")
	sandbox := fs.String("sandbox", "", "sandbox name (default: derived from workspace label)")
	image := fs.String("image", defaultConvertImage, "OCI image ref (default: alpine)")
	project := fs.String("project", service.DefaultProject, "msb project")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *workspaceID == "" {
		*workspaceID = os.Getenv("HERDR_WORKSPACE_ID")
	}
	if *workspaceID == "" {
		fmt.Fprintln(errW, "space-convert: workspace ID required (--workspace or HERDR_WORKSPACE_ID)")
		return 2
	}

	dir, err := StateDir(os.Getenv)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}

	existing, err := herdrspace.GetByWorkspaceID(ctx, dir, *workspaceID)
	if err == nil {
		fmt.Fprintf(errW, "space-convert: workspace %s is already bound to sandbox %s; use space-open-pane to attach\n", *workspaceID, existing.SandboxHandle)
		return 1
	}
	if !errors.Is(err, herdrspace.ErrNotFound) {
		fmt.Fprintln(errW, err)
		return 1
	}

	bin := herdrBin()
	ws, err := herdrWorkspaceGet(ctx, bin, *workspaceID)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}
	if refuseErr := refuseNonLinkedWorktree(ws, *workspaceID); refuseErr != nil {
		fmt.Fprintln(errW, refuseErr)
		return 1
	}
	label, checkoutPath := ws.Label, ws.CheckoutPath

	rootPaneID, err := herdrRootPaneID(ctx, bin, *workspaceID, ws.ActiveTabID)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}

	name := *sandbox
	if name == "" {
		name = sandboxNameFromLabel(label, *workspaceID)
	}

	opts := service.CreateOptions{
		Name:          name,
		ImageRef:      *image,
		Worktree:      checkoutPath,
		GuestWorktree: service.DefaultGuestWorktree,
		Credential:    true,
		Boot:          true,
	}
	if err := convertCreateSandbox(ctx, *project, opts); err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}

	if label == "" {
		label = "msb:" + name
	}
	b := herdrspace.Binding{
		SpaceLabel:       label,
		HerdrWorkspaceID: *workspaceID,
		SandboxHandle:    sandboxHandle(*project, name),
		CheckoutPath:     checkoutPath,
	}
	if putErr := herdrspace.Put(ctx, dir, b); putErr != nil {
		_ = convertRemoveSandbox(ctx, *project, name)
		fmt.Fprintln(errW, putErr)
		return 1
	}

	if err := herdrOpenGuestPane(ctx, bin, *workspaceID, rootPaneID, name, *project, checkoutPath, out); err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}

	if err := herdrPaneClose(ctx, bin, rootPaneID); err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}

	fmt.Fprintf(out, "space-convert: workspace=%s sandbox=%s worktree=%s closed-pane=%s\n", *workspaceID, b.SandboxHandle, checkoutPath, rootPaneID)
	return 0
}
