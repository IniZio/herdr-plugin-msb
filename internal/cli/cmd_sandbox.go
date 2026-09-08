package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	debug "runtime/debug"
	"strconv"
	"strings"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
	"github.com/IniZio/herdr-plugin-msb/internal/core/service"
	"github.com/IniZio/herdr-plugin-msb/internal/runtime/msb"
)

type portList []uint16

func (p *portList) String() string {
	parts := make([]string, len(*p))
	for i, v := range *p {
		parts[i] = strconv.Itoa(int(v))
	}
	return strings.Join(parts, ",")
}

func (p *portList) Set(s string) error {
	n, err := strconv.ParseUint(s, 10, 16)
	if err != nil {
		return fmt.Errorf("invalid port %q", s)
	}
	*p = append(*p, uint16(n))
	return nil
}

type envList []string

func (e *envList) String() string { return strings.Join(*e, ",") }
func (e *envList) Set(s string) error {
	*e = append(*e, s)
	return nil
}

func runCreate(ctx context.Context, args []string, out, errW io.Writer) int {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	fs.SetOutput(errW)
	name := fs.String("name", "", "sandbox name (required)")
	image := fs.String("image", "", "OCI image ref (required)")
	worktree := fs.String("worktree", "", "host dir mounted rw")
	guestPath := fs.String("guest-path", service.DefaultGuestWorktree, "guest mount point")
	mem := fs.Uint("mem", 0, "memory MiB (0=service default)")
	vcpus := fs.Uint("vcpus", 0, "vCPU count")
	cred := fs.Bool("cred", true, "mount Claude credential store")
	credPath := fs.String("cred-path", "", "credential directory path")
	motive := fs.String("motive", "", "motive string")
	noBoot := fs.Bool("no-boot", false, "create only, skip boot")
	project := fs.String("project", service.DefaultProject, "project name")
	var ports portList
	fs.Var(&ports, "port", "expose port (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *name == "" {
		fmt.Fprintln(errW, "create: --name is required")
		return 2
	}
	if *image == "" {
		fmt.Fprintln(errW, "create: --image is required")
		return 2
	}
	opts := service.CreateOptions{
		Name:           *name,
		ImageRef:       *image,
		Worktree:       *worktree,
		GuestWorktree:  *guestPath,
		MemoryMiB:      uint32(*mem),
		VCPUs:          uint32(*vcpus),
		Credential:     *cred,
		CredentialPath: *credPath,
		Motive:         *motive,
		Boot:           !*noBoot,
		Ports:          []uint16(ports),
	}
	svc := service.New(msb.New(), *project)
	ref, err := svc.Create(ctx, opts)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}
	fmt.Fprintf(out, "created %s id=%s status=%s\n", ref.Name, ref.ID, ref.Status)
	return 0
}

func runPS(ctx context.Context, args []string, out, errW io.Writer) int {
	fs := flag.NewFlagSet("ps", flag.ContinueOnError)
	fs.SetOutput(errW)
	project := fs.String("project", service.DefaultProject, "project name")
	asJSON := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	svc := service.New(msb.New(), *project)
	refs, err := svc.List(ctx)
	if err != nil {
		fmt.Fprintln(errW, err)
		return 1
	}
	if len(refs) == 0 {
		return 0
	}
	if *asJSON {
		enc := json.NewEncoder(out)
		_ = enc.Encode(refs)
		return 0
	}
	for _, r := range refs {
		fmt.Fprintf(out, "%s\t%s\t%s\n", r.Name, r.ID, r.Status)
	}
	return 0
}

// ptySize resolves the guest PTY size: explicit flags win, else the real size
// of whichever standard stream is a terminal, else a conservative 24x80.
func ptySize(rows, cols uint16) coreruntime.WinSize {
	if rows > 0 && cols > 0 {
		return coreruntime.WinSize{Rows: rows, Cols: cols}
	}
	for _, f := range []*os.File{os.Stdin, os.Stdout, os.Stderr} {
		if ws, ok := terminalSize(int(f.Fd())); ok {
			if rows > 0 {
				ws.Rows = rows
			}
			if cols > 0 {
				ws.Cols = cols
			}
			return ws
		}
	}
	if rows == 0 {
		rows = 24
	}
	if cols == 0 {
		cols = 80
	}
	return coreruntime.WinSize{Rows: rows, Cols: cols}
}

func applyPTYFields(req *coreruntime.ExecRequest, resizeCh <-chan coreruntime.WinSize, raw bool, rows, cols uint16) {
	req.TTY = true
	req.StdinReader = os.Stdin
	if raw {
		req.ResizeCh = resizeCh
	}
	ws := ptySize(rows, cols)
	req.Rows, req.Cols = ws.Rows, ws.Cols
}

