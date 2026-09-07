package main

import (
	"bufio"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type Violation struct {
	RelFile       string
	PkgImportPath string
	BannedImport  string
	Reason        string
}

func (v Violation) String() string {
	return fmt.Sprintf("IMPORT-BAN: %s: package %s may not import %s: %s",
		v.RelFile, v.PkgImportPath, v.BannedImport, v.Reason)
}

const (
	msbSDKPrefix  = "github.com/superradcompany/microsandbox"
	msbAllowedDir = "internal/runtime/msb"
	coreDir       = "internal/core"
	runtimeImport = "/internal/runtime/"
)

func underDir(relDir, dir string) bool {
	return relDir == dir || strings.HasPrefix(relDir, dir+"/")
}

func Check(repoRoot string) ([]Violation, error) {
	modulePath, err := readModulePath(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("read module path: %w", err)
	}

	var violations []Violation

	err = filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path == repoRoot {
				return nil
			}
			name := d.Name()
			if name == "vendor" || name == ".git" || name == "testdata" ||
				strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}

		if strings.HasPrefix(rel, filepath.Join("tools", "importban", "testdata")) {
			return nil
		}

		dir := filepath.Dir(rel)
		var pkgImportPath string
		if dir == "." {
			pkgImportPath = modulePath
		} else {
			pkgImportPath = modulePath + "/" + filepath.ToSlash(dir)
		}

		relDir := filepath.ToSlash(dir)

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return nil
		}

		for _, imp := range f.Imports {
			impPath := strings.Trim(imp.Path.Value, `"`)

			if strings.HasPrefix(impPath, msbSDKPrefix) {
				if !underDir(relDir, msbAllowedDir) {
					violations = append(violations, Violation{
						RelFile:       filepath.ToSlash(rel),
						PkgImportPath: pkgImportPath,
						BannedImport:  impPath,
						Reason: "only packages under internal/runtime/msb/ may import the microsandbox SDK; " +
							"closures cannot cross its fork+exec boundary, so all SDK contact must sit in one blast site",
					})
				}
			}

			if underDir(relDir, coreDir) {
				if strings.Contains(impPath, runtimeImport) {
					violations = append(violations, Violation{
						RelFile:       filepath.ToSlash(rel),
						PkgImportPath: pkgImportPath,
						BannedImport:  impPath,
						Reason: "internal/core is the substrate-agnostic seam; importing an internal/runtime " +
							"implementation inverts the dependency",
					})
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return violations, nil
}

func readModulePath(repoRoot string) (string, error) {
	f, err := os.Open(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module")), nil
		}
	}
	return "", fmt.Errorf("no module declaration found in go.mod")
}
