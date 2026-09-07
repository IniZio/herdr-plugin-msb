package main

import (
	"strings"
	"testing"
)

// Regression: a relative root of "." was skipped by the dot-directory rule, so
// make vet reported green over a live violation. See doc/import-ban.md.
func TestRelativeRootIsScanned(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"go.mod": "module github.com/example/testrepo\n\ngo 1.25.0\n",
		"internal/core/service/svc.go": "package service\n\n" +
			`import _ "github.com/superradcompany/microsandbox/sdk/go"` + "\n",
	})
	t.Chdir(root)

	for _, relRoot := range []string{".", "./"} {
		violations, err := Check(relRoot)
		if err != nil {
			t.Fatalf("Check(%q): %v", relRoot, err)
		}
		if len(violations) != 1 {
			t.Fatalf("Check(%q): want 1 violation, got %d: %v", relRoot, len(violations), violations)
		}
		if !strings.Contains(violations[0].BannedImport, "microsandbox") {
			t.Fatalf("Check(%q): unexpected violation %v", relRoot, violations[0])
		}
	}
}

func TestDotDirectoriesStillSkipped(t *testing.T) {
	root := makeRepo(t, map[string]string{
		"go.mod": "module github.com/example/testrepo\n\ngo 1.25.0\n",
		".hidden/internal/core/service/svc.go": "package service\n\n" +
			`import _ "github.com/superradcompany/microsandbox/sdk/go"` + "\n",
	})
	t.Chdir(root)

	violations, err := Check(".")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("want dot-directories skipped, got %d violations: %v", len(violations), violations)
	}
}
