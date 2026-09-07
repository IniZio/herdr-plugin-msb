package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testModule = "github.com/example/testrepo"

func makeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	gomod := "module " + testModule + "\n\ngo 1.25.0\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(gomod), 0644); err != nil {
		t.Fatal(err)
	}
	for rel, src := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestCleanTree(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"internal/core/service/svc.go": "package service\n",
	})
	vs, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Errorf("want 0 violations, got %d: %v", len(vs), vs)
	}
}

func TestRule1_CoreImportsMSB(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"internal/core/service/svc.go": `package service

import _ "github.com/superradcompany/microsandbox/sdk/go"
`,
	})
	vs, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 {
		t.Fatalf("want 1 violation, got %d: %v", len(vs), vs)
	}
	msg := vs[0].String()
	if !strings.Contains(msg, "IMPORT-BAN:") ||
		!strings.Contains(msg, "github.com/superradcompany/microsandbox/sdk/go") {
		t.Errorf("unexpected violation message: %s", msg)
	}
}

func TestRule1_MSBPackageAllowed(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"internal/runtime/msb/wrap.go": `package msb

import _ "github.com/superradcompany/microsandbox/sdk/go"
`,
	})
	vs, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Errorf("internal/runtime/msb must be allowed; got violations: %v", vs)
	}
}

func TestRule2_CoreImportsRuntime(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"internal/core/service/svc.go": `package service

import _ "` + testModule + `/internal/runtime/msb"
`,
	})
	vs, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 {
		t.Fatalf("want 1 violation, got %d: %v", len(vs), vs)
	}
	msg := vs[0].String()
	if !strings.Contains(msg, "IMPORT-BAN:") ||
		!strings.Contains(msg, "/internal/runtime/msb") {
		t.Errorf("unexpected violation message: %s", msg)
	}
}

func TestRule2_OutsideCoreImportsRuntime(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"cmd/main/main.go": `package main

import _ "` + testModule + `/internal/runtime/msb"
`,
	})
	vs, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Errorf("packages outside internal/core must be allowed to import runtime; got: %v", vs)
	}
}
