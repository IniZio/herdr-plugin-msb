package msb

import (
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

func TestAdmitCalledBeforeCreateSandboxInCreateAndBoot(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "runtime.go", nil, 0)
	if err != nil {
		t.Fatalf("parse runtime.go: %v", err)
	}
	fn := guardFindFunc(f, "CreateAndBoot")
	if fn == nil {
		t.Fatal("CreateAndBoot not found in runtime.go")
	}
	admitPos := guardFirstCallPos(fn.Body, "admission", "Admit")
	createPos := guardFirstCallPos(fn.Body, "msbsdk", "CreateSandbox")
	if !admitPos.IsValid() {
		t.Error("runtime.go CreateAndBoot: admission.Admit call not found")
	}
	if !createPos.IsValid() {
		t.Error("runtime.go CreateAndBoot: msbsdk.CreateSandbox call not found")
	}
	if admitPos.IsValid() && createPos.IsValid() && admitPos >= createPos {
		t.Errorf("runtime.go CreateAndBoot: admission.Admit (line %d) must appear before msbsdk.CreateSandbox (line %d)",
			fset.Position(admitPos).Line, fset.Position(createPos).Line)
	}
}

func TestAdmitCalledBeforeCreateSandboxInRunEphemeral(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "exec.go", nil, 0)
	if err != nil {
		t.Fatalf("parse exec.go: %v", err)
	}
	fn := guardFindFunc(f, "RunEphemeral")
	if fn == nil {
		t.Fatal("RunEphemeral not found in exec.go")
	}
	admitPos := guardFirstCallPos(fn.Body, "admission", "Admit")
	createPos := guardFirstCallPos(fn.Body, "msbsdk", "CreateSandbox")
	if !admitPos.IsValid() {
		t.Error("exec.go RunEphemeral: admission.Admit call not found")
	}
	if !createPos.IsValid() {
		t.Error("exec.go RunEphemeral: msbsdk.CreateSandbox call not found")
	}
	if admitPos.IsValid() && createPos.IsValid() && admitPos >= createPos {
		t.Errorf("exec.go RunEphemeral: admission.Admit (line %d) must appear before msbsdk.CreateSandbox (line %d)",
			fset.Position(admitPos).Line, fset.Position(createPos).Line)
	}
}
