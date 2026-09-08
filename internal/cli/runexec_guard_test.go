package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestRunExecDeferCleanupAndLiveIdents(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "cmd_sandbox.go", nil, 0)
	if err != nil {
		t.Fatalf("parse cmd_sandbox.go: %v", err)
	}
	var runExecFn *ast.FuncDecl
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if ok && fn.Name.Name == "runExec" {
			runExecFn = fn
			break
		}
	}
	if runExecFn == nil {
		t.Fatal("runExec not found in cmd_sandbox.go")
	}

	hasDefer := false
	hasPTYCall := false
	ast.Inspect(runExecFn.Body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.DeferStmt:
			c := s.Call
			if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "cleanup" && len(c.Args) == 0 {
				hasDefer = true
			}
		case *ast.CallExpr:
			id, ok := s.Fun.(*ast.Ident)
			if !ok || id.Name != "applyPTYFields" || len(s.Args) < 3 {
				return true
			}
			hasPTYCall = true
			where := fset.Position(s.Pos())
			a1, ok1 := s.Args[1].(*ast.Ident)
			if !ok1 || a1.Name == "nil" {
				t.Errorf("cmd_sandbox.go:%d: applyPTYFields arg 1 must be a live identifier (resizeCh), not nil", where.Line)
			}
			a2, ok2 := s.Args[2].(*ast.Ident)
			if !ok2 || a2.Name == "false" || a2.Name == "true" {
				t.Errorf("cmd_sandbox.go:%d: applyPTYFields arg 2 must be a live bool identifier (raw), not a literal", where.Line)
			}
		}
		return true
	})

	if !hasDefer {
		t.Error("runExec does not defer cleanup(); raw-mode terminal will not be restored on signal exit")
	}
	if !hasPTYCall {
		t.Error("runExec does not call applyPTYFields")
	}
}
