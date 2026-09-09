//go:build linux

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPaneShGuestCwd(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	paneShPath := filepath.Join(filepath.Dir(thisFile), "../../plugins/herdr/bin/pane.sh")
	if _, err := os.Stat(paneShPath); err != nil {
		t.Skip("pane.sh not found")
	}

	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv.txt")

	stub := `#!/bin/sh
for a in "$@"; do
    printf '%s\n' "$a" >> "$ARGV_FILE"
done
printf -- '---\n' >> "$ARGV_FILE"
for a in "$@"; do
    case "$a" in -pty) exit 0 ;; esac
done
echo /bin/sh
`
	stubPath := filepath.Join(dir, "herdr-plugin-msb")
	if err := os.WriteFile(stubPath, []byte(stub), 0755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("sh", paneShPath, "shell")
	cmd.Env = []string{
		"PATH=" + dir + ":" + os.Getenv("PATH"),
		"HERDR_MSB_SANDBOX=test-sandbox",
		"ARGV_FILE=" + argvFile,
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pane.sh failed: %v\n%s", err, out)
	}

	data, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatal(err)
	}

	blocks := strings.Split(strings.TrimRight(string(data), "\n"), "\n---\n")
	var lastBlock string
	for _, b := range blocks {
		if b != "" {
			lastBlock = b
		}
	}
	argv := strings.Split(strings.TrimRight(lastBlock, "\n"), "\n")

	hasPty := false
	dashDashIdx := -1
	for i, a := range argv {
		if a == "-pty" {
			hasPty = true
		}
		if a == "--" {
			dashDashIdx = i
		}
	}
	if !hasPty {
		t.Error("final exec argv missing -pty")
	}
	if dashDashIdx < 0 {
		t.Fatal("final exec argv missing --")
	}
	rest := argv[dashDashIdx+1:]
	if len(rest) < 4 {
		t.Fatalf("argv after -- too short: %v", rest)
	}
	if rest[0] != "/bin/sh" {
		t.Errorf("argv[0] after -- = %q, want /bin/sh", rest[0])
	}
	if rest[1] != "-c" {
		t.Errorf("argv[1] after -- = %q, want -c", rest[1])
	}
	cScript := rest[2]
	if !strings.Contains(cScript, "cd /workspace") {
		t.Errorf("-c script missing 'cd /workspace': %q", cScript)
	}
	if !strings.Contains(cScript, "|| cd /") {
		t.Errorf("-c script missing '|| cd /': %q", cScript)
	}
	if rest[3] != "/bin/sh" {
		t.Errorf("guest shell $0 arg = %q, want /bin/sh", rest[3])
	}
}
