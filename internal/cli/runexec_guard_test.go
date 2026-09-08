package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func findFuncDecl(f *ast.File, name string) *ast.FuncDecl {
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if ok && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

func calleeName(c *ast.CallExpr) string {
	switch fn := c.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

// boundBy reports whether id is the variable bound at position i of a `:=` whose sole RHS calls producer.
func boundBy(id *ast.Ident, producer string, i int) bool {
	if id == nil || id.Obj == nil {
		return false
	}
	as, ok := id.Obj.Decl.(*ast.AssignStmt)
	if !ok || as.Tok != token.DEFINE || len(as.Rhs) != 1 || i >= len(as.Lhs) {
		return false
	}
	call, ok := as.Rhs[0].(*ast.CallExpr)
	if !ok || calleeName(call) != producer {
		return false
	}
	lhs, ok := as.Lhs[i].(*ast.Ident)
	return ok && lhs.Obj == id.Obj
}

func identObj(e ast.Expr) *ast.Object {
	id, ok := e.(*ast.Ident)
	if !ok {
		return nil
	}
	return id.Obj
}

func TestRunExecPTYValuesFlowIntoExecRequest(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "cmd_sandbox.go", nil, 0)
	if err != nil {
		t.Fatalf("parse cmd_sandbox.go: %v", err)
	}
	runExecFn := findFuncDecl(f, "runExec")
	if runExecFn == nil {
		t.Fatal("runExec not found in cmd_sandbox.go")
	}

	var ptyCall, execCall *ast.CallExpr
	hasDefer := false
	ast.Inspect(runExecFn.Body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.DeferStmt:
			if id, ok := s.Call.Fun.(*ast.Ident); ok && len(s.Call.Args) == 0 && boundBy(id, "enterRawMode", 1) {
				hasDefer = true
			}
		case *ast.CallExpr:
			switch calleeName(s) {
			case "applyPTYFields":
				if ptyCall == nil {
					ptyCall = s
				}
			case "Exec":
				if execCall == nil {
					execCall = s
				}
			}
		}
		return true
	})

	if ptyCall == nil {
		t.Fatal("runExec does not call applyPTYFields")
	}
	if execCall == nil {
		t.Fatal("runExec does not call svc.Exec")
	}
	if len(ptyCall.Args) < 3 {
		t.Fatalf("applyPTYFields called with %d args, want >= 3", len(ptyCall.Args))
	}
	line := fset.Position(ptyCall.Pos()).Line

	if !hasDefer {
		t.Errorf("cmd_sandbox.go:%d: runExec must defer the cleanup func returned by enterRawMode; raw mode will not be restored", line)
	}

	unary, ok := ptyCall.Args[0].(*ast.UnaryExpr)
	if !ok || unary.Op != token.AND || identObj(unary.X) == nil {
		t.Fatalf("cmd_sandbox.go:%d: applyPTYFields arg 0 must be &<request variable>", line)
	}
	reqObj := identObj(unary.X)
	if len(execCall.Args) == 0 || identObj(execCall.Args[len(execCall.Args)-1]) != reqObj {
		t.Errorf("cmd_sandbox.go:%d: the request mutated by applyPTYFields is not the one passed to svc.Exec", line)
	}
	if ptyCall.Pos() >= execCall.Pos() {
		t.Errorf("cmd_sandbox.go:%d: applyPTYFields must run before svc.Exec, otherwise PTY fields never reach the guest", line)
	}

	if a1, _ := ptyCall.Args[1].(*ast.Ident); !boundBy(a1, "enterRawMode", 0) {
		t.Errorf("cmd_sandbox.go:%d: applyPTYFields arg 1 must be the resize channel returned by enterRawMode, not an unrelated or zero-valued variable", line)
	}
	if a2, _ := ptyCall.Args[2].(*ast.Ident); !boundBy(a2, "enterRawMode", 2) {
		t.Errorf("cmd_sandbox.go:%d: applyPTYFields arg 2 must be the raw-mode flag returned by enterRawMode, not an unrelated or zero-valued variable", line)
	}
}
