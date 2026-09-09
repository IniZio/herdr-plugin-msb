package cli

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
)

// verbsExecutableInTests are the only shipped verbs a test may pass to Run.
// Each either prints and exits or fails argument validation before touching the
// host. The rest — the space-*, new-tab and default-shell family — convert the
// operator's own workspace, boot microVMs, spawn panes or syscall.Exec away the
// test binary. Widen this list only with the same kind of justification.
var verbsExecutableInTests = []string{
	"create", "exec", "version", "declare", "status", "list", "local-agent", "help",
}

func verbGuardResolve(e ast.Expr, depth int) ([]string, bool) {
	if depth > 6 {
		return nil, false
	}
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			v, err := strconv.Unquote(x.Value)
			if err != nil {
				return nil, false
			}
			return []string{v}, true
		}
		return nil, false
	case *ast.ParenExpr:
		return verbGuardResolve(x.X, depth+1)
	case *ast.UnaryExpr:
		if x.Op == token.RANGE {
			return verbGuardResolve(x.X, depth+1)
		}
		return nil, false
	case *ast.KeyValueExpr:
		return verbGuardResolve(x.Value, depth+1)
	case *ast.CompositeLit:
		var out []string
		for _, el := range x.Elts {
			vs, ok := verbGuardResolve(el, depth+1)
			if !ok {
				return nil, false
			}
			out = append(out, vs...)
		}
		return out, true
	case *ast.Ident:
		if x.Name == "nil" {
			return nil, true
		}
		if x.Obj == nil {
			return nil, false
		}
		return verbGuardResolveObj(x, depth+1)
	}
	return nil, false
}

func verbGuardResolveObj(id *ast.Ident, depth int) ([]string, bool) {
	switch d := id.Obj.Decl.(type) {
	case *ast.ValueSpec:
		for i, n := range d.Names {
			if n.Obj == id.Obj && i < len(d.Values) {
				return verbGuardResolve(d.Values[i], depth)
			}
		}
		return nil, false
	case *ast.RangeStmt:
		return verbGuardResolve(d.X, depth)
	case *ast.AssignStmt:
		if d.Tok != token.DEFINE {
			return nil, false
		}
		idx := -1
		for i, l := range d.Lhs {
			if li, ok := l.(*ast.Ident); ok && li.Obj == id.Obj {
				idx = i
			}
		}
		if idx < 0 {
			return nil, false
		}
		if len(d.Rhs) == len(d.Lhs) {
			return verbGuardResolve(d.Rhs[idx], depth)
		}
		if len(d.Rhs) == 1 {
			return verbGuardResolve(d.Rhs[0], depth)
		}
	}
	return nil, false
}

func verbGuardIsRunCall(c *ast.CallExpr) bool {
	id, ok := c.Fun.(*ast.Ident)
	if !ok {
		return false
	}
	return id.Name == "Run" && id.Obj == nil && len(c.Args) >= 2
}

func TestNoTestExecutesAHostMutatingShippedVerb(t *testing.T) {
	allowed := map[string]bool{}
	for _, v := range verbsExecutableInTests {
		allowed[v] = true
	}
	shipped := map[string]bool{}
	for _, v := range shippedVerbs {
		shipped[v] = true
	}
	for _, v := range verbsExecutableInTests {
		if !shipped[v] {
			t.Fatalf("verbsExecutableInTests entry %q is not a shipped verb; a stale allowlist entry silently widens this guard", v)
		}
	}
	var banned []string
	for _, v := range shippedVerbs {
		if !allowed[v] {
			banned = append(banned, v)
		}
	}
	sort.Strings(banned)
	if len(banned) == 0 || !containsVerb(banned, "space-convert") {
		t.Fatalf("banned set is empty or missing space-convert (%v); this guard would pass vacuously", banned)
	}

	files, err := filepath.Glob("*_test.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob *_test.go: files=%v err=%v; this guard would pass vacuously", files, err)
	}

	inspected := 0
	for _, name := range files {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok || !verbGuardIsRunCall(c) {
				return true
			}
			inspected++
			pos := fset.Position(c.Pos())
			loc := fmt.Sprintf("%s:%d", pos.Filename, pos.Line)
			verbs, resolved := verbGuardResolve(c.Args[1], 0)
			if !resolved {
				t.Errorf("%s: cannot statically determine the verbs passed to Run; pass a string literal (or a literal slice/var in this file) so this guard can prove none of %v is executed", loc, banned)
				return true
			}
			for _, v := range verbs {
				if shipped[v] && !allowed[v] {
					t.Errorf("%s: test executes shipped verb %q through Run. Shipped verbs are never executed in tests: space-convert converts the operator's live $HERDR_WORKSPACE_ID, space-open-pane/new-tab spawn guest panes, and default-shell syscall.Execs away the test binary. Assert the registry statically instead.", loc, v)
				}
			}
			return true
		})
	}
	if inspected < 5 {
		t.Fatalf("only %d Run call sites inspected across %d test files; the scan is not reaching the package's tests", inspected, len(files))
	}
}

func containsVerb(list []string, v string) bool {
	for _, e := range list {
		if e == v {
			return true
		}
	}
	return false
}
