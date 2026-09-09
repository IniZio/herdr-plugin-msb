package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestPortfwdWiringGuard(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "cmd_herdr_plugin.go", nil, 0)
	if err != nil {
		t.Fatalf("parse cmd_herdr_plugin.go: %v", err)
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
	ast.Inspect(f, func(n ast.Node) bool {
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
	if totalSel < 20 {
		t.Fatalf("only %d SelectorExpr nodes found in cmd_herdr_plugin.go; AST scan did not reach the file body", totalSel)
	}
	for name, found := range want {
		if !found {
			t.Errorf("portfwd.%s has no call site in cmd_herdr_plugin.go; wiring was removed", name)
		}
	}
}
