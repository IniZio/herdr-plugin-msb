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
)

func herdrBin() string {
	if v := os.Getenv("HERDR_BIN_PATH"); v != "" {
		return undeletedHerdrBin(v)
	}
	if v := os.Getenv("HERDR_BIN"); v != "" {
		return undeletedHerdrBin(v)
	}
	return "herdr"
}

func undeletedHerdrBin(v string) string {
	undeleted := strings.TrimSuffix(v, " (deleted)")
	if undeleted == v {
		return v
	}
	if _, err := os.Stat(undeleted); err != nil {
		return v
	}
	return undeleted
}

func sandboxHandle(project, name string) string { return project + "/" + name }

func splitHandle(handle string) (project, name string) {
	if i := strings.IndexByte(handle, '/'); i >= 0 {
		return handle[:i], handle[i+1:]
	}
	return service.DefaultProject, handle
}

func herdrWorkspaceCreate(ctx context.Context, bin, label string) (workspaceID, rootPaneID string, err error) {
	out, err := exec.CommandContext(ctx, bin, "workspace", "create", "--label", label, "--no-focus").Output()
	if err != nil {
		return "", "", fmt.Errorf("herdr workspace create: %w", err)
	}
	var env struct {
		Result struct {
			Workspace struct{ WorkspaceID string `json:"workspace_id"` } `json:"workspace"`
			RootPane  struct{ PaneID string `json:"pane_id"` }        `json:"root_pane"`
		} `json:"result"`
	}
	raw := strings.TrimSpace(string(out))
	if jsonErr := json.Unmarshal([]byte(raw), &env); jsonErr != nil {
		if raw == "" {
			return "", "", fmt.Errorf("herdr workspace create: empty output")
		}
		return raw, "", nil
	}
	id := env.Result.Workspace.WorkspaceID
	if id == "" {
		return "", "", fmt.Errorf("herdr workspace create: workspace_id not found in: %s", raw)
	}
	return id, env.Result.RootPane.PaneID, nil
}

func herdrWorkspaceClose(ctx context.Context, bin, workspaceID string) error {
	cmd := exec.CommandContext(ctx, bin, "workspace", "close", workspaceID)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func herdrOpenGuestPane(ctx context.Context, bin, workspaceID, rootPaneID, sandbox, project string, out io.Writer) error {
	args := []string{
		"plugin", "pane", "open",
		"--plugin", PluginID,
		"--entrypoint", "shell",
		"--env", "HERDR_MSB_SANDBOX=" + sandbox,
		"--env", "HERDR_MSB_PROJECT=" + project,
	}
	if rootPaneID != "" {
		args = append(args, "--placement", "split", "--target-pane", rootPaneID, "--direction", "right")
	} else {
		args = append(args, "--workspace", workspaceID)
	}
	args = append(args, "--no-focus")
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runSpaceCreate(ctx context.Context, args []string, out, errW io.Writer) int {
	fs := flag.NewFlagSet("space-create", flag.ContinueOnError)
	fs.SetOutput(errW)
	project := fs.String("project", service.DefaultProject, "msb project")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintln(errW, "usage: space-create [-project <p>] <sandbox-name>")
		return 2
	}
	name := fs.Args()[0]
	label := "msb:" + name

	dir, err := StateDir(os.Getenv)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}

	bin := herdrBin()
	workspaceID, rootPaneID, err := herdrWorkspaceCreate(ctx, bin, label)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}

	b := herdrspace.Binding{
		SpaceLabel:       label,
		HerdrWorkspaceID: workspaceID,
		SandboxHandle:    sandboxHandle(*project, name),
	}
	if putErr := herdrspace.Put(ctx, dir, b); putErr != nil {
		_ = herdrWorkspaceClose(ctx, bin, workspaceID)
		fmt.Fprintln(errW, putErr)
		return 1
	}

	if err := herdrOpenGuestPane(ctx, bin, workspaceID, rootPaneID, name, *project, out); err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}
	fmt.Fprintf(out, "space-create: workspace=%s sandbox=%s\n", workspaceID, b.SandboxHandle)
	return 0
}

func runSpaceOpenPane(ctx context.Context, args []string, out, errW io.Writer) int {
	fs := flag.NewFlagSet("space-open-pane", flag.ContinueOnError)
	fs.SetOutput(errW)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	workspaceID := fs.Arg(0)
	if workspaceID == "" {
		workspaceID = os.Getenv("HERDR_WORKSPACE_ID")
	}
	if workspaceID == "" {
		fmt.Fprintln(errW, "space-open-pane: workspace ID required (arg or HERDR_WORKSPACE_ID)")
		return 2
	}

	dir, err := StateDir(os.Getenv)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}

	b, err := herdrspace.GetByWorkspaceID(ctx, dir, workspaceID)
	if err != nil {
		fmt.Fprintln(errW, "space-open-pane: no binding for workspace", workspaceID)
		return 1
	}

	project, name := splitHandle(b.SandboxHandle)
	bin := herdrBin()
	if err := herdrOpenGuestPane(ctx, bin, workspaceID, "", name, project, out); err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}
	return 0
}

func runNewTab(ctx context.Context, args []string, out, errW io.Writer) int {
	fs := flag.NewFlagSet("new-tab", flag.ContinueOnError)
	fs.SetOutput(errW)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	workspaceID := fs.Arg(0)
	if workspaceID == "" {
		workspaceID = os.Getenv("HERDR_WORKSPACE_ID")
	}
	if workspaceID == "" {
		fmt.Fprintln(errW, "new-tab: workspace ID required (arg or HERDR_WORKSPACE_ID)")
		return 2
	}

	dir, err := StateDir(os.Getenv)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}

	b, lookupErr := herdrspace.GetByWorkspaceID(ctx, dir, workspaceID)
	if lookupErr == nil {
		project, name := splitHandle(b.SandboxHandle)
		bin := herdrBin()
		if err := herdrOpenGuestPane(ctx, bin, workspaceID, "", name, project, out); err != nil {
			fmt.Fprintln(errW, err)
			return 1
		}
		return 0
	}

	if !errors.Is(lookupErr, herdrspace.ErrNotFound) {
		fmt.Fprintln(errW, "new-tab: binding lookup:", lookupErr)
		return 1
	}

	bin := herdrBin()
	cmd := exec.CommandContext(ctx, bin, "tab", "create", "--workspace", workspaceID, "--focus")
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}
	return 0
}

func spaceCleanup(ctx context.Context, dir, project, name string, closeWorkspace bool) {
	if !closeWorkspace {
		return
	}
	handle := sandboxHandle(project, name)
	b, err := herdrspace.GetByHandle(ctx, dir, handle)
	if errors.Is(err, herdrspace.ErrNotFound) {
		return
	}
	if err != nil {
		return
	}
	_ = herdrspace.Delete(ctx, dir, b.SpaceLabel)
	_ = herdrWorkspaceClose(ctx, herdrBin(), b.HerdrWorkspaceID)
}
