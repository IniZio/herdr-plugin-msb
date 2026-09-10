package portfwd

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func fakeRun(stdout, stderr string, code int, err error) Runner {
	return func(_ context.Context, argv []string) (string, string, int, error) {
		return stdout, stderr, code, err
	}
}

func TestDiscoverMachinesEmpty(t *testing.T) {
	machines, err := DiscoverMachines(context.Background(), fakeRun("[]", "", 0, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(machines) != 0 {
		t.Fatalf("want 0 machines, got %d", len(machines))
	}
}

func TestDiscoverMachinesPopulated(t *testing.T) {
	fixture := `[{"id":"abc123","label":"dev-server","target":"user@dev.example.com","session":"","enabled":true,"selected":true}]`
	machines, err := DiscoverMachines(context.Background(), fakeRun(fixture, "", 0, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(machines) != 1 {
		t.Fatalf("want 1 machine, got %d", len(machines))
	}
	m := machines[0]
	if m.ProfileID != "abc123" {
		t.Errorf("ProfileID: got %q, want %q", m.ProfileID, "abc123")
	}
	if m.Label != "dev-server" {
		t.Errorf("Label: got %q, want %q", m.Label, "dev-server")
	}
	if m.SSHTarget != "user@dev.example.com" {
		t.Errorf("SSHTarget: got %q, want %q", m.SSHTarget, "user@dev.example.com")
	}
	if !m.Enabled {
		t.Error("want Enabled=true")
	}
	if !m.Selected {
		t.Error("want Selected=true")
	}
}

func TestDiscoverMachinesUnknownFieldsIgnored(t *testing.T) {
	fixture := `[{"id":"x","label":"y","target":"z@h","session":"","enabled":false,"selected":false,"future_field":"ignored"}]`
	machines, err := DiscoverMachines(context.Background(), fakeRun(fixture, "", 0, nil))
	if err != nil {
		t.Fatalf("unexpected error on unknown field: %v", err)
	}
	if len(machines) != 1 {
		t.Fatalf("want 1 machine, got %d", len(machines))
	}
}

func TestDiscoverMachinesMalformedJSON(t *testing.T) {
	_, err := DiscoverMachines(context.Background(), fakeRun("not-json", "", 0, nil))
	if err == nil {
		t.Fatal("want error for malformed JSON, got nil")
	}
	if !errors.Is(err, ErrBadJSON) {
		t.Fatalf("want ErrBadJSON, got %v", err)
	}
}

func TestDiscoverMachinesNonZeroExit(t *testing.T) {
	_, err := DiscoverMachines(context.Background(), fakeRun("", "herdr: config not found", 1, nil))
	if err == nil {
		t.Fatal("want error for non-zero exit, got nil")
	}
	if !errors.Is(err, ErrHerdrFailed) {
		t.Fatalf("want ErrHerdrFailed, got %v", err)
	}
}

func TestDiscoverMachinesExecError(t *testing.T) {
	execErr := fmt.Errorf("exec: herdr not found")
	_, err := DiscoverMachines(context.Background(), fakeRun("", "", 0, execErr))
	if err == nil {
		t.Fatal("want error for exec failure, got nil")
	}
	if !errors.Is(err, execErr) {
		t.Fatalf("want wrapped execErr, got %v", err)
	}
}

func TestDiscoverMachinesArgvPassthrough(t *testing.T) {
	var got []string
	run := func(_ context.Context, argv []string) (string, string, int, error) {
		got = argv
		return "[]", "", 0, nil
	}
	if _, err := DiscoverMachines(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	want := []string{"herdr", "machine", "list", "--json"}
	if len(got) != len(want) {
		t.Fatalf("argv len: got %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("argv[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}
