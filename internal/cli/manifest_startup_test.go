//go:build linux

package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestManifestStartupCommandDoesNotDependOnPATH(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	manifest := filepath.Join(filepath.Dir(thisFile), "../../herdr-plugin.toml")
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(string(raw), "\n")
	idx := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "[[startup]]" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("no [[startup]] section found in herdr-plugin.toml")
	}

	cmd := ""
	for _, l := range lines[idx+1:] {
		if strings.HasPrefix(strings.TrimSpace(l), "[[") {
			break
		}
		if strings.HasPrefix(strings.TrimSpace(l), "command") {
			cmd = l
			break
		}
	}
	if cmd == "" {
		t.Fatal("[[startup]] has no command key")
	}

	if !strings.Contains(cmd, "$HERDR_PLUGIN_ROOT") {
		t.Fatalf("the startup command must locate herdr-plugin-msb-agent through\n"+
			"$HERDR_PLUGIN_ROOT. Neither build step puts that binary on PATH: the macOS\n"+
			"[[build]] step copies it into the plugin root, and herdr resolves a bare\n"+
			"command name against PATH, so the hook dies with exit 127 before it runs.\n"+
			"got: %s", strings.TrimSpace(cmd))
	}
}
