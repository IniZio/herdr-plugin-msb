package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func sigPresent(args []ast.Expr, pkg, name string) bool {
	for _, a := range args {
		sel, ok := a.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		id, ok2 := sel.X.(*ast.Ident)
		if ok2 && id.Name == pkg && sel.Sel.Name == name {
			return true
		}
	}
	return false
}

func TestMainSignalNotifyContextSignals(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	var mainFn *ast.FuncDecl
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if ok && fn.Name.Name == "main" {
			mainFn = fn
			break
		}
	}
	if mainFn == nil {
		t.Fatal("main() not found in main.go")
	}

	var call *ast.CallExpr
	ast.Inspect(mainFn.Body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok2 := c.Fun.(*ast.SelectorExpr)
		if ok2 && sel.Sel.Name == "NotifyContext" {
			if id, ok3 := sel.X.(*ast.Ident); ok3 && id.Name == "signal" {
				call = c
			}
		}
		return true
	})
	if call == nil {
		t.Fatal("main.go: signal.NotifyContext not found in main()")
	}

	if len(call.Args) < 2 {
		t.Fatal("main.go: signal.NotifyContext has no signal arguments")
	}
	sigArgs := call.Args[1:]
	for _, tc := range []struct{ pkg, name string }{
		{"os", "Interrupt"},
		{"syscall", "SIGTERM"},
		{"syscall", "SIGHUP"},
		{"syscall", "SIGQUIT"},
	} {
		if !sigPresent(sigArgs, tc.pkg, tc.name) {
			t.Errorf("main.go: signal.NotifyContext missing %s.%s", tc.pkg, tc.name)
		}
	}
}
