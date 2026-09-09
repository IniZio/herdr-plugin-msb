package logguard_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var successWords = []string{"PASS", "PROOF", "correct"}

func hasSuccessWord(s string) bool {
	for _, w := range successWords {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found walking up from " + dir)
		}
		dir = parent
	}
}

func tCallName(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return sel.Sel.Name
}

func callHasSuccessWord(call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		lit, ok := arg.(*ast.BasicLit)
		if !ok {
			continue
		}
		if hasSuccessWord(lit.Value) {
			return true
		}
	}
	return false
}

func checkFunc(fset *token.FileSet, fn *ast.FuncDecl) []string {
	var violations []string
	for _, stmt := range fn.Body.List {
		expr, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := expr.X.(*ast.CallExpr)
		if !ok {
			continue
		}
		name := tCallName(call)
		if name != "Log" && name != "Logf" {
			continue
		}
		if !callHasSuccessWord(call) {
			continue
		}
		pos := fset.Position(call.Pos())
		violations = append(violations, fmt.Sprintf(
			"%s:%d: success-implying log at top level of test function (not inside a guarding conditional)",
			pos.Filename, pos.Line,
		))
	}
	return violations
}

func TestNoUnguardedSuccessLogs(t *testing.T) {
	root := repoRoot(t)
	var violations []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, "_live_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			violations = append(violations, checkFunc(fset, fn)...)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking repo: %v", err)
	}
	for _, v := range violations {
		t.Error(v)
	}
}
