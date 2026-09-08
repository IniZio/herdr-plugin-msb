package msb

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func guardFindFunc(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

func guardIsSel2(expr ast.Expr, pkg, sel string) bool {
	s, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	return ok && id.Name == pkg && s.Sel.Name == sel
}

func guardFirstCallPos(body *ast.BlockStmt, pkg, fn string) token.Pos {
	var pos token.Pos
	ast.Inspect(body, func(n ast.Node) bool {
		if pos.IsValid() {
			return false
		}
		c, ok := n.(*ast.CallExpr)
		if ok && guardIsSel2(c.Fun, pkg, fn) {
			pos = c.Pos()
		}
		return true
	})
	return pos
}

func guardFindEnclosingIf(body *ast.BlockStmt, call *ast.CallExpr) *ast.IfStmt {
	var found *ast.IfStmt
	ast.Inspect(body, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		ifStmt, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		as, ok := ifStmt.Init.(*ast.AssignStmt)
		if ok && len(as.Rhs) == 1 && as.Rhs[0] == call {
			found = ifStmt
		}
		return true
	})
	return found
}

func guardBodyReturnsObj(body *ast.BlockStmt, obj *ast.Object) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, r := range ret.Results {
			if id, ok := r.(*ast.Ident); ok && id.Obj == obj {
				found = true
			}
		}
		return true
	})
	return found
}

func guardAdmitDataflow(fset *token.FileSet, fn *ast.FuncDecl, src string) string {
	createPos := guardFirstCallPos(fn.Body, "msbsdk", "CreateSandbox")
	if !createPos.IsValid() {
		return src + " " + fn.Name.Name + ": msbsdk.CreateSandbox not found"
	}

	var admitCalls []*ast.CallExpr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok && guardIsSel2(c.Fun, "admission", "Admit") {
			admitCalls = append(admitCalls, c)
		}
		return true
	})
	if len(admitCalls) == 0 {
		return fmt.Sprintf("%s %s: admission.Admit not called; host RAM budget silently disabled", src, fn.Name.Name)
	}

	foundQualifying := false
	for _, call := range admitCalls {
		p := fset.Position(call.Pos())
		loc := fmt.Sprintf("%s:%d", p.Filename, p.Line)

		ifStmt := guardFindEnclosingIf(fn.Body, call)
		if ifStmt == nil {
			return fmt.Sprintf("%s: admission.Admit error is discarded (not the Init of an if-stmt); host RAM budget silently disabled", loc)
		}
		as, ok := ifStmt.Init.(*ast.AssignStmt)
		if !ok || as.Tok != token.DEFINE || len(as.Rhs) != 1 || len(as.Lhs) != 1 {
			return fmt.Sprintf("%s: admission.Admit Init is not a single-var :=; error discarded, host RAM budget silently disabled", loc)
		}
		lhs, ok := as.Lhs[0].(*ast.Ident)
		if !ok || lhs.Obj == nil {
			return fmt.Sprintf("%s: admission.Admit LHS is not a bound identifier; host RAM budget silently disabled", loc)
		}
		errObj := lhs.Obj

		bin, ok := ifStmt.Cond.(*ast.BinaryExpr)
		if !ok {
			return fmt.Sprintf("%s: admission.Admit if-cond is not `<errIdent> != nil`; error discarded, host RAM budget silently disabled", loc)
		}
		condId, ok2 := bin.X.(*ast.Ident)
		nilId, ok3 := bin.Y.(*ast.Ident)
		if bin.Op != token.NEQ || !ok2 || !ok3 || condId.Obj != errObj || nilId.Name != "nil" {
			return fmt.Sprintf("%s: admission.Admit if-cond is not `<errIdent> != nil`; error discarded, host RAM budget silently disabled", loc)
		}

		if !guardBodyReturnsObj(ifStmt.Body, errObj) {
			return fmt.Sprintf("%s: admission.Admit error is not returned in if-body; host RAM budget silently disabled", loc)
		}

		if ifStmt.Pos() < createPos {
			foundQualifying = true
		}
	}

	if !foundQualifying {
		return fmt.Sprintf("%s %s: no qualifying admission.Admit error-return if-stmt precedes msbsdk.CreateSandbox; host RAM budget silently disabled", src, fn.Name.Name)
	}
	return ""
}

func TestAdmitErrorFlowsToReturnInCreateAndBoot(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "runtime.go", nil, 0)
	if err != nil {
		t.Fatalf("parse runtime.go: %v", err)
	}
	fn := guardFindFunc(f, "CreateAndBoot")
	if fn == nil {
		t.Fatal("CreateAndBoot not found in runtime.go")
	}
	if msg := guardAdmitDataflow(fset, fn, "runtime.go"); msg != "" {
		t.Error(msg)
	}
}

func TestAdmitErrorFlowsToReturnInRunEphemeral(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "exec.go", nil, 0)
	if err != nil {
		t.Fatalf("parse exec.go: %v", err)
	}
	fn := guardFindFunc(f, "RunEphemeral")
	if fn == nil {
		t.Fatal("RunEphemeral not found in exec.go")
	}
	if msg := guardAdmitDataflow(fset, fn, "exec.go"); msg != "" {
		t.Error(msg)
	}
}
