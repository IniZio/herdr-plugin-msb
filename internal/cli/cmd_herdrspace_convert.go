package cli

import (
	"context"
	"encoding/json"
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
				CheckoutPath string `json:"checkout_path"`
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

func herdrWorkspaceGet(ctx context.Context, bin, workspaceID string) (label, checkoutPath, activeTabID string, err error) {
	out, err := exec.CommandContext(ctx, bin, "workspace", "get", workspaceID).Output()
	if err != nil {
		return "", "", "", fmt.Errorf("herdr workspace get %s: %w", workspaceID, err)
	}
	raw := strings.TrimSpace(string(out))
	var info herdrWorkspaceInfo
	if jsonErr := json.Unmarshal([]byte(raw), &info); jsonErr != nil {
		return "", "", "", fmt.Errorf("herdr workspace get %s: parse: %w: %s", workspaceID, jsonErr, raw)
	}
	ws := info.Result.Workspace
	if ws.Worktree == nil || strings.TrimSpace(ws.Worktree.CheckoutPath) == "" {
		return "", "", "", fmt.Errorf("herdr workspace %s has no worktree checkout_path; space-convert requires a worktree-backed workspace", workspaceID)
	}
	return ws.Label, ws.Worktree.CheckoutPath, ws.ActiveTabID, nil
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
	workspaceID := fs.String("workspace", "", "existing herdr workspace id (required)")
	sandbox := fs.String("sandbox", "", "sandbox name (default: derived from workspace label)")
	image := fs.String("image", "", "OCI image ref (required)")
	project := fs.String("project", service.DefaultProject, "msb project")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *workspaceID == "" {
		fmt.Fprintln(errW, "space-convert: --workspace is required")
		return 2
	}
	if *image == "" {
		fmt.Fprintln(errW, "space-convert: --image is required")
		return 2
	}

	dir, err := StateDir(os.Getenv)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}

	bin := herdrBin()
	label, checkoutPath, activeTabID, err := herdrWorkspaceGet(ctx, bin, *workspaceID)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}

	rootPaneID, err := herdrRootPaneID(ctx, bin, *workspaceID, activeTabID)
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

	if err := herdrOpenGuestPane(ctx, bin, *workspaceID, rootPaneID, name, *project, out); err != nil {
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
