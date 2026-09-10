package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Manifest struct {
	SourceCommit string            `json:"source_commit"`
	Artifacts    map[string]string `json:"artifacts"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: distcheck <repo-root> [--write]")
		os.Exit(2)
	}
	repoRoot := os.Args[1]
	writeMode := len(os.Args) > 2 && os.Args[2] == "--write"

	if writeMode {
		if err := writeManifest(repoRoot); err != nil {
			fmt.Fprintf(os.Stderr, "dist-check: FAIL — %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := verifyManifest(repoRoot); err != nil {
		fmt.Fprintf(os.Stderr, "dist-check: FAIL — %v\n", err)
		os.Exit(1)
	}
}

func verifyManifest(repoRoot string) error {
	manifestPath := filepath.Join(repoRoot, "dist", "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("manifest not found: %w; run 'make dist'", err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}

	current, err := lastSourceCommit(repoRoot)
	if err != nil {
		return err
	}

	if m.SourceCommit != current {
		return fmt.Errorf("source drifted: manifest built from %s, last source commit is %s; run 'make dist'",
			m.SourceCommit[:8], current[:8])
	}

	distDir := filepath.Join(repoRoot, "dist")
	for name, expected := range m.Artifacts {
		actual, err := sha256File(filepath.Join(distDir, name))
		if err != nil {
			return fmt.Errorf("artifact %s: %w", name, err)
		}
		if actual != expected {
			return fmt.Errorf("artifact %s: checksum mismatch\n  expected %s\n  got      %s", name, expected, actual)
		}
	}

	fmt.Printf("dist-check: OK (source %s, %d artefacts)\n", current[:8], len(m.Artifacts))
	return nil
}

func writeManifest(repoRoot string) error {
	commit, err := lastSourceCommit(repoRoot)
	if err != nil {
		return err
	}

	distDir := filepath.Join(repoRoot, "dist")
	entries, err := os.ReadDir(distDir)
	if err != nil {
		return fmt.Errorf("cannot read dist/: %w", err)
	}

	artifacts := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "manifest.json" {
			continue
		}
		sha, err := sha256File(filepath.Join(distDir, entry.Name()))
		if err != nil {
			return fmt.Errorf("sha256 %s: %w", entry.Name(), err)
		}
		artifacts[entry.Name()] = sha
	}

	m := Manifest{SourceCommit: commit, Artifacts: artifacts}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "manifest.json"), data, 0644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	fmt.Printf("dist-check: wrote manifest (source %s, %d artefacts)\n", commit[:8], len(artifacts))
	return nil
}

func lastSourceCommit(repoRoot string) (string, error) {
	out, err := exec.Command("git", "-C", repoRoot, "log", "--format=%H", "-1", "--", ":(exclude)dist").Output()
	if err != nil {
		return "", fmt.Errorf("git log: %w", err)
	}
	c := strings.TrimSpace(string(out))
	if len(c) != 40 {
		return "", fmt.Errorf("unexpected commit hash %q", c)
	}
	return c, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
