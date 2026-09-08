package livemsb

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"testing"
)

func TestRefreshPropagationUsesDirMounts(t *testing.T) {
	const target = "TestAC2AC6RefreshPropagation"
	src := "refreshprop_live_test.go"
	if override := os.Getenv("GUARD_SRC_OVERRIDE"); override != "" {
		src = override
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, src, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", src, err)
	}

	var fn *ast.FuncDecl
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if ok && fd.Name.Name == target {
			fn = fd
			break
		}
	}
	if fn == nil {
		t.Fatalf("%s: function %s not found", src, target)
	}

	callSites := 0
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var isRequire bool
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			isRequire = fun.Name == "RequireSandbox"
		case *ast.SelectorExpr:
			isRequire = fun.Sel.Name == "RequireSandbox"
		}
		if !isRequire || len(call.Args) < 2 {
			return true
		}
		callSites++
		where := fset.Position(call.Pos())
		lit, ok := call.Args[1].(*ast.CompositeLit)
		if !ok {
			t.Errorf("%s:%d: RequireSandbox second arg in %s is not a composite literal", src, where.Line, target)
			return true
		}
		hasDirMounts, hasFileMounts := false, false
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			id, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			switch id.Name {
			case "DirMounts":
				hasDirMounts = true
			case "FileMounts":
				hasFileMounts = true
			}
		}
		if hasFileMounts {
			t.Errorf("%s:%d: RequireSandbox in %s uses FileMounts; the cred store is a directory and must use DirMounts",
				src, where.Line, target)
		}
		if !hasDirMounts {
			t.Errorf("%s:%d: RequireSandbox in %s does not set DirMounts; credential directory requires DirMounts",
				src, where.Line, target)
		}
		return true
	})

	if callSites == 0 {
		t.Fatalf("%s: no RequireSandbox calls found in %s", src, target)
	}
}
