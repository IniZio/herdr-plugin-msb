package cli

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestRunDelegation(t *testing.T) {
	ctx := context.Background()
	for _, verb := range []string{"declare", "status", "list", "local-agent"} {
		var stdout, stderr bytes.Buffer
		Run(ctx, []string{verb}, &stdout, &stderr)
		if strings.Contains(stderr.String(), "unknown command") {
			t.Errorf("verb %q: unexpected 'unknown command' in stderr: %s", verb, stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	code := Run(ctx, []string{"nosuchverb"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("nosuchverb: expected exit 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Errorf("nosuchverb: expected 'unknown command' in stderr, got: %s", stderr.String())
	}
}

func TestRunNoArgs(t *testing.T) {
	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	code := Run(ctx, nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
	if stderr.Len() == 0 {
		t.Error("expected usage on stderr, got nothing")
	}
	if stdout.Len() != 0 {
		t.Errorf("expected nothing on stdout, got: %s", stdout.String())
	}
}

func TestRunHelp(t *testing.T) {
	ctx := context.Background()
	expectedVerbs := []string{
		"create", "ps", "exec", "start", "stop", "rm",
		"version", "declare", "status", "list", "local-agent", "help",
	}
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		var stdout, stderr bytes.Buffer
		code := Run(ctx, args, &stdout, &stderr)
		if code != 0 {
			t.Errorf("%v: expected exit 0, got %d", args, code)
		}
		if stdout.Len() == 0 {
			t.Errorf("%v: expected usage on stdout", args)
		}
		if stderr.Len() != 0 {
			t.Errorf("%v: unexpected stderr: %s", args, stderr.String())
		}
		out := stdout.String()
		for _, v := range expectedVerbs {
			if !strings.Contains(out, v) {
				t.Errorf("%v: usage missing verb %q", args, v)
			}
		}
	}
}

func TestRunVersion(t *testing.T) {
	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	code := Run(ctx, []string{"version"}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "revision=") {
		t.Errorf("version output missing 'revision=': %s", stdout.String())
	}
}

func TestRunCreateValidation(t *testing.T) {
	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	code := Run(ctx, []string{"create"}, &stdout, &stderr)
	if code == 0 {
		t.Error("create with no --name: expected non-zero exit")
	}
	if !strings.Contains(stderr.String(), "name") {
		t.Errorf("create with no --name: expected 'name' in stderr, got: %s", stderr.String())
	}
}

func TestRunExecValidation(t *testing.T) {
	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	code := Run(ctx, []string{"exec", "mybox"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exec with no --: expected exit 2, got %d", code)
	}
}

// shippedVerbs is the single source of truth for the verb registry: every verb
// that appears in the usage text. helpAliases dispatch but are not listed there.
// Shipped verbs are NEVER executed here — space-convert, space-open-pane and
// new-tab create sandboxes, bindings and panes, and default-shell syscall.Execs
// away the test binary. Their runtime behaviour has dedicated tests above and in
// cmd_herdrspace_*_test.go; this test asserts the registry statically.
var shippedVerbs = []string{
	"create", "ps", "exec", "start", "stop", "rm",
	"version", "declare", "status", "list", "local-agent", "fwd-sync", "attach", "wrap-herdr", "ports-pane", "ports-toggle", "help",
	"space-create", "space-convert", "space-open-pane", "new-tab", "space-prune",
	"default-shell",
}

var helpAliases = []string{"--help", "-h"}

var declinedVerbs = []string{
	"shell", "run", "ssh", "log", "snapshot",
	"fork", "sandbox", "volume", "image", "cp", "auth",
	"egress", "ls", "harvest", "orca", "pause", "reap",
	"recover", "restore", "resume", "mcp", "doctor", "forward",
	"supervisor-upgrade", "supervisor-backfill-netns-identity",
}

func usageVerbs(t *testing.T) []string {
	t.Helper()
	var stdout bytes.Buffer
	Run(context.Background(), []string{"help"}, &stdout, &bytes.Buffer{})
	out := stdout.String()

	var found []string
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		colonIdx := strings.Index(trimmed, ":")
		if colonIdx <= 0 {
			continue
		}
		label := trimmed[:colonIdx]
		if strings.Contains(label, " ") || label == "usage" {
			continue
		}
		for _, word := range strings.Fields(trimmed[colonIdx+1:]) {
			found = append(found, word)
		}
	}

	return found
}

func dispatchCases(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "run.go", nil, 0)
	if err != nil {
		t.Fatalf("parse run.go: %v", err)
	}

	var sw *ast.SwitchStmt
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Run" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if s, ok := n.(*ast.SwitchStmt); ok && sw == nil {
				sw = s
				return false
			}
			return true
		})
		break
	}
	if sw == nil {
		t.Fatal("Run switch not found in run.go")
	}

	found := map[string]bool{}
	for _, stmt := range sw.Body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok || cc.List == nil {
			continue
		}
		for _, expr := range cc.List {
			lit, ok := expr.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			found[strings.Trim(lit.Value, `"`)] = true
		}
	}

	return found
}

func TestVerbRegistry(t *testing.T) {
	registry := make(map[string]bool, len(shippedVerbs))
	for _, v := range shippedVerbs {
		registry[v] = true
	}
	dispatchable := make(map[string]bool, len(registry)+len(helpAliases))
	for v := range registry {
		dispatchable[v] = true
	}
	for _, v := range helpAliases {
		dispatchable[v] = true
	}

	t.Run("usage_matches_registry", func(t *testing.T) {
		found := usageVerbs(t)
		foundSet := make(map[string]bool, len(found))
		for _, v := range found {
			foundSet[v] = true
		}
		for _, v := range shippedVerbs {
			if !foundSet[v] {
				t.Errorf("usage missing shipped verb %q", v)
			}
		}
		for _, v := range found {
			if !registry[v] {
				t.Errorf("usage lists unregistered verb %q", v)
			}
		}
		if len(found) != len(shippedVerbs) {
			t.Errorf("usage verb count: got %d, want %d; found=%v", len(found), len(shippedVerbs), found)
		}
	})

	t.Run("dispatch_matches_registry", func(t *testing.T) {
		cases := dispatchCases(t)
		for verb := range cases {
			if !dispatchable[verb] {
				t.Errorf("run.go switch has unregistered case %q — add it to shippedVerbs and usage, or remove the case", verb)
			}
		}
		for verb := range dispatchable {
			if !cases[verb] {
				t.Errorf("registered verb %q missing from run.go switch — was it removed?", verb)
			}
		}
	})

	t.Run("declined_verbs_rejected", func(t *testing.T) {
		cases := dispatchCases(t)
		ctx := context.Background()
		for _, verb := range declinedVerbs {
			if cases[verb] {
				t.Errorf("declined verb %q has a case in run.go switch", verb)
				continue
			}
			var stdout, stderr bytes.Buffer
			code := Run(ctx, []string{verb}, &stdout, &stderr)
			if code != 2 {
				t.Errorf("declined verb %q: expected exit 2, got %d", verb, code)
			}
			if !strings.Contains(stderr.String(), "unknown command") {
				t.Errorf("declined verb %q: expected 'unknown command' in stderr, got: %s", verb, stderr.String())
			}
		}
	})
}
