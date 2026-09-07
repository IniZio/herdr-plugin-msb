package runtime

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestProxyEndpointAbsent(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var specFound, vcpuFound, memFound bool
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if bytes.Contains(data, []byte("ProxyEndpoint")) {
			t.Errorf("%s: bytes contain ProxyEndpoint", name)
		}
		f, err := parser.ParseFile(fset, name, data, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == "ProxyEndpoint" {
				t.Errorf("%s:%s: ProxyEndpoint identifier found", name, fset.Position(id.Pos()))
			}
			if ts, ok := n.(*ast.TypeSpec); ok && ts.Name.Name == "SandboxSpec" {
				specFound = true
				if st, ok2 := ts.Type.(*ast.StructType); ok2 {
					for _, field := range st.Fields.List {
						for _, fn := range field.Names {
							switch fn.Name {
							case "VCPUs":
								vcpuFound = true
							case "MemoryMiB":
								memFound = true
							}
						}
					}
				}
			}
			return true
		})
	}
	if !specFound {
		t.Error("SandboxSpec not found in package")
	}
	if !vcpuFound {
		t.Error("SandboxSpec missing VCPUs field")
	}
	if !memFound {
		t.Error("SandboxSpec missing MemoryMiB field")
	}
}
