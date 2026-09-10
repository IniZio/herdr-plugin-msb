package clientagent

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IniZio/herdr-plugin-msb/internal/core/portfwd"
)

type fakeRunner struct {
	calls  [][]string
	stdout string
}

func (f *fakeRunner) run(ctx context.Context, argv []string) (string, string, int, error) {
	f.calls = append(f.calls, argv)
	return f.stdout, "", 0, nil
}

func hasSuffix(calls [][]string, sub string) bool {
	for _, argv := range calls {
		for _, a := range argv {
			if strings.Contains(a, sub) {
				return true
			}
		}
	}
	return false
}

func baseProvisioner(t *testing.T, fr *fakeRunner, copyFn CopyFn) *Provisioner {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "herdr-plugin-msb")
	if err := os.WriteFile(src, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	toml := filepath.Join(dir, "herdr-plugin.toml")
	if err := os.WriteFile(toml, []byte("[plugin]"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Provisioner{
		Target:       "user@host",
		BinSrc:       src,
		PluginTOML:   toml,
		LocalVersion: "0.1.0",
		Run:          portfwd.Runner(fr.run),
		Copy:         copyFn,
		Stderr:       &bytes.Buffer{},
	}
}

func TestProvisionNoConsent(t *testing.T) {
	var copied int
	fakeCopy := func(_ context.Context, _, _ string) error { copied++; return nil }

	fr := &fakeRunner{}
	p := baseProvisioner(t, fr, fakeCopy)
	p.CheckConsent = func(string) error { return ErrNoConsent }

	err := p.EnsureProvisioned(context.Background())
	/* RED: got <nil>, want ErrNoConsent — CheckConsent was not checked first */
	if !errors.Is(err, ErrNoConsent) {
		t.Fatalf("want ErrNoConsent, got %v", err)
	}
	if copied != 0 {
		t.Fatalf("Copy called %d times, want 0", copied)
	}

	fr2 := &fakeRunner{}
	p2 := baseProvisioner(t, fr2, fakeCopy)
	p2.CheckConsent = func(string) error { return nil }
	copied = 0
	_ = p2.EnsureProvisioned(context.Background())
	if copied == 0 {
		t.Fatal("negative control: Copy never called when consent given")
	}
}

func TestProvisionIdempotentSameVersion(t *testing.T) {
	var copied int
	fakeCopy := func(_ context.Context, _, _ string) error { copied++; return nil }

	fr := &fakeRunner{stdout: "0.1.0\n"}
	p := baseProvisioner(t, fr, fakeCopy)
	p.CheckConsent = func(string) error { return nil }

	err := p.EnsureProvisioned(context.Background())
	/* RED: Copy was called; idempotent path not taken when versions match */
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if copied != 0 {
		t.Fatalf("Copy called %d times, want 0 (idempotent)", copied)
	}

	fr3 := &fakeRunner{stdout: "0.0.9\n"}
	p3 := baseProvisioner(t, fr3, fakeCopy)
	p3.CheckConsent = func(string) error { return nil }
	copied = 0
	_ = p3.EnsureProvisioned(context.Background())
	if copied == 0 {
		t.Fatal("negative control: Copy not called when version differs")
	}
}

func TestProvisionVersionMismatch(t *testing.T) {
	fakeCopy := func(_ context.Context, _, _ string) error { return nil }

	fr := &fakeRunner{stdout: "0.0.1\n"}
	var stderr bytes.Buffer
	p := baseProvisioner(t, fr, fakeCopy)
	p.CheckConsent = func(string) error { return nil }
	p.Stderr = &stderr

	if err := p.EnsureProvisioned(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	/* RED: Stderr empty, VERSION MISMATCH not logged */
	if !strings.Contains(stderr.String(), "VERSION MISMATCH") {
		t.Fatalf("want VERSION MISMATCH in stderr, got %q", stderr.String())
	}

	var stderr2 bytes.Buffer
	fr2 := &fakeRunner{stdout: "0.1.0\n"}
	p2 := baseProvisioner(t, fr2, fakeCopy)
	p2.CheckConsent = func(string) error { return nil }
	p2.Stderr = &stderr2
	_ = p2.EnsureProvisioned(context.Background())
	if strings.Contains(stderr2.String(), "MISMATCH") {
		t.Fatal("negative control: MISMATCH logged when versions match")
	}
}

func TestProvisionNoBinary(t *testing.T) {
	fakeCopy := func(_ context.Context, _, _ string) error { return nil }

	fr := &fakeRunner{}
	p := baseProvisioner(t, fr, fakeCopy)
	p.CheckConsent = func(string) error { return nil }
	p.BinSrc = filepath.Join(t.TempDir(), "nonexistent-binary")

	err := p.EnsureProvisioned(context.Background())
	/* RED: got <nil>, want ErrNoSourceBinary */
	if !errors.Is(err, ErrNoSourceBinary) {
		t.Fatalf("want ErrNoSourceBinary, got %v", err)
	}

	dir := t.TempDir()
	existingBin := filepath.Join(dir, "herdr-plugin-msb")
	if err2 := os.WriteFile(existingBin, []byte("bin"), 0o755); err2 != nil {
		t.Fatal(err2)
	}
	fr2 := &fakeRunner{}
	p2 := baseProvisioner(t, fr2, fakeCopy)
	p2.CheckConsent = func(string) error { return nil }
	p2.BinSrc = existingBin
	if err3 := p2.EnsureProvisioned(context.Background()); err3 != nil {
		t.Fatalf("negative control: got unexpected error %v", err3)
	}
}

func TestProvisionFirstInstall(t *testing.T) {
	var copiedSrcs []string
	fakeCopy := func(_ context.Context, src, _ string) error {
		copiedSrcs = append(copiedSrcs, src)
		return nil
	}

	fr := &fakeRunner{stdout: ""}
	p := baseProvisioner(t, fr, fakeCopy)
	p.CheckConsent = func(string) error { return nil }

	if err := p.EnsureProvisioned(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	/* RED: Copy count was 0, ssh mkdir/chmod/link calls absent */
	if len(copiedSrcs) < 2 {
		t.Fatalf("want ≥2 Copy calls, got %d", len(copiedSrcs))
	}
	if !hasSuffix(fr.calls, "mkdir") {
		t.Fatal("no ssh mkdir call recorded")
	}
	if !hasSuffix(fr.calls, "chmod") {
		t.Fatal("no ssh chmod call recorded")
	}
	if !hasSuffix(fr.calls, "herdr plugin link") {
		t.Fatal("no ssh herdr plugin link call recorded")
	}
}
