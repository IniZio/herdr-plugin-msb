package msbversion_test

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IniZio/herdr-plugin-msb/internal/msbversion"
)

const sdkModule = "github.com/superradcompany/microsandbox/sdk/go"

func findGoMod(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		candidate := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func TestSDKVersionPinned(t *testing.T) {
	path := findGoMod(t)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open go.mod: %v", err)
	}
	defer f.Close()

	var found string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		for i, field := range fields {
			if field == sdkModule && i+1 < len(fields) {
				found = fields[i+1]
				break
			}
		}
		if found != "" {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan go.mod: %v", err)
	}
	if found == "" {
		t.Fatalf("require line for %s not found in go.mod", sdkModule)
	}
	if found != msbversion.Required {
		t.Fatalf("go.mod pins %s to %s; want %s", sdkModule, found, msbversion.Required)
	}
}
