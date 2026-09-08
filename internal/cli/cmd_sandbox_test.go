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

func TestUsageVerbs(t *testing.T) {
	ctx := context.Background()
	var stdout bytes.Buffer
	Run(ctx, []string{"help"}, &stdout, &bytes.Buffer{})
	out := stdout.String()

	expected := []string{
		"create", "ps", "exec", "start", "stop", "rm",
		"version", "declare", "status", "list", "local-agent", "help",
	}

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

	expectedSet := make(map[string]bool, len(expected))
	for _, v := range expected {
		expectedSet[v] = true
	}
	foundSet := make(map[string]bool, len(found))
	for _, v := range found {
		foundSet[v] = true
	}
	for _, v := range expected {
		if !foundSet[v] {
			t.Errorf("usage missing expected verb %q", v)
		}
	}
	for _, v := range found {
		if !expectedSet[v] {
			t.Errorf("usage lists unexpected verb %q", v)
		}
	}
	if len(found) != len(expected) {
		t.Errorf("usage verb count: got %d, want %d; found=%v", len(found), len(expected), found)
	}
}

func TestShippedVerbRegistry(t *testing.T) {
	shippedVerbs := []string{
		"create", "ps", "exec", "start", "stop", "rm",
		"version", "declare", "status", "list", "local-agent",
		"help", "--help", "-h",
	}
	declinedVerbs := []string{
		"shell", "run", "attach", "ssh", "log", "snapshot",
		"fork", "sandbox", "volume", "image", "cp", "auth",
		"egress", "ls", "harvest", "orca", "pause", "reap",
		"recover", "restore", "resume", "mcp", "doctor", "forward",
		"supervisor-upgrade", "supervisor-backfill-netns-identity",
	}

	ctx := context.Background()
	for _, verb := range shippedVerbs {
		var stdout, stderr bytes.Buffer
		Run(ctx, []string{verb}, &stdout, &stderr)
		if strings.Contains(stderr.String(), "unknown command") {
			t.Errorf("shipped verb %q: unexpected 'unknown command' in stderr: %s", verb, stderr.String())
		}
	}

	for _, verb := range declinedVerbs {
		var stdout, stderr bytes.Buffer
		Run(ctx, []string{verb}, &stdout, &stderr)
		if !strings.Contains(stderr.String(), "unknown command") {
			t.Errorf("declined verb %q: expected 'unknown command' in stderr, got: %s", verb, stderr.String())
		}
	}
}

func TestDispatchSetEquality(t *testing.T) {
	declared := map[string]bool{
		"help": true, "--help": true, "-h": true,
		"create": true, "ps": true, "exec": true,
		"start": true, "stop": true, "rm": true,
		"declare": true, "status": true, "list": true, "local-agent": true,
		"version": true,
	}

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

	for verb := range found {
		if !declared[verb] {
			t.Errorf("run.go switch has undeclared case %q — add to declared set or remove the case", verb)
		}
	}
	for verb := range declared {
		if !found[verb] {
			t.Errorf("declared verb %q missing from run.go switch — was it removed?", verb)
		}
	}
}
