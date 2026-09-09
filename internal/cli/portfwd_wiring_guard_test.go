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

func TestFwdSyncDispatch(t *testing.T) {
	var errBuf bytes.Buffer
	code := RunHerdrPlugin(context.Background(), []string{"fwd-sync"}, &bytes.Buffer{}, &errBuf)
	if code != 2 {
		t.Fatalf("RunHerdrPlugin(\"fwd-sync\") returned %d, want 2", code)
	}
	got := errBuf.String()
	const wantMsg = "fwd-sync: --control-path and --ssh-host are required"
	if !strings.Contains(got, wantMsg) {
		t.Fatalf("stderr %q does not contain %q; fwd-sync dispatch may be missing", got, wantMsg)
	}
}

func TestPortfwdWiringGuard(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "cmd_herdr_plugin.go", nil, 0)
	if err != nil {
		t.Fatalf("parse cmd_herdr_plugin.go: %v", err)
	}
	var body *ast.BlockStmt
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "runFwdSync" {
			continue
		}
		body = fn.Body
	}
	if body == nil {
		t.Fatalf("runFwdSync not found in cmd_herdr_plugin.go; function was removed")
	}
	want := map[string]bool{
		"NewManager":      false,
		"Reconcile":       false,
		"TeardownSandbox": false,
		"DiscoverAll":     false,
	}
	if len(want) != 4 {
		t.Fatalf("want map has %d entries, expected 4; test is misconfigured", len(want))
	}
	totalSel := 0
	ast.Inspect(body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		totalSel++
		if _, exists := want[sel.Sel.Name]; exists {
			want[sel.Sel.Name] = true
		}
		return true
	})
	if totalSel < 4 {
		t.Fatalf("only %d SelectorExpr nodes in runFwdSync body; AST scan did not reach the function", totalSel)
	}
	for name, found := range want {
		if !found {
			t.Errorf("portfwd.%s has no call site in runFwdSync; wiring was removed", name)
		}
	}
}
