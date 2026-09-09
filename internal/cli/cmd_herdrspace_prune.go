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
	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
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

var pruneSandboxStatus = func(ctx context.Context, project, name string) (coreruntime.SandboxStatus, error) {
	ref, err := service.New(msb.New(), project).Resolve(ctx, name)
	if err != nil {
		return "", err
	}
	return ref.Status, nil
}

func pruneRunningCheck(ctx context.Context, project, name string) (running bool, err error) {
	status, statusErr := pruneSandboxStatus(ctx, project, name)
	if errors.Is(statusErr, service.ErrNotFound) {
		return false, nil
	}
	if statusErr != nil {
		return false, statusErr
	}
	return status == coreruntime.SandboxStatusRunning, nil
}

type sandboxLiveness int

const (
	sandboxLivenessUnknown sandboxLiveness = iota
	sandboxLivenessRunning
	sandboxLivenessIdle
)

func pruneLiveness(ctx context.Context, handle string) (sandboxLiveness, error) {
	project, name := splitSandboxHandle(handle)
	running, err := pruneRunningCheck(ctx, project, name)
	if err != nil {
		return sandboxLivenessUnknown, err
	}
	if running {
		return sandboxLivenessRunning, nil
	}
	return sandboxLivenessIdle, nil
}

func pruneReclaim(ctx context.Context, project, name string) error {
	err := pruneRemoveSandbox(ctx, project, name)
	if err == nil || errors.Is(err, service.ErrNotFound) {
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
	killRunning := fs.Bool("kill-running", false, "permit reclaiming a RUNNING sandbox (destroys any in-flight work inside it)")
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
		state := sandboxLivenessUnknown
		if strings.TrimSpace(b.CheckoutPath) == "" {
			var stateErr error
			state, stateErr = pruneLiveness(ctx, b.SandboxHandle)
			if stateErr != nil {
				fmt.Fprintf(errW, "space-prune: %s: cannot determine sandbox status: %v\n", b.SandboxHandle, stateErr)
			}
		}
		reason, stranded := pruneStrandReason(live, b, state)
		if !stranded {
			fmt.Fprintf(out, "space-prune: keep %s workspace=%s reason=%s\n", b.SandboxHandle, b.HerdrWorkspaceID, reason)
			continue
		}
		reclaimable++
		if !*apply {
			fmt.Fprintf(out, "space-prune: would-reclaim %s workspace=%s reason=%s\n", b.SandboxHandle, b.HerdrWorkspaceID, reason)
			continue
		}
		hp, hn := splitSandboxHandle(b.SandboxHandle)
		running, statusErr := pruneRunningCheck(ctx, hp, hn)
		if statusErr != nil {
			fmt.Fprintf(errW, "space-prune: refusing %s: cannot determine sandbox status: %v\n", b.SandboxHandle, statusErr)
			exitErr = true
			continue
		}
		if running && !*killRunning {
			fmt.Fprintf(errW, "space-prune: refusing to reclaim RUNNING sandbox %s workspace=%s (pass --kill-running to destroy it and any in-flight work)\n", b.SandboxHandle, b.HerdrWorkspaceID)
			exitErr = true
			continue
		}
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

func pruneStrandReason(live map[string]string, b herdrspace.Binding, state sandboxLiveness) (reason string, stranded bool) {
	_, workspaceLive := live[b.HerdrWorkspaceID]
	path := strings.TrimSpace(b.CheckoutPath)
	if path == "" {
		switch {
		case workspaceLive:
			return "workspace-alive-no-worktree", false
		case state == sandboxLivenessRunning:
			return "workspace-gone-sandbox-running", false
		case state == sandboxLivenessIdle:
			return "workspace-gone+sandbox-not-running", true
		default:
			return "sandbox-state-unknown", false
		}
	}
	_, statErr := os.Stat(path)
	pathGone := os.IsNotExist(statErr)
	switch {
	case !workspaceLive && pathGone:
		return "workspace-gone+worktree-gone:" + path, true
	case !workspaceLive:
		return "workspace-gone-worktree-present:" + path, false
	case pathGone:
		return "worktree-gone-workspace-alive:" + path, false
	}
	return "workspace-alive", false
}

func splitSandboxHandle(handle string) (project, name string) {
	i := strings.LastIndex(handle, "/")
	if i < 0 {
		return "", handle
	}
	return handle[:i], handle[i+1:]
}
