package msb

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func guardIsSel(e ast.Expr, pkg, sel string) bool {
	s, ok := e.(*ast.SelectorExpr)
	if !ok || s.Sel.Name != sel {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	return ok && id.Name == pkg
}

func guardTreeCallsIdent(n ast.Node, name string) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if c, ok := x.(*ast.CallExpr); ok {
			if id, ok2 := c.Fun.(*ast.Ident); ok2 && id.Name == name {
				found = true
			}
		}
		return true
	})
	return found
}

func guardTreeCallsSel(n ast.Node, pkg, sel string) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if c, ok := x.(*ast.CallExpr); ok && guardIsSel(c.Fun, pkg, sel) {
			found = true
		}
		return true
	})
	return found
}

func guardAssignedExprs(fn *ast.FuncDecl, name string) []ast.Expr {
	var out []ast.Expr
	ast.Inspect(fn, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range s.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name == name && i < len(s.Rhs) {
					out = append(out, s.Rhs[i])
				}
			}
		case *ast.ValueSpec:
			for i, id := range s.Names {
				if id.Name == name && i < len(s.Values) {
					out = append(out, s.Values[i])
				}
			}
		}
		return true
	})
	return out
}

func TestCreateSandboxCallSitesCarryNetworkPolicy(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	callSites := 0
	var optionsFn *ast.FuncDecl
	var optionsFile string

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if fn.Recv == nil && fn.Name.Name == "SandboxOptions" {
				optionsFn, optionsFile = fn, name
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || !guardIsSel(call.Fun, "msbsdk", "CreateSandbox") {
					return true
				}
				callSites++
				where := fmt.Sprintf("%s:%d", name, fset.Position(call.Pos()).Line)
				if call.Ellipsis == token.NoPos || len(call.Args) == 0 {
					t.Errorf("%s: CreateSandbox does not spread a built option set", where)
					return true
				}
				argExpr := call.Args[len(call.Args)-1]
				exprs := []ast.Expr{argExpr}
				if id, ok := argExpr.(*ast.Ident); ok {
					exprs = append(exprs, guardAssignedExprs(fn, id.Name)...)
				}
				routed, ownNetwork := false, false
				for _, ex := range exprs {
					if guardTreeCallsIdent(ex, "SandboxOptions") {
						routed = true
					}
					if guardTreeCallsSel(ex, "msbsdk", "WithNetwork") {
						ownNetwork = true
					}
				}
				if !routed {
					t.Errorf("%s: CreateSandbox options in %s are not derived from SandboxOptions(...)", where, fn.Name.Name)
				}
				if ownNetwork {
					t.Errorf("%s: CreateSandbox call site in %s builds its own msbsdk.WithNetwork", where, fn.Name.Name)
				}
				return true
			})
		}
	}

	if callSites == 0 {
		t.Fatal("no msbsdk.CreateSandbox call sites found in package msb")
	}
	if optionsFn == nil {
		t.Fatal("SandboxOptions not found in package msb")
	}

	netFound := false
	var stack []ast.Node
	ast.Inspect(optionsFn.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if c, ok := n.(*ast.CallExpr); ok && guardIsSel(c.Fun, "msbsdk", "WithNetwork") {
			netFound = true
			for _, a := range stack {
				switch a.(type) {
				case *ast.IfStmt, *ast.CaseClause, *ast.ForStmt, *ast.RangeStmt, *ast.FuncLit:
					t.Errorf("%s:%d: msbsdk.WithNetwork inside SandboxOptions is gated by %T; every call site routes through it, so a nil-gated network policy re-opens the hole",
						optionsFile, fset.Position(c.Pos()).Line, a)
					return false
				}
			}
		}
		stack = append(stack, n)
		return true
	})
	if !netFound {
		t.Errorf("%s: SandboxOptions never appends msbsdk.WithNetwork", optionsFile)
	}
}