func runExec(ctx context.Context, args []string, out, errW io.Writer) int {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	fs.SetOutput(errW)
	project := fs.String("project", service.DefaultProject, "project name")
	cwd := fs.String("cwd", "", "working directory in guest")
	pty := fs.Bool("pty", false, "allocate a PTY in the guest")
	rows := fs.Uint("rows", 0, "guest PTY rows (0=detect from terminal)")
	cols := fs.Uint("cols", 0, "guest PTY cols (0=detect from terminal)")
	var envs envList
	fs.Var(&envs, "env", "env var KEY=VALUE (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	remaining := fs.Args()
	sepIdx := -1
	for i, a := range remaining {
		if a == "--" {
			sepIdx = i
			break
		}
	}
	if sepIdx <= 0 || sepIdx+1 >= len(remaining) {
		fmt.Fprintln(errW, "usage: exec [flags] <name> -- <argv...>")
		return 2
	}
	name := remaining[0]
	guestArgv := remaining[sepIdx+1:]
	envMap := make(map[string]string, len(envs))
	for _, kv := range envs {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			fmt.Fprintf(errW, "invalid env %q: must be KEY=VALUE\n", kv)
			return 2
		}
		envMap[parts[0]] = parts[1]
	}
	svc := service.New(msb.New(), *project)
	req := coreruntime.ExecRequest{
		Argv:   guestArgv,
		Env:    envMap,
		Cwd:    *cwd,
		Stdout: out,
		Stderr: errW,
	}
	if *pty {
		resizeCh, cleanup, raw := enterRawMode(int(os.Stdin.Fd()))
		defer cleanup()
		applyPTYFields(&req, resizeCh, raw, uint16(*rows), uint16(*cols))
	}
	res, err := svc.Exec(ctx, name, req)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			fmt.Fprintf(errW, "no such sandbox: %s\n", name)
		} else {
			fmt.Fprintln(errW, err)
		}
		return 1
	}
	// Return the guest's own exit code so a failing guest command is distinguishable from a CLI error.
	return int(res.ExitCode)
}

func runStart(ctx context.Context, args []string, out, errW io.Writer) int {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(errW)
	project := fs.String("project", service.DefaultProject, "project name")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintln(errW, "usage: start <name>")
		return 2
	}
	name := fs.Args()[0]
	svc := service.New(msb.New(), *project)
	ref, err := svc.Start(ctx, name)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			fmt.Fprintf(errW, "no such sandbox: %s\n", name)
		} else {
			fmt.Fprintln(errW, err)
		}
		return 1
	}
	fmt.Fprintf(out, "started %s status=%s\n", ref.Name, ref.Status)
	return 0
}

func runStop(ctx context.Context, args []string, out, errW io.Writer) int {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	fs.SetOutput(errW)
	project := fs.String("project", service.DefaultProject, "project name")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintln(errW, "usage: stop <name>")
		return 2
	}
	name := fs.Args()[0]
	svc := service.New(msb.New(), *project)
	ref, err := svc.Stop(ctx, name)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			fmt.Fprintf(errW, "no such sandbox: %s\n", name)
		} else {
			fmt.Fprintln(errW, err)
		}
		return 1
	}
	fmt.Fprintf(out, "stopped %s status=%s\n", ref.Name, ref.Status)
	if dir, dirErr := StateDir(os.Getenv); dirErr == nil {
		spaceCleanup(ctx, dir, *project, name, false)
	}
	return 0
}

func runRM(ctx context.Context, args []string, out, errW io.Writer) int {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	fs.SetOutput(errW)
	project := fs.String("project", service.DefaultProject, "project name")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintln(errW, "usage: rm <name>")
		return 2
	}
	name := fs.Args()[0]
	svc := service.New(msb.New(), *project)
	if err := svc.Remove(ctx, name); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			fmt.Fprintf(errW, "no such sandbox: %s\n", name)
		} else {
			fmt.Fprintln(errW, err)
		}
		return 1
	}
	fmt.Fprintf(out, "removed %s\n", name)
	if dir, dirErr := StateDir(os.Getenv); dirErr == nil {
		spaceCleanup(ctx, dir, *project, name, true)
	}
	return 0
}

func runVersion(_ context.Context, _ []string, out, _ io.Writer) int {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		fmt.Fprintln(out, "herdr-plugin-msb")
		fmt.Fprintln(out, "revision=unknown")
		return 0
	}
	fmt.Fprintln(out, "herdr-plugin-msb")
	fmt.Fprintf(out, "go=%s\n", info.GoVersion)
	fmt.Fprintf(out, "path=%s\n", info.Main.Path)
	var rev, built, dirty string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			built = s.Value
		case "vcs.modified":
			dirty = s.Value
		}
	}
	fmt.Fprintf(out, "revision=%s\n", rev)
	fmt.Fprintf(out, "built=%s\n", built)
	fmt.Fprintf(out, "dirty=%s\n", dirty)
	return 0
}
