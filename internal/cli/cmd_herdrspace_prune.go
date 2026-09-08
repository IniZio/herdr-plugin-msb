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

var pruneListWorkspaces = func(ctx context.Context, bin string) (map[string]string, error) {
	out, err := exec.CommandContext(ctx, bin, "workspace", "list").Output()
	if err != nil {
		return nil, fmt.Errorf("herdr workspace list: %w", err)
	}
	var info struct {
		Result struct {
			Workspaces []struct {
				WorkspaceID string `json:"workspace_id"`
				Worktree    *struct {
					CheckoutPath string `json:"checkout_path"`
				} `json:"worktree"`
			} `json:"workspaces"`
		} `json:"result"`
	}
	if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &info); jsonErr != nil {
		return nil, fmt.Errorf("herdr workspace list: parse: %w", jsonErr)
	}
	m := make(map[string]string, len(info.Result.Workspaces))
	for _, w := range info.Result.Workspaces {
		cp := ""
		if w.Worktree != nil {
			cp = strings.TrimSpace(w.Worktree.CheckoutPath)
		}
		m[w.WorkspaceID] = cp
	}
	return m, nil
}

var pruneRemoveSandbox = func(ctx context.Context, project, name string) error {
	return service.New(msb.New(), project).Remove(ctx, name)
}

var pruneStopSandbox = func(ctx context.Context, project, name string) error {
	_, err := service.New(msb.New(), project).Stop(ctx, name)
	return err
}

func pruneReclaim(ctx context.Context, project, name string) error {
	err := pruneRemoveSandbox(ctx, project, name)
	if err == nil {
		return nil
	}
	if stopErr := pruneStopSandbox(ctx, project, name); stopErr != nil {
		return fmt.Errorf("%w (stop after failed remove: %v)", err, stopErr)
	}
	return pruneRemoveSandbox(ctx, project, name)
}

func runSpacePrune(ctx context.Context, args []string, out, errW io.Writer) int {
	fs := flag.NewFlagSet("space-prune", flag.ContinueOnError)
	fs.SetOutput(errW)
	apply := fs.Bool("apply", false, "actually reclaim stranded sandboxes")
	all := fs.Bool("all", false, "permit unscoped --apply sweep (required when --apply without --workspace)")
	wsFilter := fs.String("workspace", "", "consider only this herdr workspace id")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *apply && *wsFilter == "" && !*all {
		fmt.Fprintln(errW, "space-prune: --apply without --workspace requires --all (refusing to reclaim every binding)")
		return 2
	}

	dir, err := StateDir(os.Getenv)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}

	allBindings, err := herdrspace.List(ctx, dir)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}

	bindings := allBindings
	if *wsFilter != "" {
		bindings = bindings[:0:0]
		for _, b := range allBindings {
			if b.HerdrWorkspaceID == *wsFilter {
				bindings = append(bindings, b)
			}
		}
	}

	bin := herdrBin()
	live, err := pruneListWorkspaces(ctx, bin)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}

	considered, reclaimable, applied := 0, 0, 0
	exitErr := false

	for _, b := range bindings {
		considered++
		reason := pruneStrandReason(live, b)
		if reason == "workspace-alive" {
			fmt.Fprintf(out, "space-prune: keep %s workspace=%s reason=%s\n", b.SandboxHandle, b.HerdrWorkspaceID, reason)
			continue
		}
		reclaimable++
		if !*apply {
			fmt.Fprintf(out, "space-prune: would-reclaim %s workspace=%s reason=%s\n", b.SandboxHandle, b.HerdrWorkspaceID, reason)
			continue
		}
		hp, hn := splitSandboxHandle(b.SandboxHandle)
		if removeErr := pruneReclaim(ctx, hp, hn); removeErr != nil {
			fmt.Fprintln(errW, removeErr)
			exitErr = true
			continue
		}
		if delErr := herdrspace.Delete(ctx, dir, b.SpaceLabel); delErr != nil {
			fmt.Fprintln(errW, delErr)
			exitErr = true
			continue
		}
		applied++
		fmt.Fprintf(out, "space-prune: reclaimed %s workspace=%s reason=%s\n", b.SandboxHandle, b.HerdrWorkspaceID, reason)
	}

	fmt.Fprintf(out, "space-prune: considered=%d reclaimable=%d applied=%d apply=%v\n", considered, reclaimable, applied, *apply)
	if exitErr {
		return 1
	}
	return 0
}

func pruneStrandReason(live map[string]string, b herdrspace.Binding) string {
	checkoutPath, ok := live[b.HerdrWorkspaceID]
	if !ok {
		return "workspace-gone"
	}
	if checkoutPath == "" {
		return "workspace-alive"
	}
	if _, err := os.Stat(checkoutPath); os.IsNotExist(err) {
		return "worktree-gone:" + checkoutPath
	}
	return "workspace-alive"
}

func splitSandboxHandle(handle string) (project, name string) {
	i := strings.LastIndex(handle, "/")
	if i < 0 {
		return "", handle
	}
	return handle[:i], handle[i+1:]
}
